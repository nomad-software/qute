//go:build goqt_debug

package goqt_test

import (
	"strings"
	"testing"

	"github.com/nomad-software/qute/goqt"
	"github.com/nomad-software/qute/qtwidgets"
)

// Debug builds check the thread on every call.
func TestDebugWrongThreadPanics(t *testing.T) {
	var lbl *qtwidgets.QLabel
	goqt.Run(t, func() { lbl = qtwidgets.NewQLabelWithText("x", nil) })
	defer goqt.Run(t, func() { lbl.Delete() })
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "off the GUI thread") {
			t.Errorf("want thread panic, got %v", p)
		}
	}()
	lbl.SetText("from a test goroutine")
}

// Debug builds forbid Delete inside a handler (rule 5).
func TestDebugDeleteInHandlerPanics(t *testing.T) {
	goqt.Run(t, func() {
		btn := qtwidgets.NewQPushButtonWithText("x", nil)
		var msg string
		btn.OnClicked(func(bool) {
			defer func() {
				if p := recover(); p != nil {
					msg = p.(string)
				}
			}()
			btn.Delete()
		})
		goqt.Click(btn)
		if !strings.Contains(msg, "DeleteLater") {
			t.Errorf("want DeleteLater hint, got %q", msg)
		}
		btn.Delete()
	})
}

// Debug builds record where an object died.
func TestDebugDeathSite(t *testing.T) {
	goqt.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		lbl := qtwidgets.NewQLabelWithText("x", w)
		w.Delete()
		defer func() {
			msg, _ := recover().(string)
			if !strings.Contains(msg, "debug_test.go:") {
				t.Errorf("want death site in message, got %q", msg)
			}
		}()
		lbl.Text()
	})
}
