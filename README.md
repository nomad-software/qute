# qute

Qt 6 bindings for Go. Every Qt object has a Go handle that tracks who owns it,
so a double free or use-after-free is a clear panic instead of a segfault. GC
cleanup is on by default and safe, and `Delete()` is there when you need
cleanup to be deterministic. Apps can be built, tested, screenshotted and
inspected headlessly, which makes qute a good fit for desktop apps written
with a coding agent.

```go
package main

import (
	"os"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtwidgets"
)

func main() {
	os.Exit(qute.Main(func() {
		w := qtwidgets.NewQWidget(nil)                // Owned
		lay := qtwidgets.NewQVBoxLayout(w)            // Parented to w
		lbl := qtwidgets.NewQLabelWithText("hi", nil) // Owned
		lay.AddWidget(lbl)                            // lbl: Owned -> Parented
		w.Show()                                      // w pinned while visible
	}))
}
```

Requirements: Go 1.26+, Qt 6 development packages, `pkg-config` and a C++17
compiler. Linux is the supported platform for now. The first build compiles
the C++ shims, which takes about a minute; after that Go's build cache keeps
rebuilds fast.

## Why qute

- **Mistakes panic and say what to do.** Calling a method on a widget whose
  parent has already freed it is the classic Qt crash. In qute it is a Go
  panic that names the method, what happened and the fix:
  `QLabel.SetText: object destroyed (destroyed by Qt); keep its owner alive
  while you use it, or check Alive() first`. Ownership mistakes don't corrupt
  memory.
- **Memory management that works like Go.** Objects you create are freed by
  the GC once they are unreachable. Objects handed to Qt (added to a layout,
  given a parent) are left for Qt to free. Visible windows, Go subclasses and
  objects with live signal connections are pinned, so the GC never frees
  something on screen. `Delete()` is there when you want it.
- **Your Qt knowledge carries over.** Names follow the Qt docs (`setText` is
  `SetText`, `QLineEdit::Password` is `QLineEditPassword`), and every
  generated method's doc comment gives the C++ signature, its ownership rule
  and a link to the Qt page.
- **Idiomatic Go at the edges.** `QString` is `string`, `QList<T>` is a slice,
  `QPoint`/`QRect`/`QColor` are plain structs, out-parameters are extra
  results, failures return `error`, and `std::function` parameters take Go
  funcs.
- **Broad coverage.** About 300 classes across QtCore, QtGui, QtWidgets,
  QtOpenGL and QtOpenGLWidgets: over 8,200 constructors, methods and
  functions, plus subclassing (virtual overrides, item models,
  `QOpenGLWidget`).
- **Checked, not hoped.** Every ownership rule lives in a small YAML file
  and has a generated test (create, transfer, delete the owner, check the
  handle is Dead). CI runs those tests under AddressSanitizer and
  `cgocheck2`. `COVERAGE.md` lists every method left out and why.
- **Plain `go build`.** One cgo package per Qt module, found through
  `pkg-config`. You don't need a separate build system or code generation
  step in your app.

## Building apps with a coding agent

GUI code is hard for an agent to get right without seeing the result: it
can't watch the window, and with most C++ bindings a mistake is a segfault
with no clue where it came from. qute gives an agent the same loop it has for
any other Go code (write, run the tests, read the output, fix) and adds a way
to see the UI:

- **Everything runs headlessly.** Tests use Qt's offscreen platform, so
  `go test ./...` needs no display, no X server and no window manager. It
  works the same in a container, in CI or in an agent's sandbox.
- **The agent can look at the window.** `qute.Screenshot(w)` renders any
  widget to an `image.Image`, and `qute.SaveScreenshot(w, "out.png")` writes a
  PNG an agent can open and inspect. Layout bugs (clipped text, a widget the
  wrong size, a missing button) are visible without a person at the screen.
- **The agent can read the window.** `qute.DumpTree(w)` returns the object
  tree as JSON: class, `objectName`, ownership state, pins, geometry and
  visibility. It is a precise, text-only view of what is on screen.
- **The agent can use the window.** `qute.Click`, `ClickAt`, `DoubleClick`,
  `Type` (including non-ASCII text), `Key` and `Wait` drive real Qt input
  events through QTest, so behaviour tests exercise signals and handlers the
  way a user would.
