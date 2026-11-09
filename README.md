# qute

Qt 6 bindings for Go. Every Qt object has a Go handle
that tracks who owns it, so a double free or use-after-free is a clear panic
instead of a segfault. GC cleanup is on by default and safe; `Delete()` is
there when you need it to be deterministic.

```go
package main

import (
	"os"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtwidgets"
)

func main() {
	os.Exit(qute.Main(func() {
		w := qtwidgets.NewQWidget(nil)                  // Owned
		lay := qtwidgets.NewQVBoxLayout(w)              // Parented to w
		lbl := qtwidgets.NewQLabelWithText("hi", nil)   // Owned
		lay.AddWidget(lbl)                              // lbl: Owned -> Parented
		w.Show()                                        // w pinned while visible
	}))
}
```

Requirements: Go 1.24+, Qt 6 development packages, `pkg-config`, a C++17
compiler. The first build compiles the C++ shims (about a minute); after that
Go's build cache keeps rebuilds fast.

## Ownership

Each handle is in one state, changed only by annotated methods:

| State    | Who frees it                   | Example                          |
| -------- | ------------------------------ | -------------------------------- |
| Owned    | Go (GC cleanup or `Delete()`)  | `NewQWidget(nil)`                |
| Parented | its Qt parent or container     | a widget after `lay.AddWidget(w)`|
| Borrowed | someone else; Go never frees   | `QApplicationFocusWidget()`      |
| Dead     | already destroyed              | any handle after its object died |

- A dead handle panics with the class, method and fix:
  `QLabel.SetText: object destroyed (destroyed by Qt); keep its owner alive while you use it, or check Alive() first`.
  Build with `-tags qute_debug` to also get the call sites where the object
  was created and destroyed, a GUI-thread check on every call, and a panic for
  `Delete()` inside a handler.
- GC never frees a window you can see: an Owned object is pinned while it is a
  visible top-level window, has Go virtual overrides or has live signal
  connections, or after `Keep()`. GC also never deletes a QObject that has a
  parent, even if an ownership rule was missed.
- Cleanups never run `delete` on a GC goroutine: they queue onto the GUI
  thread. After the event loop exits, queued cleanups are dropped.
- `Ownership()`, `Alive()`, `Delete()`, `Keep()`, `Ptr()` exist on every object.
  `XFromPtr(p, qtcore.Borrowed)` is the unsafe escape hatch.

## API conventions

Names follow the Qt docs, so Qt C++ knowledge carries over:

| Qt C++                              | Go                                          |
| ----------------------------------- | ------------------------------------------- |
| `new QLabel(parent)`                | `qtwidgets.NewQLabel(parent)`               |
| `new QLabel("hi", parent)`          | `qtwidgets.NewQLabelWithText("hi", parent)` |
| `label->setText(s)`                 | `label.SetText(s)`                          |
| `QApplication::focusWidget()`       | `qtwidgets.QApplicationFocusWidget()`       |
| `connect(btn, &QPushButton::clicked, …)` | `btn.OnClicked(func(checked bool) { … })` |
| `emit model->dataChanged(a, b)`     | `model.EmitDataChanged(a, b)`               |
| `Qt::AlignLeft \| Qt::AlignTop`     | `qtcore.AlignLeft \| qtcore.AlignTop`       |
| `QLineEdit::Password`               | `qtwidgets.QLineEditPassword`               |
| `qobject_cast<QLabel*>(obj)`        | `qtwidgets.QLabelCast(obj)`                 |

- Trailing default arguments are dropped; a constructor's `parent` is kept.
  Pass `nil` for "no parent".
- Overloads get readable names from the overlays (`DrawRect`, `DrawRectF`,
  `AddTabIcon`). Where no name has been chosen yet, a numbered fallback
  (`Resize2`) is used and listed in `UNREVIEWED.md`.
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

Event arguments are scoped: they are Dead once the handler returns. Call
`c.Super().PaintEvent(e)` for the base implementation. Models work the same
way: embed `qtcore.QAbstractListModelBase` and define `RowCount` and `Data`.

All Qt calls happen on the GUI thread (the main goroutine inside
`qute.Main`). From other goroutines use `qute.Post(f)` or `qute.PostSync(f)`.

## Checking an app without a screen

```go
func TestMain(m *testing.M) { qute.TestMain(m) } // offscreen QApplication

func TestClick(t *testing.T) {
	qute.Run(t, func() {             // on the GUI thread
		w := NewWindow()
		defer w.Delete()
		qute.Click(w.Button)
		qute.Type(w.Name, "Ada")
		img := qute.Screenshot(w)        // image.Image, no display needed
		t.Log(qute.DumpTree(w))          // class, objectName, ownership, geometry as JSON
	})
}
```

