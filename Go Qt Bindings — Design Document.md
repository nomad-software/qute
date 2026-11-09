# Go Qt Bindings — Design Document

Nov 9, 2026 · @Gary Willoughby

## Summary

The library (working name `goqt`) tracks who owns every Qt object in a Go-side handle, so a double free or use-after-free becomes a clear panic instead of a segfault. Targets Qt 6 and Go 1.24+.

**Goals**

- No double free or dangling pointer reachable from ordinary Go code.
- Automatic cleanup by default, deterministic `Delete()` when needed.
- Ownership rules live in small spec files, not in generator code.
- A binding bug is usually a one-line spec fix plus a test.
- Apps, including ones Claude writes, can be built, tested and visually checked headlessly.

**Non-goals**

- Qt 5 support.
- QML-first API (later phase).
- Zero cgo overhead; correctness comes first.

## Lessons from prior bindings

The mature bindings (PySide, PyQt) solved ownership with per-method annotations and wrapper invalidation; the Go ones mostly left it to the user.

| Library | What worked | What hurt | What we take |
| --- | --- | --- | --- |
| [miqt](https://github.com/mappu/miqt) (Go) | Generated from headers, wide module coverage, plain `go build`, uic/rcc tools | QObjects need `Delete()` or opt-in `GoGC()`; ownership is not re-tracked when it changes later; \~10 min first build; every fix goes through the generator | Track ownership changes per method; GC on by default, made safe by pinning |
| therecipe/qt (Go) | Huge coverage, deploy tooling | Custom build tool, moc step, largely inactive | Keep the build plain |
| PySide6 / Shiboken | Typesystem files with ownership rules; wrappers invalidated when the C++ object dies | Verbose XML | Declarative spec + invalidation |
| PyQt / SIP | `/Transfer/`, `/TransferBack/`, `/TransferThis/` annotations | Separate `.sip` files to maintain | The annotation vocabulary |
| cxx-qt, qmetaobject-rs (Rust) | Thread-safe queue to the Qt thread; `QPointer`-style weak refs | Needs a build step | `RunOnMain`, weak handles |
| gotk4 (Go, GTK) | Toggle refs tie Go GC to GObject refcounts | Finalizers run off the main thread | Cleanups go through a main-thread queue |

## Architecture

Four run-time layers sit on top of a generator fed by a checked-in IR and ownership overlays.

&#91;embedded content: architecture · build time and run time\]

Only the runtime layer decides who frees what; the shim calls back into it when Qt destroys an object or runs an overridden virtual.

## Ownership and memory model

Every handle is in one ownership state, stored in the handle and changed only by annotated methods. Compile-time `Owned[T]` types were rejected: `SetParent` changes ownership at runtime, so a static type would lie.

| State | Who frees it | Go cleanup attached | Example |
| --- | --- | --- | --- |
| Owned | Go (GC cleanup or `Delete()`) | Yes | `qt.NewQWidget(nil)` |
| Parented | Qt parent / container | No | widget after `layout.AddWidget(w)` |
| Borrowed | Someone else; Go never frees | No | `app.FocusWidget()` |
| Dead | Already destroyed | No | any handle after its object is deleted |

&#91;embedded content: ownership states · 4 states, 7 transitions\]

`SetParent(p)` and `SetParent(nil)` move between Owned and Parented the same way the annotated methods do.

**Pinning.** GC must never delete a window the user is looking at. An Owned QObject is pinned (held strongly by the runtime) while it is a visible top-level window, has Go virtual overrides, or has live signal connections; `Keep()` pins it explicitly. GC frees it only when it is unpinned and unreachable. This makes GC safe to turn on by default, unlike miqt's opt-in `GoGC()`.

```go
type handle struct {
    ptr     unsafe.Pointer
    state   atomic.Uint32 // owned, parented, borrowed, dead
    pins    atomic.Int32  // > 0: held strongly, GC cannot free it
    cleanup runtime.Cleanup
}
```

Rules:

1. Constructors return Owned, or Parented when a non-nil parent is passed.
2. `transfer-to-cpp` arguments become Parented and their GC cleanup is cancelled.
3. `transfer-to-go` returns (`QLayout::takeAt`, `QTreeWidget::takeTopLevelItem`) become Owned and get a cleanup.
4. `Delete()` is idempotent and works on Owned and Parented; on Borrowed it panics.
5. Inside a signal or event handler, use `DeleteLater()`; `Delete()` there panics in debug builds, since Qt may still be using the object.
6. Arguments marked `scoped` in the spec (events, `QPainter*`, style options) are marked Dead when the callback returns. Long-lived arguments, such as the item in `currentItemChanged`, stay plain Borrowed.

A typical lifecycle:

```go
w := qt.NewQWidget(nil)                  // Owned
lay := qt.NewQVBoxLayout(w)              // Parented to w
lbl := qt.NewQLabelWithText("hi", nil)   // Owned
lay.AddWidget(lbl)                       // lbl: Owned -> Parented
w.Show()                                 // w pinned while visible
// hidden + unreachable -> GC frees w, which frees lay and lbl
```

## Object lifetime and handle safety

One C++ object maps to one Go handle, and that handle learns when the object dies.

- **Registry.** `map[unsafe.Pointer]weak.Pointer[handle]` (Go 1.24 `weak`). Pinned handles are also held in a strong set, so their Go state (overrides, closures) survives. Wrapping a pointer twice returns the same handle, so its state never diverges.
- **Death notice.** On first wrap of a QObject, the shim connects `QObject::destroyed` to a C callback. It marks the handle Dead, removes the registry entry and releases its callbacks.
- **Checked calls.** Each generated method does one atomic load; a Dead handle panics with class and method name (`QLabel.SetText on destroyed object`), never a segfault.
- **Address reuse.** The registry entry is removed before memory is freed, so a new object at the same address gets a fresh handle.
- **Non-QObject types** (`QTreeWidgetItem`, `QStandardItem`, `QGraphicsItem`) have no `destroyed` signal. Where the class has a virtual destructor, the shim subclass notifies Go from its destructor; otherwise the spec marks it Borrowed once inside a container.

## Value types and marshalling

Small value types become plain Go values at the boundary, so most data never needs freeing.

| Qt type | Go type | Freed by |
| --- | --- | --- |
| `QString`, `QByteArray` | `string`, `[]byte` | Copied, C buffer freed in the wrapper |
| `QList<T>`, `QStringList` | `[]T`, `[]string` | Copied |
| `QPoint`, `QSize`, `QRect`, `QColor`, `QMargins` | Go structs | Nothing to free |
| `QImage`, `QPixmap`, `QIcon`, `QFont`, `QVariant` | Owned handle | `runtime.AddCleanup`; copies use Qt implicit sharing |

- Go strings pass as `(ptr, len)` into `QString::fromUtf8`, which copies, so cgo pointer rules hold.
- Return buffers come back as `{data, len}` from `malloc`; the generated wrapper copies and frees in the same call. C memory never reaches user code.

## Threading model

All GUI calls run on the main OS thread, and nothing ever deletes a Qt object from a GC goroutine.

- `goqt.Main(func())` locks the main goroutine to the main OS thread (`runtime.LockOSThread`) and runs the event loop. The lock is taken in package init, while the main goroutine is still on the main thread.
- `goqt.Post(func())` and `goqt.PostSync(func())` queue work via `QMetaObject::invokeMethod(qApp, …, Qt::QueuedConnection)`.
- Cleanups never call `delete` directly. They push the pointer onto a lock-free queue that the main thread drains on a posted event.
- Build tag `goqt_debug` adds a thread check to every call and panics with the method name when called off the GUI thread.
- Objects moved to a `QThread` are a later phase; the queue design extends to one queue per thread.

**Shutdown.** Once `QApplication` has exited, queued cleanups are dropped, not run: deleting objects after the app is gone crashes inside Qt destructors.

## Signals, callbacks and subclassing

Every Go closure held by Qt has an owner and a release point, so callbacks never leak or fire into freed state.

**Signals**

```go
conn := btn.OnClicked(func(checked bool) { … })
conn.Disconnect() // optional
```

- The closure is stored as a `cgo.Handle`; the C++ lambda captures only its id.
- The sender is the lambda's context object, so Qt auto-disconnects on destroy and the handle is released then.

A closure that captures its own sender keeps an Owned sender pinned until it is disconnected, deleted or parented. So a parentless object with connections must be `Delete()`d; the vet tool flags ones that never are.

**Virtual overrides**

```go
type Canvas struct{ qt.QWidgetBase }

func (c *Canvas) PaintEvent(e *qt.QPaintEvent) {
    // draw…
    c.Super().PaintEvent(e)
}

w := qt.NewQWidgetWith(&Canvas{}, nil)
```

- Each class has a generated shim subclass (`goqt_QWidget`).
- At construction, the library type-asserts the Go value against one small interface per virtual (`PaintEventer`, …) and sets a bitmask in the shim.
- Virtuals that are not overridden skip the cgo hop entirely.
- Event arguments are Borrowed and marked Dead on return (Ownership rule 6).

## Code generator and annotation spec

Ownership is data in YAML overlays, so most binding fixes never touch the generator.

- **Input.** Qt headers parsed with libclang into a JSON IR that is checked in, so the generator runs without Qt installed.
- **Defaults** (Shiboken-style heuristics): a `QObject*` argument named `parent` is `transfer-to-cpp`; a returned `QObject*` is `borrowed`; constructors are `owned`.
- **Overlays** override defaults per method:

```yaml
QLayout:
  addWidget:  { args: { w: transfer-to-cpp } }
  takeAt:     { return: transfer-to-go }
QApplication:
  focusWidget: { return: borrowed }
QPushButton:
  QPushButton(const QString&, QWidget*): { name: NewQPushButtonWithText }
```

- **Review report.** Every method that passes or returns a pointer with only a default rule is listed in `UNREVIEWED.md`, so coverage gaps are visible.
- **Output.** One Go package per Qt module (`qtcore`, `qtgui`, `qtwidgets`); shim compiled by cgo via `pkg-config`; plain `go build`.
- **Escape hatch.** Hand-written `*_manual.go` files are never overwritten.

## Error handling and API ergonomics

Programmer errors panic with a clear message; runtime failures return `error`.

- Panics: dead handle, wrong thread, `Delete()` on Borrowed.
- Errors: calls whose Qt result signals failure (`QImage::load`, `QFile::open`) return `(T, error)` per a spec flag.
- `nil` maps to `nullptr` both ways; a returned null is never wrapped in a handle.
- Overloads get readable names from the spec; numbered fallbacks (`NewQWidget2`) only appear when unannotated and are listed in the review report.
- Enums are typed constants; flags are bitmask types with `Has()`.
- Unsafe escape hatch: `.Ptr()` and `qt.FromPtr(p, qt.Borrowed)`.

## Built for AI-authored apps

Claude will write apps on this library, so the API must be guessable, checkable, and self-correcting without a human at the screen.

- **One obvious way.** No numbered overloads or aliases; names mirror the Qt docs (`SetText`, `OnClicked`), so Qt C++ knowledge transfers directly.
- **Actionable panics.** Each message names what happened, the rule, and the fix, e.g. `QLabel.SetText: object destroyed (parent deleted at main.go:42); keep the parent or check lbl.Alive()`. Debug builds record creation and death call sites.
- **`goqt vet`.** Static checks for: Owned objects never deleted or parented, `Delete()` inside handlers, GUI calls from goroutines, scoped arguments stored past their callback.
- **Headless screenshots.** `goqt.Screenshot(w)` returns an `image.Image` under the offscreen platform, so a layout can be rendered, looked at and fixed with no display.
- **Object tree dump.** `goqt.DumpTree(w)` prints class, `objectName`, ownership state and geometry as JSON.
- **Scripted input.** Thin wrappers over `QTest` (`goqt.Click(btn)`, `goqt.Type(edit, "x")`) so UI behaviour runs in `go test`.
- **Docs where the code is.** Every generated method's godoc carries the Qt summary, its ownership rule and a Qt doc link, so `go doc` is enough.
- **Scaffold.** `goqt new app` writes `main.go`, a window, a behaviour test and a screenshot test: a known-good start.
- **Fast rebuilds.** Shim compiled once per Qt version and cached, so iterating stays in seconds.

## Testing, CI and contribution

Every ownership annotation ships with a generated test that fails under a sanitizer if the rule is wrong.

- **Generated ownership tests.** For each `transfer-*` rule: create, transfer, delete the owner, assert the handle is Dead and the sanitizer is quiet.
- **Sanitizers.** `go test -asan` with the shim built `-fsanitize=address`; LeakSanitizer on exit; `GOEXPERIMENT=cgocheck2` in a separate job.
- **Headless CI.** `QT_QPA_PLATFORM=offscreen` on Linux, macOS and Windows.
- **Contribution path.** A typical fix is one YAML line plus its generated test. Review is light, and two maintainers per module can merge.
- **Triage.** Issue template asks for the class, method and expected owner, which maps straight to an overlay entry.

## Roadmap and open questions

Build the runtime first, then widen coverage on top of it.

1. **Runtime.** Handle registry, cleanup queue, `Main`/`Post`, value-type marshalling, QtCore basics, headless screenshots and tree dump (so every later phase is checked by the same tools).
2. **Widgets.** QtWidgets with reviewed overlays for the \~100 most-used classes.
3. **Subclassing and models.** Virtual overrides, `QAbstractItemModel`, item views.
4. **Beyond.** `QThread` affinity, QtQuick/QML, more modules.

**Open questions**

- Is a `destroyed` connection per wrapped QObject cheap enough, or should it be lazy?
- Pinning on visibility needs a show/hide hook per top-level window; is an event filter on `QApplication` enough?
- Should `PostSync` exist at all, given deadlock risk?
- How to keep first-build time well under miqt's \~10 minutes: prebuilt shim archives, or fewer generated symbols?
- Static linking and Qt's LGPL terms for distributed binaries.
- Can miqt's IR or overlays be reused to save phase 2 time?

## Sources

- [miqt README and FAQ](https://github.com/mappu/miqt): `GoGC()`, finalizers for by-value types, first-build time.
- Other rows in the lessons table are from general knowledge of those projects and were not re-checked for this doc.
