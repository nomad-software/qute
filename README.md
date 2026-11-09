# qute

Qt 6 bindings for Go (working name `goqt`). Every Qt object has a Go handle
that tracks who owns it, so a double free or use-after-free is a clear panic
instead of a segfault. GC cleanup is on by default and safe; `Delete()` is
there when you need it to be deterministic.

```go
package main

import (
	"os"

	"github.com/nomad-software/qute/goqt"
	"github.com/nomad-software/qute/qtwidgets"
)

func main() {
	os.Exit(goqt.Main(func() {
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
  Build with `-tags goqt_debug` to also get the call sites where the object
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
`goqt.Main`). From other goroutines use `goqt.Post(f)` or `goqt.PostSync(f)`.

## Checking an app without a screen

```go
func TestMain(m *testing.M) { goqt.TestMain(m) } // offscreen QApplication

func TestClick(t *testing.T) {
	goqt.Run(t, func() {             // on the GUI thread
		w := NewWindow()
		defer w.Delete()
		goqt.Click(w.Button)
		goqt.Type(w.Name, "Ada")
		img := goqt.Screenshot(w)        // image.Image, no display needed
		t.Log(goqt.DumpTree(w))          // class, objectName, ownership, geometry as JSON
	})
}
```

The `goqt` command:

- `goqt new app <dir> [module]` writes `main.go`, a window, a behaviour test
  and a screenshot test.
- `goqt vet ./...` reports Owned objects with connections that are never
  deleted or parented, `Delete()` inside handlers, Qt calls from goroutines,
  and event arguments stored past their callback.

Try the example: `go run ./examples/hello`, or render it headless with
`go run ./examples/hello -screenshot out.png -tree`.

## How the bindings are made

```
Qt headers --libclang--> spec/ir/*.json --+
                         spec/classes/    +--> cmd/goqtgen --> qtcore/ qtgui/ qtwidgets/
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
| `goqt`             | `Main`, `Post`, `Screenshot`, `DumpTree`, `Click`, tests |
| `cmd/goqtgen`      | the generator                                            |
| `cmd/goqt`         | `goqt vet`, `goqt new app`                               |
| `spec`             | IR, class lists, ownership overlays                      |
| `examples/hello`   | a small app                                              |

## Status

Phases 1–3 of the design are in place: the runtime, about 170 classes across
QtCore, QtGui and QtWidgets, and Go subclassing (virtual overrides, item
models). Not done yet: `QThread` affinity, QtQuick/QML, more modules, and
reviewing the remaining entries in `UNREVIEWED.md`. Doc comments link to the
Qt docs but don't include Qt's summary text, because the Qt doc index isn't
available offline here. The Linux CI matrix has been run locally; the macOS
and Windows jobs are written but not yet verified.
