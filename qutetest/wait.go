package qutetest

// #include "input.h"
import "C"

import (
	"testing"
	"time"

	"github.com/nomad-software/qute"
	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"
)

// waitSlice is how long WaitFor runs the event loop between two looks at
// its condition.
const waitSlice = 5 * time.Millisecond

// WaitFor runs the event loop until cond returns true or timeout passes, and
// returns cond's last answer, like Qt's QTRY_VERIFY:
//
//	if !qutetest.WaitFor(func() bool { return len(w.panes()) == 2 }, time.Second) {
//		t.Error("the pane did not close")
//	}
//
// cond is checked before the first wait, so a condition already true costs
// nothing. Call it on the GUI thread.
func WaitFor(cond func() bool, timeout time.Duration) bool {
	rt.GUIOnly("qutetest.WaitFor")
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		C.qute_tools_wait(C.int(min(waitSlice, max(time.Until(deadline), time.Millisecond)) / time.Millisecond))
		rt.RethrowCallbackPanic()
	}
	return true
}

// pollInterval is how often WhenModal and WhenPopup look for the widget.
const pollInterval = 5 * time.Millisecond

// WhenModal runs f on the next modal widget to become active (a dialog's
// Exec, a QMessageBox, a static dialog function such as
// QFileDialogGetOpenFileName), once it is up. Call it before the call that
// blocks in the dialog's event loop; f answers or closes the dialog, which
// returns that call:
//
//	qutetest.WhenModal(t, func(w *qtwidgets.QWidget) {
//		qtwidgets.QDialogCast(w).Accept()
//	})
//	dlg.Exec()
//
// It looks every few milliseconds while the event loop runs, so it needs no
// guessed delay. A modal widget already active when WhenModal is called does
// not count. If none has appeared by the end of the test, the test fails.
func WhenModal(t testing.TB, f func(w *qtwidgets.QWidget)) {
	t.Helper()
	whenActive(t, "qutetest.WhenModal", "modal widget", qtwidgets.QApplicationActiveModalWidget, f)
}

// WhenPopup runs f on the next popup to become active (a menu's Exec or
// ExecAt, a combo box's list), once it is up, as WhenModal does for modal
// widgets. f picks an item or closes the popup, which returns the call that
// opened it:
//
//	qutetest.WhenPopup(t, func(w *qtwidgets.QWidget) {
//		qutetest.Key(w, qtcore.Key_Down, 0)
//		qutetest.Key(w, qtcore.Key_Return, 0)
//	})
//	chosen := menu.ExecAt(pos)
//
// If no popup has appeared by the end of the test, the test fails.
func WhenPopup(t testing.TB, f func(w *qtwidgets.QWidget)) {
	t.Helper()
	whenActive(t, "qutetest.WhenPopup", "popup", qtwidgets.QApplicationActivePopupWidget, f)
}

func whenActive(t testing.TB, method, what string, active func() *qtwidgets.QWidget, f func(w *qtwidgets.QWidget)) {
	rt.GUIOnly(method)
	before := active()
	timer := qtcore.NewQTimer(nil)
	timer.SetInterval(int(pollInterval / time.Millisecond))
	done := false
	timer.OnTimeout(func() {
		w := active()
		if done || w == nil || w == before {
			return
		}
		done = true
		timer.Stop()
		timer.DeleteLater()
		f(w)
	})
	timer.Start()
	t.Cleanup(func() {
		qute.PostSync(func() {
			if done {
				return
			}
			done = true
			t.Errorf("%s: no %s appeared during the test", method, what)
			if timer.Alive() {
				timer.Delete()
			}
		})
	})
}
