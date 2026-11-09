# CLAUDE.md

qute: Qt 6 bindings for Go, module `github.com/nomad-software/qute`. This file
is the source of truth for the design and for how to work in the repo. If code
and this file disagree, fix one of them; don't leave them diverged. The README
is the user-facing introduction, including why to use qute and how it suits
agent-driven development; keep its claims (numbers, flags, function names)
true when the code changes.

## Goals

- No double free or dangling pointer reachable from ordinary Go code. Misuse
  is a clear panic, never a segfault.
- Automatic cleanup by default (GC), deterministic `Delete()` when needed.
- Ownership rules live in small YAML spec files, not in generator code. A
  binding bug is usually a one-line spec fix plus its generated test.
- Apps, including ones Claude writes, can be built, tested and visually
  checked headlessly.

Platform priority: Linux comes first for now. macOS and Windows support is
still wanted, but later; keep the code portable, and don't spend effort on
those platforms until asked.

Non-goals: Qt 5, a QML-first API (later), zero cgo overhead (correctness
first).

Background: miqt (Go) leaves QObjects to `Delete()` or opt-in `GoGC()` and
doesn't re-track ownership when it changes. PySide's typesystem files and
PyQt's `/Transfer/` annotations show ownership belongs in declarative
per-method data. cxx-qt and qmetaobject-rs use a queue to the Qt thread and
`QPointer`-style weak handles. gotk4 shows GC finalizers must not run Qt code
off the main thread. qute takes per-method ownership tracking, GC on by
default (made safe by pinning), PyQt's annotation vocabulary, and a GUI-thread
cleanup queue.

## Architecture

```
Qt headers --libclang (tools/irgen)--> spec/ir/*.json --+
                                       spec/classes/    +--> cmd/qutegen --> qtcore/ qtgui/ qtwidgets/
                                       spec/overlays/ --+                    COVERAGE.md, ownership tests

run time: app -> qute (root package) -> qtcore/qtgui/qtwidgets (generated Go + C++ shim) -> Qt
                                     \-> internal/rt (handles, registry, pins, cleanup queue)
```

Only `internal/rt` decides who frees what. The C++ shim calls back into it
when Qt destroys an object, emits a signal, or runs an overridden virtual.

One Go package per Qt module (`qtcore`, `qtgui`, `qtwidgets`, `qtopengl`,
`qtopenglwidgets`), each with its own cgo shim built via `pkg-config`; plain
`go build`. Module dependencies are explicit in `cmd/qutegen/main.go` (QtOpenGL
needs only QtCore and QtGui), because each shim may only include headers its
pkg-config entry provides. The root package
`qute` holds `Main`, `Post`, `PostSync`, `Screenshot`, `DumpTree`,
`Click`/`Type`/`Key` and the test harness. `cmd/qute` is the companion tool.

Adding a module touches more than the generator. Every list of the generated
packages must include it: `mods` in `cmd/qutegen/main.go`, `libraryPkgs` in
`internal/rt/site.go` (debug call sites), `qtPkgs` in `cmd/qute/vet.go`
(otherwise `qute vet` silently ignores the package), `goKeywords` in
`cmd/qutegen/model.go`, and the package lists in CI, the README and below.

## Ownership model

Every handle is in exactly one state, stored in the handle and changed only by
annotated methods. Compile-time `Owned[T]` types were rejected: `SetParent`
changes ownership at runtime, so a static type would lie.

| State    | Who frees it                  | GC cleanup | Example                          |
| -------- | ----------------------------- | ---------- | -------------------------------- |
| Owned    | Go (GC cleanup or `Delete()`) | yes        | `qtwidgets.NewQWidget(nil)`      |
| Parented | Qt parent / container         | no         | widget after `lay.AddWidget(w)`  |
| Borrowed | someone else; Go never frees  | no         | `QApplicationFocusWidget()`      |
| Dead     | already destroyed             | no         | any handle after its object died |

