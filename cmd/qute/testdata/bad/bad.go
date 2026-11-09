// Package bad holds one example of each mistake qute vet reports.
package bad

import (
	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtopengl"
	"github.com/nomad-software/qute/qtwidgets"
)

var lastEvent *qtgui.QMouseEvent

type Canvas struct{ qtwidgets.QWidgetBase }

func (c *Canvas) MousePressEvent(e *qtgui.QMouseEvent) {
	lastEvent = e // want scoped-escape
	local := e    // fine: a local
	_ = local
}

func Leak() {
	btn := qtwidgets.NewQPushButtonWithText("x", nil) // want owned-leak
	btn.OnClicked(func(bool) {})
}

func NoLeak() *qtwidgets.QPushButton {
	btn := qtwidgets.NewQPushButtonWithText("x", nil)
	btn.OnClicked(func(bool) {})
	return btn
}

func DeleteInHandler(w *qtwidgets.QWidget) {
	btn := qtwidgets.NewQPushButtonWithText("x", w)
	btn.OnClicked(func(bool) {
		btn.Delete() // want delete-handler
	})
}

func Goroutine(lbl *qtwidgets.QLabel) {
	go func() {
		lbl.SetText("x") // want gui-goroutine
		qute.Post(func() { lbl.SetText("fine") })
	}()
}

func GLGoroutine(buf *qtopengl.QOpenGLBuffer) {
	go func() {
		buf.Bind() // want gui-goroutine
	}()
}
