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

// A Go subclass handed on as its wrapper (an embedded field) is not leaked:
// the caller gives it a parent.
func SubclassReturned() *qtwidgets.QWidget {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	return c.QWidget
}

type holder struct{ w *qtwidgets.QWidget }

func SubclassStored(h *holder) {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	h.w = c.QWidget
}

func SubclassAliased() *qtwidgets.QWidget {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	w := c.QWidget
	return w
}

// An As<Class>() upcast hands the object on like an embedded field
// (a helper returning a style that its caller gives QApplication).
func SubclassUpcastReturned() *qtwidgets.QWidget {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	return c.AsQWidget()
}

func SubclassUpcastStored(h *holder) {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	h.w = c.AsQWidget()
}

func SubclassUpcastInLiteral() holder {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil)
	return holder{w: c.AsQWidget()}
}

// Calling a method that returns something else does not hand it on.
func SubclassUpcastNotReturned() string {
	c := &Canvas{}
	qtwidgets.NewQWidgetWith(c, nil) // want owned-leak
	return c.AsQWidget().ObjectName()
}

// A field that is not the object does not hand it on.
type labelled struct {
	qtwidgets.QWidgetBase
	name string
}

func SubclassFieldOnly() string {
	l := &labelled{name: "x"}
	qtwidgets.NewQWidgetWith(l, nil) // want owned-leak
	return l.name
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

// Rule 5 is about QObjects: values and plain objects made in a handler are
// deleted there.
func DeleteValueInHandler(w *qtwidgets.QWidget, img *qtgui.QImage) {
	btn := qtwidgets.NewQPushButtonWithText("x", w)
	btn.OnClicked(func(bool) {
		p := qtgui.NewQPainterOn(img)
		path := qtgui.NewQPainterPath()
		p.DrawPath(path)
		path.Delete()
		p.Delete()
		img.Delete()
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

var lastPainter *qtgui.QPainter

// Dot overrides a virtual that is no event handler: its painter is scoped
// all the same, and it runs inside Qt like any handler.
type Dot struct {
	qtwidgets.QGraphicsItemBase
	owner *qtwidgets.QWidget
}

func (d *Dot) Paint(painter *qtgui.QPainter, option *qtwidgets.QStyleOptionGraphicsItem, widget *qtwidgets.QWidget) {
	lastPainter = painter // want scoped-escape
	d.owner.Delete()      // want delete-handler
}

// Ring gets its subclass from the type it embeds.
type Ring struct{ Dot }

func (r *Ring) MousePressEvent(e *qtwidgets.QGraphicsSceneMouseEvent) {
	r.owner.Delete() // want delete-handler
}

// Helper is no override: it has no scoped argument and is not an event handler.
func (d *Dot) Helper() {
	d.owner.Delete()
}

// drawGrid is a helper, not an override: the painter may be the program's own.
func (d *Dot) drawGrid(p *qtgui.QPainter) {
	lastPainter = p
}

// PaintOn's painter is scoped to the call, and the function runs at once,
// so the handler's event may be used inside it.
type Board struct{ qtwidgets.QWidgetBase }

func (b *Board) PaintEvent(e *qtgui.QPaintEvent) {
	qtgui.PaintOn(b, func(p *qtgui.QPainter) {
		p.FillRect(e.Rect(), qtgui.RGB(0, 0, 0))
		lastPainter = p // want scoped-escape
	})
}

var painters []*qtgui.QPainter

func PaintImage(img *qtgui.QImage) {
	qtgui.PaintOn(img, func(p *qtgui.QPainter) {
		painters = append(painters, p) // want scoped-escape
	})
}

var lastKey *qtgui.QKeyEvent

// A per-object override pins its object like a connection.
func OverrideLeak() {
	w := qtwidgets.NewQWidget(nil) // want owned-leak
	w.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), e *qtgui.QKeyEvent) { super(e) })
}

// Removing it is not a reason to report.
func OverrideRemoved() {
	w := qtwidgets.NewQWidget(nil)
	w.OverrideKeyPressEvent(nil)
}

// The function is a handler: its event is scoped and Delete() is refused.
func OverrideHandler(parent *qtwidgets.QWidget) {
	b := qtwidgets.NewQPushButtonWithText("x", parent)
	b.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), e *qtgui.QKeyEvent) {
		lastKey = e // want scoped-escape
		b.Delete()  // want delete-handler
	})
}
