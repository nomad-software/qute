package qute_test

import (
	"strings"
	"testing"
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

// Per-object overrides (OverrideX) replace a virtual of one object made by
// a plain constructor, with no Go subclass type.

func handleOf[T any](w *T) *rt.Handle { return rt.RefOf(unsafe.Pointer(w)).H }

// An event virtual: the function runs instead of the C++ code, super runs
// the C++ code, and nil restores it.
func TestOverrideEvent(t *testing.T) {
	qutetest.Run(t, func() {
		e := qtwidgets.NewQLineEdit(nil)
		defer e.Delete()
		if !handleOf(e).IsShim() {
			t.Fatal("NewQLineEdit did not build the shim")
		}
		var seen []string
		// OverrideKeyPressEvent is declared on QWidget and promoted; it must
		// reach QLineEdit's slot for the virtual and QLineEdit's super
		e.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), ev *qtgui.QKeyEvent) {
			seen = append(seen, ev.Text())
			if ev.Text() != "x" {
				super(ev) // swallow x
			}
		})
		e.Show()
		qutetest.Type(e, "axb")
		if strings.Join(seen, "") != "axb" {
			t.Errorf("override saw %q, want every key", seen)
		}
		if e.Text() != "ab" {
			t.Errorf("text %q: super did not run QLineEdit's code, or x was not swallowed", e.Text())
		}
		if n := handleOf(e).OverrideCount(); n != 1 {
			t.Errorf("OverrideCount = %d", n)
		}
		e.OverrideKeyPressEvent(nil)
		qutetest.Type(e, "x")
		if e.Text() != "abx" || len(seen) != 3 {
			t.Errorf("after OverrideKeyPressEvent(nil): text %q, %d calls; want the C++ code back", e.Text(), len(seen))
		}
		if n := handleOf(e).OverrideCount(); n != 0 {
			t.Errorf("OverrideCount after nil = %d", n)
		}
	})
}

// A virtual with a result, called from C++ and through the Go binding.
func TestOverrideResult(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		defer w.Delete()
		var base qtcore.QSize
		w.OverrideSizeHint(func(super func() qtcore.QSize) qtcore.QSize {
			base = super()
			return qtcore.QSize{Width: 123, Height: 45}
		})
		if got := w.SizeHint(); got != (qtcore.QSize{Width: 123, Height: 45}) {
			t.Errorf("SizeHint() = %v", got)
		}
		if base != (qtcore.QSize{Width: -1, Height: -1}) {
			t.Errorf("super() = %v, want QWidget's invalid hint", base)
		}
		// a layout asks the virtual from C++
		win := qtwidgets.NewQWidget(nil)
		defer win.Delete()
		lay := qtwidgets.NewQVBoxLayout(win)
		lay.SetContentsMargins(qtcore.QMargins{})
		child := qtwidgets.NewQWidget(nil)
		child.OverrideSizeHint(func(func() qtcore.QSize) qtcore.QSize { return qtcore.QSize{Width: 77, Height: 66} })
		lay.AddWidget(child)
		if got := lay.SizeHint(); got.Height != 66 {
			t.Errorf("layout's SizeHint %v does not include the child's overridden hint", got)
		}
	})
}

// A plain (non-QObject) class built by its constructor takes overrides too:
// QTreeWidgetItem::text calls the virtual data.
func TestOverridePlainObject(t *testing.T) {
	qutetest.Run(t, func() {
		it := qtwidgets.NewQTreeWidgetItemWithStrings([]string{"stored"})
		defer it.Delete()
		it.OverrideData(func(super func(int, int) *qtcore.QVariant, column, role int) *qtcore.QVariant {
			if role == int(qtcore.DisplayRole) {
				return qtcore.NewQVariantOf("computed " + super(column, role).ToString())
			}
			return super(column, role)
		})
		if got := it.Text(0); got != "computed stored" {
			t.Errorf("Text(0) = %q", got)
		}
	})
}

// A pure virtual has no C++ code: super is nil.
type listOnly struct{ qtcore.QAbstractListModelBase }

func TestOverridePureVirtual(t *testing.T) {
	qutetest.Run(t, func() {
		m := qtcore.NewQAbstractListModelWith(&listOnly{}, nil)
		defer m.Delete()
		superNil := false
		m.OverrideRowCount(func(super func(*qtcore.QModelIndex) int, parent *qtcore.QModelIndex) int {
			superNil = super == nil
			return 3
		})
		if got := m.RowCount(); got != 3 {
			t.Errorf("RowCount() = %d", got)
		}
		if !superNil {
			t.Error("super of a pure virtual is not nil")
		}
	})
}

