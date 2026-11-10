// Package bad holds one example of each mistake qute vet reports.
package bad

import (
	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtopengl"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
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

// A parent passed as a concrete wrapper is still a parent.
func ParentedByDialog() {
	d := qtwidgets.NewQDialog(nil)
	b := qtwidgets.NewQPushButtonWithText("ok", d)
	b.OnClicked(func(bool) { d.Accept() })
	d.Show()
}

// Exec returns with the dialog hidden, still pinned by its own connection.
func DialogLeak() {
	d := qtwidgets.NewQDialog(nil) // want owned-leak
	d.OnFinished(func(int) {})
	d.Exec()
}

// A parentless Go subclass is pinned for its Go value.
func SubclassLeak() {
	qtwidgets.NewQWidgetWith(&Canvas{}, nil)      // want owned-leak
	c := qtwidgets.NewQWidgetWith(&Canvas{}, nil) // want owned-leak
	c.Resize(10, 10)
	ok := qtwidgets.NewQWidgetWith(&Canvas{}, nil)
	defer ok.Delete()
	v := &Canvas{}
	qtwidgets.NewQWidgetWith(v, nil) // want owned-leak
	given := &Canvas{}
	qtwidgets.NewQWidgetWith(given, nil)
	qtwidgets.NewQVBoxLayout(ok).AddWidget(given) // the value stands for the widget
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
		qutetest.Click(lbl) // want gui-goroutine
		qute.PostSync(func() { qutetest.Click(lbl) })
	}()
}

func GLGoroutine(buf *qtopengl.QOpenGLBuffer) {
	go func() {
		buf.Bind() // want gui-goroutine
	}()
}
