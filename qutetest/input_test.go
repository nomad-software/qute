package qutetest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

func TestMain(m *testing.M) { qutetest.TestMain(m) }

// mouseRecorder records where it was pressed and how often it was
// double-clicked.
type mouseRecorder struct {
	qtwidgets.QWidgetBase
	presses []qtcore.QPoint
	doubles int
}

func (m *mouseRecorder) MousePressEvent(e *qtgui.QMouseEvent) {
	m.presses = append(m.presses, e.Pos()) // copy the value; e is Dead after return
}

func (m *mouseRecorder) MouseDoubleClickEvent(e *qtgui.QMouseEvent) { m.doubles++ }

func TestClickAtAndDoubleClick(t *testing.T) {
	qutetest.Run(t, func() {
		rec := &mouseRecorder{}
		w := qtwidgets.NewQWidgetWith(rec, nil)
		defer w.Delete()
		w.Resize(100, 80)

		qutetest.ClickAt(w, qtcore.LeftButton, qtcore.QPoint{X: 7, Y: 11})
		if len(rec.presses) != 1 || rec.presses[0] != (qtcore.QPoint{X: 7, Y: 11}) {
			t.Errorf("ClickAt: presses = %v, want [{7 11}]", rec.presses)
		}
		// (0, 0) is the corner, not QTest's "centre"
		qutetest.ClickAt(w, qtcore.LeftButton, qtcore.QPoint{})
		if len(rec.presses) != 2 || rec.presses[1] != (qtcore.QPoint{}) {
			t.Errorf("ClickAt(0, 0): presses = %v, want [{7 11} {0 0}]", rec.presses)
		}
		qutetest.DoubleClick(w)
		if rec.doubles != 1 {
			t.Errorf("DoubleClick: %d double-click events, want 1", rec.doubles)
		}
	})
}

func TestKeyWithModifiers(t *testing.T) {
	qutetest.Run(t, func() {
		e := qtwidgets.NewQLineEdit(nil)
		defer e.Delete()
		qutetest.Type(e, "select me")
		qutetest.Key(e, qtcore.Key_A, qtcore.ControlModifier)
		if got := e.SelectedText(); got != "select me" {
			t.Errorf("after Ctrl+A, SelectedText() = %q", got)
		}
	})
}

func TestWaitRunsTimers(t *testing.T) {
	qutetest.Run(t, func() {
		timer := qtcore.NewQTimer(nil)
		defer timer.Delete()
		fired := 0
		timer.OnTimeout(func() { fired++ })
		timer.SetSingleShot(true)
		timer.StartWithInterval(1)
		qutetest.Wait(50)
		if fired != 1 {
			t.Errorf("timer fired %d times during Wait, want 1", fired)
		}
	})
}

// dragRecorder records the mouse events a drag sends: each event's kind,
// position and the buttons held.
type dragRecorder struct {
	qtwidgets.QWidgetBase
	log []string
}

func (d *dragRecorder) record(kind string, e *qtgui.QMouseEvent) {
	p := e.Pos()
	d.log = append(d.log, fmt.Sprintf("%s %d,%d %d", kind, p.X, p.Y, e.Buttons()))
}

func (d *dragRecorder) MousePressEvent(e *qtgui.QMouseEvent)   { d.record("press", e) }
func (d *dragRecorder) MouseMoveEvent(e *qtgui.QMouseEvent)    { d.record("move", e) }
func (d *dragRecorder) MouseReleaseEvent(e *qtgui.QMouseEvent) { d.record("release", e) }

// Press, Move and Release send one event each, and a move between a press
// and its release carries the held button, also outside the widget; Drag
// does all three, moving in steps.
func TestPressMoveRelease(t *testing.T) {
	qutetest.Run(t, func() {
		rec := &dragRecorder{}
		w := qtwidgets.NewQWidgetWith(rec, nil)
		defer w.Delete()
		w.Resize(100, 80)
		w.Show()

		qutetest.Press(w, qtcore.LeftButton, 0, qtcore.QPoint{X: 5, Y: 6})
		qutetest.Move(w, qtcore.QPoint{X: 150, Y: 7})
		qutetest.Release(w, qtcore.LeftButton, 0, qtcore.QPoint{X: 150, Y: 7})
		want := []string{"press 5,6 1", "move 150,7 1", "release 150,7 0"}
		if !slices.Equal(rec.log, want) {
			t.Errorf("Press, Move, Release: %q, want %q", rec.log, want)
		}

		rec.log = nil
		qutetest.Drag(w, qtcore.RightButton, qtcore.QPoint{X: 0, Y: 0}, qtcore.QPoint{X: 50, Y: -50})
		want = []string{"press 0,0 2", "move 10,-10 2", "move 20,-20 2", "move 30,-30 2", "move 40,-40 2", "move 50,-50 2", "release 50,-50 0"}
		if !slices.Equal(rec.log, want) {
			t.Errorf("Drag: %q, want %q", rec.log, want)
		}
	})
}