- **Errors explain themselves.** Panics name the method, say what happened,
  give the fix and, where an ownership rule applies, cite its number.
  `qute.Run` turns a panic into a test failure (with the Go stack for runtime
  errors) instead of crashing the test binary. An agent can act on the
  message alone. With `-tags qute_debug` the messages also give the call
  sites where the object was created and destroyed, and every call checks it
  is on the GUI thread.
- **Static checks catch the common mistakes.** `qute vet ./...` reports
  leaked Owned objects, `Delete()` inside a handler, Qt calls from a goroutine
  and event arguments kept past their callback.
- **A known-good starting point.** `qute new app <dir>` writes a window, a
  `main.go`, a behaviour test and a screenshot test that pass from the start.
- **The API is predictable.** Names map one-to-one from the Qt docs an agent
  already knows, and there is one obvious way to do each thing: redundant
  overloads are left out and the rest get descriptive names.

A typical loop looks like this:

```sh
qute new app ./myapp && cd myapp      # window, main.go, tests
go test ./...                          # behaviour + screenshot tests, headless
go test -run Screenshot -v .           # logs the PNG path and the object tree
qute vet ./...                         # ownership and threading checks
```

A test that both checks behaviour and leaves something to look at:

```go
func TestMain(m *testing.M) { qute.TestMain(m) } // offscreen QApplication

func TestClick(t *testing.T) {
	qute.Run(t, func() { // runs on the GUI thread
		w := NewWindow()
		defer w.Delete()
		qute.Click(w.Button)
		qute.Type(w.Name, "Ada")
		if got := w.Greeting.Text(); got != "Hello, Ada" {
			t.Errorf("greeting = %q", got)
		}
		qute.SaveScreenshot(w, filepath.Join(t.TempDir(), "window.png"))
		t.Log(qute.DumpTree(w)) // class, objectName, ownership, geometry as JSON
	})
}
```

For your own project's agent instructions (`CLAUDE.md`, `AGENTS.md`), these
lines are a good start:

```markdown
- GUI code uses qute (Qt 6 for Go). Run `go test ./...`; tests are headless.
- To check a layout, save a screenshot with qute.SaveScreenshot and open
  the PNG; use qute.DumpTree for exact geometry.
- Drive the UI in tests with qute.Click/Type/Key inside qute.Run.
- Run `qute vet ./...` before finishing. Inside handlers use DeleteLater,
  never Delete. From goroutines use qute.Post.
```

## Ownership

Each handle is in one state, changed only by annotated methods:

| State    | Who frees it                   | Example                           |
| -------- | ------------------------------ | --------------------------------- |
| Owned    | Go (GC cleanup or `Delete()`)  | `NewQWidget(nil)`                 |
| Parented | its Qt parent or container     | a widget after `lay.AddWidget(w)` |
| Borrowed | someone else; Go never frees   | `QApplicationFocusWidget()`       |
| Dead     | already destroyed              | any handle after its object died  |

- Using a Dead handle panics with the class, method and fix. Build with
  `-tags qute_debug` to also get the creation and destruction call sites, a
  GUI-thread check on every call, and a panic for `Delete()` inside a
  handler.
- GC never frees a window you can see. An Owned object is pinned while it is
  a visible top-level window, while it has Go virtual overrides or live signal
  connections, and after `Keep()` until `Unkeep()`. GC also never deletes a
  QObject that has a parent, even if an ownership rule was missed.
- An Owned object with no parent but with signal connections stays pinned, so
  `Delete()` it when done (`qute vet` flags ones that never are).
- GC cleanups never run `delete` on a GC goroutine. They queue onto the GUI
  thread, and after the event loop exits, queued cleanups are dropped.
- `Ownership()`, `Alive()`, `Delete()`, `Keep()`, `Unkeep()` and `Ptr()` exist
  on every object. `XFromPtr(p, qtcore.Borrowed)` is the unsafe escape hatch.

## API conventions

| Qt C++                                   | Go                                          |
| ---------------------------------------- | ------------------------------------------- |
| `new QLabel(parent)`                     | `qtwidgets.NewQLabel(parent)`               |
| `new QLabel("hi", parent)`               | `qtwidgets.NewQLabelWithText("hi", parent)` |
| `label->setText(s)`                      | `label.SetText(s)`                          |
| `QApplication::focusWidget()`            | `qtwidgets.QApplicationFocusWidget()`       |
| `connect(btn, &QPushButton::clicked, …)` | `btn.OnClicked(func(checked bool) { … })`   |
| `emit model->dataChanged(a, b)`          | `model.EmitDataChanged(a, b)`               |
| `Qt::AlignLeft \| Qt::AlignTop`          | `qtcore.AlignLeft \| qtcore.AlignTop`       |
| `QLineEdit::Password`                    | `qtwidgets.QLineEditPassword`               |
| `qobject_cast<QLabel*>(obj)`             | `qtwidgets.QLabelCast(obj)`                 |