Rules (panic messages cite these numbers; keep them stable):

1. Constructors return Owned, or Parented when a non-nil parent is passed.
2. `transfer-to-cpp` arguments become Parented and their GC cleanup is cancelled.
3. `transfer-to-go` results and arguments become Owned and get a cleanup.
4. `Delete()` is idempotent and works on Owned and Parented; on Borrowed it panics.
5. Inside a signal or event handler use `DeleteLater()`; `Delete()` of a
   QObject there panics in debug builds.
6. Scoped arguments (events, `QPainter*` and style options in callbacks) are
   marked Dead when the callback returns. Long-lived arguments stay Borrowed.
7. Safety net: GC never deletes a QObject that has a parent, even if a rule
   was missed.

`SetParent(p)` / `SetParent(nil)` move between Parented and Owned
(`transfer-this`).

```go
w := qtwidgets.NewQWidget(nil)                // Owned
lay := qtwidgets.NewQVBoxLayout(w)            // Parented to w
lbl := qtwidgets.NewQLabelWithText("hi", nil) // Owned
lay.AddWidget(lbl)                            // lbl: Owned -> Parented
w.Show()                                      // w pinned while visible
// hidden + unreachable -> GC frees w, which frees lay and lbl
```

**Pinning.** GC must never delete something the user is looking at. An object
is pinned (held strongly in `rt`'s strong set) while it is a visible top-level
window (an event filter on QApplication watches Show/Hide), while it has Go
virtual overrides (for its whole life, so its Go state survives), while it has
live signal connections, or after `Keep()` (until `Unkeep()`). GC frees an
Owned object only when it is unpinned and unreachable. So a parentless object
with connections must be `Delete()`d; `qute vet` flags ones that never are.

## Object lifetime and handle safety

- **Registry.** `map[unsafe.Pointer]weak.Pointer[Handle]` (Go `weak`).
  Wrapping a pointer twice returns the same handle, and the same Go wrapper
  per type; an upcast of a derived wrapper returns its embedded base, so
  comparing wrappers of the same object with `==` works.
- **Death notice.** On first wrap of a QObject, `rt` connects
  `QObject::destroyed`; it marks the handle Dead, removes the registry entry
  and releases its Go callbacks. The entry goes before the memory is freed, so
  a new object at a reused address gets a fresh handle. Queued cleanups carry
  a serial number for the same reason.
- **Checked calls.** Every generated method does one atomic load; a Dead
  handle panics with class and method name and the fix.
- **Non-QObject classes.** With `notify-dtor` the shim subclass reports death
  from its destructor; only objects built by our constructors are registered.
  Other non-QObjects (`KindPlain`) are never registered and get a fresh handle
  per wrap.
- **Value classes** (`QImage`, `QFont`, `QIcon`, `QVariant`, ...) are heap
  copies in Owned handles, freed by GC; Qt's implicit sharing keeps copies
  cheap.

## Marshalling

| Qt type                                    | Go type                 | Freed by                          |
| ------------------------------------------ | ----------------------- | --------------------------------- |
| `QString`, `QByteArray`                    | `string`, `[]byte`      | copied; C buffer freed in wrapper |
| `QStringList`, `QList<T>`, `QPolygon(F)`   | `[]string`, `[]T`       | copied                            |
| `QPoint(F)`, `QSize(F)`, `QRect(F)`, `QMargins`, `QColor` | Go structs | nothing to free             |
| value classes                              | Owned handle            | GC cleanup                        |
| object pointers                            | `*T`; args take `TLike` | per ownership state               |
| enums / `QFlags`                           | typed constants / bitmask type with `Has()` | —             |

Beyond the table, the generator maps (see `cmd/qutegen/types.go`):

- out-parameters (`int *row`, `bool *ok`, `QTransform &result`) to extra Go
  results, and in-out references (`QString &`, `int &`) to a parameter plus a
  result;
- a pointer followed by its count (`const QPoint *, int`, `const uchar *, int`,
  `char *data, qint64 maxlen`) to one slice, and `QSpan<T>` to a slice;
- `QMap`/`QHash` with string or int keys to Go maps;
- `std::chrono::duration` to `time.Duration`, `time_point<system_clock>` to
  `time.Time`, `std::filesystem::path` to `string`, `std::optional<T>` to a
  nil-able handle;
- `std::function` parameters to Go funcs, called back through
  `qute_<pkg>_call` and released when Qt drops the function;
- `void *`, `uchar *` to `unsafe.Pointer` (escape hatch);
- a value class's `T &` result that is `*this` to the receiver, for chaining;
- `const T *` of a value class to a nil-able handle;
- public data members to getter/setter methods, nested classes
  (`QFont::Tag`) to flat Go names (`QFontTag`), and classes without declared
  constructors to an implicit `NewX()`;
- `QList<std::pair<A, B>>` to `[]qtcore.Pair[A, B]`, a single `std::pair`
  result to two Go results (`major, minor := f.Version()`), `QSet<T>` results
  to slices, `char32_t`/`char16_t` to `rune`, `QStringView` results to
  `string`;
- a `const T *` of a value class with a plural name and no count (an array
  Qt would read past one element) is skipped, never bound as one handle;
- classes marked `raw-pointers` (the GL function tables, `QOpenGLTexture`)
  take every pointer-to-number argument as `unsafe.Pointer`, because GL
  decides the size from other arguments;
- the integer `GL_*` macros become untyped constants in `qtgui/gl_gen.go`
  (`irgen` reads them with libclang's macro records).

Deliberately unsupported (reported as "skipped on purpose", with the reason):
C++ move overloads, `QBindable`, `QThread`, iterators, Qt's private and
platform internals, C function pointers, returned `std::function`s,
`std::initializer_list`, fixed-size C arrays, C++20 ordering types, template
classes (`QGenericMatrix`, `QRgbaFloat`), printing page setup, accessibility,
Vulkan, low-level font and text-layout internals, and interfaces that need
C++ subclassing.

Go strings go in as `(ptr, len)` into `QString::fromUtf8`, which copies, so
cgo pointer rules hold. Results come back malloc'd and are copied and freed in
the same generated wrapper; C memory never reaches user code. `nil` maps to
`nullptr` both ways and a returned null is never wrapped.

## Threading

- The main goroutine is locked to the main OS thread in `internal/rt`'s init.
  `qute.Main(setup)` creates QApplication there and runs the event loop.
- `qute.Post(f)` / `qute.PostSync(f)` queue work via
  `QMetaObject::invokeMethod(qApp, ..., Qt::QueuedConnection)`. `PostSync` on
  the GUI thread runs `f` inline (no deadlock) and re-raises panics in the
  caller.
- GC cleanups never call `delete`: they push onto a lock-free queue that the
  GUI thread drains from a posted event. After the event loop exits, queued
  cleanups are dropped, since deleting after the app is gone crashes Qt.
- Build tag `qute_debug` adds a GUI-thread check to every call, records
  creation and death call sites, and enforces rule 5.

## Signals and subclassing

- `btn.OnClicked(func(checked bool) {...})` returns a `qtcore.Connection`.
  The closure is stored as a `cgo.Handle`; the C++ lambda captures only its
  id, and a `GoSlot` destructor releases it when Qt drops the connection
  (`Disconnect()` or sender destroyed). The sender is the context object.
- `model.EmitDataChanged(...)` emits a signal.
- Subclassing: embed `qtwidgets.QWidgetBase` (etc.), define virtual methods
  by name, construct with `NewQWidgetWith(v, parent)`. The shim subclass
  (`qute_QWidget`) checks a per-instance bitmask, so non-overridden virtuals
  skip the cgo hop. Overrides are detected with interface assertions plus
  `rt.UserDefined`, which rejects promoted (compiler-generated) methods so a
  public virtual like `SizeHint` can't dispatch to itself. `b.Super().X()`
  calls the C++ base; protected methods are on the Base type. Pure virtuals
  not overridden return a default value. Classes whose pure virtuals can't be
  bound aren't subclassable.

```go
type Canvas struct{ qtwidgets.QWidgetBase }

func (c *Canvas) PaintEvent(e *qtgui.QPaintEvent) {
	p := qtgui.NewQPainterOn(c)
	defer p.Delete()
	p.FillRect(e.Rect(), qtgui.RGB(200, 30, 30))
}

qtwidgets.NewQWidgetWith(&Canvas{}, nil)
```

## Generator and spec

- **IR** (`spec/ir/*.json`) from `tools/irgen/irgen.py`, checked in, so the
  generator needs neither Qt nor libclang. It records public and protected
  members, signals and slots (via `QT_ANNOTATE_ACCESS_SPECIFIER`), enums,
  `QFlags` typedefs, and private virtual overrides (which hide base virtuals
  from the shim).
- **Classes** (`spec/classes/<module>.yaml`): which classes are bound, with
  options `kind: value|event`, `subclass`, `notify-dtor`, `no-construct`,
  `skip-virtuals`, `raw-pointers`, `enums-only` (enums but no wrapper, e.g.
  `QChar`, which is a `rune` in Go), `reviewed`. A class's primary base must
  be bound too. Don't use class-level `reviewed`: it hides methods nobody
  looked at (it hid `QTextCursor::insertList` and `QGraphicsWidget::setLayout`).
- **Overlays** (`spec/overlays/<module>.yaml`), keyed by method name (all
  overloads) or signature (`addWidget(QWidget*, int, int, Qt::Alignment)`):

  ```yaml
  QLayout:
    addWidget:  { args: { w: transfer-to-cpp } }
    takeAt:     { return: transfer-to-go }
  ```

  Arg rules: `transfer-to-cpp`, `transfer-to-go`, `transfer-this`,
  `borrowed`, `scoped` (keyed by name or index). Return rules:
  `transfer-to-go`, `parented`, `borrowed`. An unknown rule, or a return
  rule on a result that is not an object pointer, is a generator warning.
  Other keys: `name`, `skip`, `keep-defaults`, `full`, `error`,
  `error-string`, `reviewed`, `doc`, `test`, `cpp` (a custom C++ call: `$self`, `$0`, `$1`…) and `created: true` (the
  result is built by the binding, so it is registered and its death seen).
  `QGraphicsScene::addRect` and friends use `cpp` + `created` to build
  qute's notifying item subclasses instead of Qt's, so those items report
  their death however they die.
- **Defaults** (Shiboken-style): a constructor argument named `parent` is
  `transfer-this`; other object pointers and returned objects are
  `borrowed`; value results are Owned copies; trailing default arguments are
  dropped (a constructor's `parent` is kept).
- **Naming**: methods mirror Qt (`setText` → `SetText`); constructors are
  `NewX` (parent-only form) and `NewXWith<FirstParam>`; statics are package
  functions `<Class><Method>`; signals `On<Signal>` / `Emit<Signal>`; enums
  `<Class><Value>` (Qt namespace: bare, e.g. `AlignLeft`); unnamed C++
  parameters get names from setters or their type.
- **Overloads** are named automatically, in this order: overloads identical
  in Go (a `QStringView` twin of a `QString` one) are dropped; plain-number
  twins of a struct overload (`setStart(x, y)` next to `setStart(QPointF)`)
  are dropped; the overload with the fewest parameters gets the plain name;
  the others get a suffix from what differs (`Year()` / `YearWithCalendar`,
  `ToStringFloat64`, `DrawLineSegmentsF`). A new overload never reuses a name
  the class inherits through an override (`QFile.Open` stays the
  `QIODevice` one). Overlay `name:` entries override all of this; whatever
  is still ambiguous gets a number and is listed in `COVERAGE.md`.
- **Coverage report** `COVERAGE.md` (summary table first): pointer methods on default rules,
  numbered names, skipped methods (split into missing type support, counted
  by type, and skipped on purpose), rules without a test, and results whose
  death qute can't observe. Signal emitters count as reviewed, since emitting
  never changes ownership.
- **Escape hatches**: `.Ptr()`, `XFromPtr(p, qtcore.Borrowed)`, `XCast(obj)`
  (`qobject_cast`), and hand-written `*_manual.{go,cpp,h}` files the
  generator never touches.

## Errors and ergonomics

- Programmer errors panic with what happened, the rule and the fix: dead
  handle, wrong thread, `Delete()` on Borrowed, nil receiver.
- Runtime failures return `error` where the overlay sets `error: true`
  (`QImage.Load`, `QPixmap.Save`).
- One obvious way: redundant overloads (int-coordinate twins of
  `QRect`/`QPoint` forms, `GlobalColor` twins) are skipped in overlays.
- Godoc on every generated method: the C++ signature, its ownership rule and
  a Qt docs link. Qt's summary text isn't included (no offline doc index was
  available).

## Built for AI-authored apps

- `qute.Screenshot(w)` returns an image under the offscreen platform;
  `qute.SaveScreenshot`; `go run ./examples/hello -screenshot out.png`.
- `qute.DumpTree(w)`: class, objectName, ownership, pins, geometry as JSON.
- `qute.Click`, `ClickAt`, `DoubleClick`, `Type` (handles non-ASCII), `Key`,
  `Wait`, `ProcessEvents`, over QTest.
- `qute.TestMain(m)` and `qute.Run(t, f)` run tests on the GUI thread,
  offscreen; `qute.CollectGarbage()` / `qute.Freed()` check that GC frees.
- `qute vet`: owned-leak, delete-handler, gui-goroutine, scoped-escape.
- `qute new app <dir> [module]`: main.go, a window, a behaviour test and a
  screenshot test.

## Testing and CI

- Every transfer rule gets a generated test (`*/ownership_gen_test.go`):
  create, transfer, delete the owner, assert Dead. Return rules are tested by
  calling the method and checking the result's state; borrowed object
  arguments are passed as nil. Rules the generator can't
  set up take a `test:` snippet (Go code run inside `qute.Run`), also on
  constructors. Tests that need a GL context skip with the reason when the
  driver can't provide one.
  `TestLayout` checks every primary base sits at offset 0.
- `lsan.supp` suppresses memory that GL drivers keep until exit (driver
  libraries only), for `LSAN_OPTIONS=suppressions=$PWD/lsan.supp`.
- `opengl_test.go` draws a triangle (shader program, buffer, VAO) and checks
  the pixels; `scene_test.go` checks scene items die with the scene, with
  `Clear()` and with their parent item.
- `.github/workflows/ci.yml`: Linux runs test, `qute_debug`, `-asan`,
  `GOEXPERIMENT=cgocheck2` and a regenerate-and-diff check. The macOS and
  Windows jobs are kept for later but are not yet verified; they are
  non-blocking (`continue-on-error`) until that work starts.

## Roadmap

1. Runtime: done.
2. Widgets: done for about 300 classes in five modules, including OpenGL. No
   method waits for type support, every ownership rule is reviewed, every
   overload has a proper name, and all rules but `QEvent::clone` have a test.
3. Subclassing and models: done (virtual overrides, item models).
4. Beyond: `QThread` affinity (one cleanup queue per thread), QtQuick/QML,
   more modules, Qt's summary text in doc comments, then macOS and Windows.

Known limitation: a non-QObject that Qt itself creates and frees (with no
notifying subclass qute could substitute) can't report its death. None are
bound today; `COVERAGE.md` lists any under "Results whose death qute cannot
see".

Open questions: is one eager `destroyed` connection per wrapped QObject cheap
enough, or should it be lazy? The first build takes about a minute; would
prebuilt shim archives help? Static linking and Qt's LGPL terms for
distributed binaries. Could miqt's IR or overlays save review time?

## Working in the repo

```sh
go test ./...                                   # all tests, headless
go test -tags qute_debug ./...                  # debug runtime checks
LSAN_OPTIONS=suppressions=$PWD/lsan.supp go test -asan . ./qtcore ./qtgui ./qtwidgets ./qtopengl ./qtopenglwidgets
GOEXPERIMENT=cgocheck2 go test . ./qtcore ./qtgui ./qtwidgets ./qtopengl ./qtopenglwidgets
go generate ./qtcore                            # = go run ./cmd/qutegen -root .
go vet ./... && gofmt -l .
go run ./examples/hello -screenshot out.png     # render offscreen, then Read the PNG
python3 tools/irgen/irgen.py                    # only for a new Qt version (needs python3-clang)
```

- Go 1.26 is the minimum: `golang.org/x/tools` (used by `qute vet`) needs it,
  and `go mod tidy` raises the `go` line back if it is lowered. Keep CI's
  `go-version` and the `go.mod` that `qute new` writes in step.
- A cold build of qtwidgets compiles ~14k lines of C++; `go test ./qtwidgets`
  takes 1–2 minutes cold. Use long timeouts.
- To debug a C++ crash: `go test -c -o x.test ./pkg`, then
  `QT_QPA_PLATFORM=offscreen gdb -batch -ex run -ex bt --args ./x.test -test.run Name`.
  `qute.Run` failures caused by runtime errors include the Go stack.

Layout:

- Root package `qute`: `qute.go`, `tools.{go,cpp,h}`, `testing.go`, tests
  (`tools_test.go` covers every input and inspection helper).
- `internal/rt`: runtime (hand-written).
- `qtcore`, `qtgui`, `qtwidgets`, `qtopengl`, `qtopenglwidgets`: **generated**. Never edit `*_gen.*`,
  `qute_common.h`, `ownership_gen_test.go` or `export_gen_test.go`; change
  the generator or spec and regenerate. Hand-written code is in `*_manual.*`.
- `cmd/qutegen`: `types.go` (type mapping), `model.go` (resolution, naming,
  rules, out-parameters, pairs, fields), `emit.go` (Go/C/C++ output),
  `tests.go`, `report.go`.
- `features_test.go` checks each extra type kind end to end (out-parameters,
  chaining, buffers, maps, durations, fields, callbacks).
- `cmd/qute`: `vet` and `new`. `testdata/bad/bad.go` holds `// want <rule>`
  comments that `vet_test.go` checks.

Invariants:

- Every wrapper struct starts with `rt.Ref` (roots hold it; derived types
  embed their primary base first), and code reinterprets wrapper pointers as
  `*rt.Ref`.
- Only C types cross cgo. Each generated package exports its own
  `qute_<pkg>_signal/release/virtual/dead/call` callbacks; C code never calls a
  function exported by another Go package.
- All Qt calls happen on the GUI thread; only `rt.Drain` deletes for GC.
- Generation is idempotent and warning-free. Unmatched overlay keys warn.

Conventions:

- Fixing a binding: check Qt's docs for who owns what, edit the overlay,
  `go generate ./qtcore`, run that module's tests. The generated tests have
  caught wrong rules (e.g. `QDateTimeEdit::setCalendarWidget` does not take
  ownership).
- Per-method decisions (ownership, names, skips) go in overlays, never in
  generator code; `deliberate` in `cmd/qutegen/types.go` holds only
  type-wide reasons for leaving something out. A `skip:` string is the reason
  shown in `COVERAGE.md`.
- Panic messages name the method, what happened and the fix; keep that style.
- A new exported helper in the root package or a `*_manual.go` file gets a
  test (generated code is covered by `ownership_gen_test.go` and
  `features_test.go`).
- After generator changes: regenerate, `go vet ./...`, `gofmt -l .`, and the
  affected tests.
- Ask before committing. Commit messages end with the attribution line the
  harness supplies.
