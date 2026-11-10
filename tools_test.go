package qute_test

import (
	"testing"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

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

func TestProcessEventsRunsPostedWork(t *testing.T) {
	qutetest.Run(t, func() {
		ran := false
		qute.Post(func() { ran = true })
		qute.ProcessEvents()
		if !ran {
			t.Error("work posted to the GUI thread did not run in ProcessEvents")
		}
		if !qute.IsGUIThread() {
			t.Error("IsGUIThread() = false inside qutetest.Run")
		}
	})
}