- Trailing default arguments are dropped; a constructor's `parent` is kept.
  Pass `nil` for "no parent".
- Overloads get descriptive names: the one with the fewest parameters gets
  the plain name and the others a suffix for what differs (`Year` /
  `YearWithCalendar`, `DrawRect` / `DrawRectF`).
- An argument of type `QWidgetLike` accepts any widget (`*QLabel`,
  `*QPushButton`, your own subclass). Pass untyped `nil`, not a typed nil.
- `QString`, `QByteArray` and `QStringList` are `string`, `[]byte` and
  `[]string`. `QPoint`, `QSize`, `QRect`, `QMargins` and `QColor` are plain
  Go structs. `QImage`, `QFont`, `QIcon`, `QVariant` and other value classes
  are Owned handles that GC frees. `qtcore.NewQVariantOf(v)` and
  `(*QVariant).Value()` convert to and from Go values.
- Calls whose Qt result signals failure return `error`, e.g.
  `img.Load(path)`.
- Out-parameters become results: `l, t, r, b := layout.GetContentsMargins()`.
  A pointer and its count become one slice (`f.Read(buf)`), `QMap`/`QHash`
  become Go maps, `std::chrono` durations and times become `time.Duration` and
  `time.Time`, `std::filesystem::path` is a `string`, and `std::function`
  parameters take a Go func.
- Methods that return the object itself chain: `t.Translate(10, 20).Rotate(45)`.
- Public data members get getters and setters (`opt.Rect()`, `opt.SetRect(r)`),
  and nested classes get flat names (`QFont::Tag` is `qtgui.QFontTag`).

## Signals, subclassing, threads

```go
conn := btn.OnClicked(func(checked bool) { ... })
conn.Disconnect() // optional; the connection also ends when btn dies

type Canvas struct{ qtwidgets.QWidgetBase }

func (c *Canvas) PaintEvent(e *qtgui.QPaintEvent) {
	p := qtgui.NewQPainterOn(c)
	defer p.Delete()
	p.FillRect(e.Rect(), qtgui.RGB(200, 30, 30))
}

qtwidgets.NewQWidgetWith(&Canvas{}, nil)
```

Event arguments are scoped: they are Dead once the handler returns, so copy
out the values you need. Call `c.Super().PaintEvent(e)` for the base
implementation. Models work the same way: embed
`qtcore.QAbstractListModelBase` and define `RowCount` and `Data`.

All Qt calls happen on the GUI thread (the main goroutine inside
`qute.Main`). From other goroutines use `qute.Post(f)` or `qute.PostSync(f)`.

## OpenGL

`QOpenGLWidget` is subclassed the same way. GL calls go through the context's
function table, with the `GL_*` constants in `qtgui`. The helpers
(`QOpenGLShaderProgram`, `QOpenGLBuffer`, `QOpenGLVertexArrayObject`,
`QOpenGLTexture`, framebuffer objects) are in `qtopengl`:

```go
type Canvas struct{ qtopenglwidgets.QOpenGLWidgetBase }

func (c *Canvas) InitializeGL() { /* build shaders and buffers */ }

func (c *Canvas) PaintGL() {
	gl := c.Context().Functions()
	gl.GlClearColor(0, 0.5, 1, 1)
	gl.GlClear(qtgui.GL_COLOR_BUFFER_BIT)
}

w := qtopenglwidgets.NewQOpenGLWidgetWith(&Canvas{}, nil)
img := w.GrabFramebuffer() // works headlessly with the offscreen platform
```

GL pointer arguments (`glBufferData`, `glGenBuffers`, …) are `unsafe.Pointer`,
because GL decides from other arguments how much memory it reads or writes;
keep that memory alive for the call. Uniforms use the location-based setters:
`p.SetUniformMatrix4x4(p.UniformLocation("mvp"), m)`. `opengl_test.go` draws
a triangle with a shader program, a vertex buffer and a VAO.

## The qute command