The `qute` command (`go install github.com/nomad-software/qute/cmd/qute@latest`):

- `qute new app <dir> [module]` writes `main.go`, a window, a behaviour test
  and a screenshot test.
- `qute vet ./...` reports Owned objects with connections that are never
  deleted or parented, `Delete()` inside handlers, Qt calls from goroutines,
  and event arguments stored past their callback.

Try the example: `go run ./examples/hello`, or render it headless with
`go run ./examples/hello -screenshot out.png -tree`.

## How the bindings are made

```
Qt headers --libclang--> spec/ir/*.json --+
                         spec/classes/    +--> cmd/qutegen --> qtcore/ qtgui/ qtwidgets/
                         spec/overlays/ --+                    UNREVIEWED.md, ownership tests
```

- `tools/irgen/irgen.py` parses the Qt headers into the IR. It is checked
  in, so the generator runs without Qt or libclang.
- `spec/classes/<module>.yaml` picks the classes and their kind (object,
  value, event; `subclass`, `notify-dtor`).
- `spec/overlays/<module>.yaml` holds ownership rules per method, using
  PyQt's vocabulary:

  ```yaml
  QLayout:
    addWidget:  { args: { w: transfer-to-cpp } }
    takeAt:     { return: transfer-to-go }
  QPainter:
    drawRect(const QRect&): { name: DrawRect }
  ```

  Defaults follow Shiboken: a constructor argument named `parent` is
  `transfer-this`, other object pointers are `borrowed`, value results are
  Owned copies.
- `go generate ./qtcore` regenerates everything. Hand-written `*_manual.*`
  files are never overwritten.
- `UNREVIEWED.md` lists every pointer method still on a default rule, every
  numbered overload name, every skipped method, and every rule without a
  generated test.
- Each transfer rule gets a generated test (`*/ownership_gen_test.go`): create,
  transfer, delete the owner, check the handle is Dead. CI runs them under
  `-asan` and `GOEXPERIMENT=cgocheck2`. A wrong rule is usually a one-line
  YAML fix plus its generated test.

## Repository layout

| Path               | What                                                     |
| ------------------ | -------------------------------------------------------- |
| `internal/rt`      | runtime: handles, registry, pins, cleanup queue, threads |
| `qtcore`, `qtgui`, `qtwidgets` | generated packages (+ `*_manual` files)  |
| `.` (package `qute`) | `Main`, `Post`, `Screenshot`, `DumpTree`, `Click`, tests |
| `cmd/qutegen`      | the generator                                            |
| `cmd/qute`         | `qute vet`, `qute new app`                               |
| `spec`             | IR, class lists, ownership overlays                      |
| `examples/hello`   | a small app                                              |

## Development

```sh
go test ./...                          # everything, headless (offscreen)
go test -tags qute_debug ./...         # with thread checks and call sites
go test -asan . ./qtcore ./qtgui ./qtwidgets
GOEXPERIMENT=cgocheck2 go test . ./qtcore ./qtgui ./qtwidgets
go generate ./qtcore                   # regenerate packages, tests, UNREVIEWED.md
python3 tools/irgen/irgen.py           # re-parse headers (new Qt version only; needs python3-clang)
```

To fix a binding, edit `spec/overlays/<module>.yaml`, run `go generate
./qtcore`, then run that module's tests. To bind a new class, add it to
`spec/classes/<module>.yaml` (its primary base must be bound too).

## Status

Phases 1–3 of the roadmap in CLAUDE.md are in place: the runtime, 263 classes
across QtCore, QtGui and QtWidgets (about 6,800 constructors, methods and
functions), and Go subclassing (virtual overrides, item models). Every binding
has a reviewed ownership rule and a proper name, and every ownership rule but
`QEvent::clone` has a generated test (about 430 tests in all).

Not done yet:

- About 150 methods, mostly on recently added classes, wait for types the
  generator doesn't handle yet (`QVector3D`, `char32_t`, `QTextLength`,
  `QFileInfo`, ...). `UNREVIEWED.md` counts them by type. About 430 more are
  skipped on purpose, each with its reason (C++ move overloads, `QBindable`,
  iterators, platform internals, `QStringView` and plain-number twins).
- Items that `QGraphicsScene` creates itself (`addRect`, `addLine`, ...) are
  not QObjects, so qute can't see when the scene frees them; don't use them
  after deleting the scene.
- `QThread` affinity, QtQuick/QML and other modules.
- Doc comments link to the Qt docs but don't include Qt's summary text.
- The Linux CI checks have been run locally; the macOS and Windows jobs in
  `.github/workflows/ci.yml` are written but not yet verified.