// A splitter handle dragged with Drag moves the splitter, as a user's drag
// would.
func TestDragSplitter(t *testing.T) {
	qutetest.Run(t, func() {
		s := qtwidgets.NewQSplitter(nil)
		defer s.Delete()
		s.AddWidget(qtwidgets.NewQWidget(nil))
		s.AddWidget(qtwidgets.NewQWidget(nil))
		s.Resize(200, 100)
		s.SetSizes([]int{100, 100})
		s.Show()
		qutetest.Wait(10)
		h := s.Handle(1)
		mid := qtcore.QPoint{X: h.Width() / 2, Y: h.Height() / 2}
		qutetest.Drag(h, qtcore.LeftButton, mid, qtcore.QPoint{X: mid.X - 40, Y: mid.Y})
		if got := s.Sizes(); len(got) != 2 || got[0] >= 100 {
			t.Errorf("sizes after dragging the handle left: %v", got)
		}
	})
}

func TestWaitFor(t *testing.T) {
	qutetest.Run(t, func() {
		timer := qtcore.NewQTimer(nil)
		defer timer.Delete()
		fired := false
		timer.OnTimeout(func() { fired = true })
		timer.SetSingleShot(true)
		timer.StartWithInterval(20)
		start := time.Now()
		if !qutetest.WaitFor(func() bool { return fired }, 5*time.Second) {
			t.Error("WaitFor: the timer never fired")
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("WaitFor took %v for a 20ms timer", d)
		}
		start = time.Now()
		if qutetest.WaitFor(func() bool { return false }, 30*time.Millisecond) {
			t.Error("WaitFor of a false condition returned true")
		}
		if d := time.Since(start); d < 30*time.Millisecond || d > time.Second {
			t.Errorf("WaitFor of a false condition took %v, want about 30ms", d)
		}
		calls := 0
		if !qutetest.WaitFor(func() bool { calls++; return true }, 0) || calls != 1 {
			t.Errorf("WaitFor of a true condition: %d calls", calls)
		}
	})
}

// WhenModal answers a dialog's Exec, and WhenPopup picks from a menu's
// ExecAt, without guessed delays.
func TestWhenModalAndPopup(t *testing.T) {
	qutetest.Run(t, func() {
		dlg := qtwidgets.NewQDialog(nil)
		defer dlg.Delete()
		qutetest.WhenModal(t, func(w *qtwidgets.QWidget) {
			if w != dlg.AsQWidget() {
				t.Errorf("WhenModal got %s, want the dialog", w.MetaObject().ClassName())
			}
			qtwidgets.QDialogCast(w).Accept()
		})
		if r := dlg.Exec(); r != int(qtwidgets.QDialogAccepted) {
			t.Errorf("Exec = %d, want Accepted", r)
		}

		menu := qtwidgets.NewQMenu(nil)
		defer menu.Delete()
		menu.AddActionText("one")
		two := menu.AddActionText("two")
		qutetest.WhenPopup(t, func(w *qtwidgets.QWidget) {
			qutetest.Key(w, qtcore.Key_Down, 0)
			qutetest.Key(w, qtcore.Key_Down, 0)
			qutetest.Key(w, qtcore.Key_Return, 0)
		})
		if got := menu.ExecAt(qtcore.QPoint{X: 10, Y: 10}); got != two {
			t.Errorf("ExecAt chose %v, want the second action", got)
		}
	})
}

// A WhenModal whose dialog never appears fails the test at its end, instead
// of polling for ever.
func TestWhenModalNeverShown(t *testing.T) {
	ft := &fakeTB{TB: t}
	qutetest.Run(t, func() {
		qutetest.WhenModal(ft, func(*qtwidgets.QWidget) { t.Error("no dialog was shown") })
	})
	ft.cleanup()
	if !strings.Contains(ft.errs, "no modal widget appeared") {
		t.Errorf("errors: %q", ft.errs)
	}
}

// fakeTB collects the errors and cleanups given to it.
type fakeTB struct {
	testing.TB
	errs     string
	cleanups []func()
}

func (f *fakeTB) Helper()                           {}
func (f *fakeTB) Cleanup(c func())                  { f.cleanups = append(f.cleanups, c) }
func (f *fakeTB) Errorf(format string, args ...any) { f.errs += fmt.Sprintf(format, args...) + "\n" }
func (f *fakeTB) cleanup() {
	for _, c := range slices.Backward(f.cleanups) {
		c()
	}
}