`go install github.com/nomad-software/qute/cmd/qute@latest`

- `qute new app <dir> [module]` writes `main.go`, a window, a behaviour test
  and a screenshot test.
- `qute vet ./...` reports Owned objects with connections that are never
  deleted or parented, `Delete()` inside handlers, Qt calls from goroutines,
  and event arguments stored past their callback.

Try the example: `go run ./examples/hello`, or render it headlessly with
`go run ./examples/hello -screenshot out.png -tree`.

## How the bindings are made

```
Qt headers --libclang--> spec/ir/*.json --+
                         spec/classes/    +--> cmd/qutegen --> qtcore/ qtgui/ qtwidgets/ qtopengl/ qtopenglwidgets/
                         spec/overlays/ --+                    COVERAGE.md, ownership tests
```

- `tools/irgen/irgen.py` parses the Qt headers into the IR. The IR is
  checked in, so the generator runs without Qt or libclang.
- `spec/classes/<module>.yaml` picks the classes and their options (`kind:
  value|event`, `subclass`, `notify-dtor`, ...).
- `spec/overlays/<module>.yaml` holds the ownership rules per method, using
  PyQt's vocabulary:

  ```yaml
  QLayout:
    addWidget:  { args: { w: transfer-to-cpp } }
    takeAt:     { return: transfer-to-go }
  QPainter:
    drawLines(const QList<QLineF>&): { name: DrawLineSegmentsF }
  ```

  The defaults follow Shiboken: a constructor argument named `parent` is
  `transfer-this`, other object pointers are `borrowed`, and value results
  are Owned copies.
- `go generate ./qtcore` regenerates everything. Hand-written `*_manual.*`
  files are never overwritten.
- `COVERAGE.md` records what is bound, every method skipped on purpose with
  its reason, and anything still open. A summary table at the top shows the
  counts.
- A wrong rule is usually a one-line YAML fix plus its generated test.

## Repository layout

| Path                 | What                                                       |
| -------------------- | ---------------------------------------------------------- |
| `.` (package `qute`) | `Main`, `Post`, `Screenshot`, `DumpTree`, `Click`, testing |
| `internal/rt`        | runtime: handles, registry, pins, cleanup queue, threads   |
| `qtcore`, `qtgui`, `qtwidgets`, `qtopengl`, `qtopenglwidgets` | generated packages (plus `*_manual` files) |
| `cmd/qutegen`        | the generator                                              |
| `cmd/qute`           | `qute vet`, `qute new app`                                 |
| `spec`               | IR, class lists, ownership overlays                        |
| `examples/hello`     | a small app                                                |

## Development

```sh
go test ./...                          # everything, headless (offscreen)
go test -tags qute_debug ./...         # with thread checks and call sites
LSAN_OPTIONS=suppressions=$PWD/lsan.supp go test -asan . ./qtcore ./qtgui ./qtwidgets ./qtopengl ./qtopenglwidgets
GOEXPERIMENT=cgocheck2 go test . ./qtcore ./qtgui ./qtwidgets ./qtopengl ./qtopenglwidgets
go generate ./qtcore                   # regenerate packages, tests, COVERAGE.md
python3 tools/irgen/irgen.py           # re-parse headers (new Qt version only; needs python3-clang)
```

`lsan.supp` silences memory that GL drivers keep until exit. It lists only
driver libraries, so qute's own allocations are still checked.

To fix a binding, edit `spec/overlays/<module>.yaml`, run `go generate
./qtcore`, then run that module's tests. To bind a new class, add it to
`spec/classes/<module>.yaml` (its primary base must be bound too).

## Status

The runtime, the widget bindings and Go subclassing are done. No method is
waiting for type support, every ownership rule has been reviewed, every
overload has a proper name, and every rule but `QEvent::clone` has a
generated test (over 430 in all).

Not done yet:

- About 600 methods are skipped on purpose, each with its reason in
  `COVERAGE.md`: C++ move overloads, `QBindable`, iterators, Qt's private
  and platform internals, `QStringView` and plain-number twins, printing page
  setup, accessibility, Vulkan, and interfaces that need C++ subclassing.
- `QThread` affinity, QtQuick/QML and other modules.
- Doc comments link to the Qt docs but don't include Qt's summary text.
- Linux comes first for now; macOS and Windows support comes later. Their
  jobs in `.github/workflows/ci.yml` are kept, unverified and non-blocking.