// Objects Qt made are not qute's shim: the setter panics with the
// alternative, as does a virtual the object's Go type already defines.
func TestOverrideMisuse(t *testing.T) {
	qutetest.Run(t, func() {
		c := qtwidgets.NewQComboBox(nil)
		defer c.Delete()
		view := c.View() // made by Qt
		msg := panicText(func() {
			view.OverrideKeyPressEvent(func(func(*qtgui.QKeyEvent), *qtgui.QKeyEvent) {})
		})
		if !strings.Contains(msg, "not made by a qute constructor") || !strings.Contains(msg, "event filter") {
			t.Errorf("override on a Qt-made object: panic %q", msg)
		}

		v := &keyCanvas{}
		w := qtwidgets.NewQWidgetWith(v, nil)
		defer w.Delete()
		msg = panicText(func() {
			w.OverrideKeyPressEvent(func(func(*qtgui.QKeyEvent), *qtgui.QKeyEvent) {})
		})
		if !strings.Contains(msg, "already defines KeyPressEvent") {
			t.Errorf("override of a method the Go type defines: panic %q", msg)
		}
		// a virtual the type does not define can be set, and both run
		var sized bool
		w.OverrideSizeHint(func(super func() qtcore.QSize) qtcore.QSize { sized = true; return super() })
		w.Show()
		qutetest.Type(w, "k")
		_ = w.SizeHint()
		if !v.pressed || !sized {
			t.Errorf("type's KeyPressEvent ran %v, per-object SizeHint ran %v", v.pressed, sized)
		}

		d := qtwidgets.NewQWidget(nil)
		d.Delete()
		msg = panicText(func() { d.OverrideSizeHint(nil) })
		if !strings.Contains(msg, "OverrideSizeHint") {
			t.Errorf("override on a deleted object: panic %q", msg)
		}
	})
}

type keyCanvas struct {
	qtwidgets.QWidgetBase
	pressed bool
}

func (k *keyCanvas) KeyPressEvent(*qtgui.QKeyEvent) { k.pressed = true }

// Delete() inside an override is refused as in any handler (rule 5), and
// its event is scoped: Dead when the override returns.
func TestOverrideIsHandler(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		defer w.Delete()
		var kept *qtgui.QKeyEvent
		var msg string
		w.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), e *qtgui.QKeyEvent) {
			kept = e
			msg = panicText(func() { w.Delete() })
		})
		w.Show()
		qutetest.Key(w, qtcore.Key_Space, qtcore.NoModifier)
		if !strings.Contains(msg, "DeleteLater") {
			t.Errorf("Delete() in an override: panic %q, want rule 5", msg)
		}
		if kept == nil || kept.Alive() {
			t.Error("the override's event is still alive after it returned")
		}
	})
}

// An override is Go state of its object, held like a connection: a child's
// override that refers to its window does not keep the window from being
// collected, a parentless object is held while it has one, and removing it
// lets the object go.
func TestOverrideCollection(t *testing.T) {
	const n = 20
	before := qutetest.Freed()
	qutetest.Run(t, func() {
		for range n {
			win := qtwidgets.NewQWidget(nil)
			child := qtwidgets.NewQWidget(win)
			child.OverrideSizeHint(func(super func() qtcore.QSize) qtcore.QSize {
				_ = win.ObjectName()
				return super()
			})
		}
	})
	qutetest.CollectGarbage()
	if got := qutetest.Freed() - before; got < n {
		t.Errorf("freed %d of %d windows whose child's override refers to them", got, n)
	}

	var held, released unsafe.Pointer // C++ addresses
	qutetest.Run(t, func() {
		a := qtwidgets.NewQWidget(nil)
		a.OverrideSizeHint(func(super func() qtcore.QSize) qtcore.QSize { return super() })
		held = a.Ptr()
		b := qtwidgets.NewQWidget(nil)
		b.OverrideSizeHint(func(super func() qtcore.QSize) qtcore.QSize { return super() })
		b.OverrideSizeHint(nil)
		released = b.Ptr()
	})
	qutetest.CollectGarbage()
	qutetest.Run(t, func() {
		h := rt.Lookup(held)
		if h == nil {
			t.Error("a parentless widget with an override was collected while the override needs it")
		} else {
			w := qtwidgets.QWidgetFromPtr(held, qtcore.Borrowed)
			w.OverrideSizeHint(nil)
			w.Delete()
		}
		if rt.Lookup(released) != nil {
			t.Error("a widget whose only override was removed is still held")
		}
	})
}
