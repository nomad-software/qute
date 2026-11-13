package qutetest

// #cgo pkg-config: Qt6Widgets Qt6Test
// #cgo CXXFLAGS: -std=c++17 -fPIC -O1 -g0
// #include "input.h"
import "C"

import (
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"
)

func widgetPtr(w qtwidgets.QWidgetLike, method string) unsafe.Pointer {
	if w == nil {
		panic(method + ": widget is nil")
	}
	rt.GUIOnly(method)
	return rt.Use(unsafe.Pointer(w.AsQWidget()), method)
}

// Click sends a left mouse click to the centre of w, as a user would.
func Click(w qtwidgets.QWidgetLike) {
	C.qute_tools_click(widgetPtr(w, "qutetest.Click"), C.int(qtcore.LeftButton))
	rt.RethrowCallbackPanic()
}

// ClickAt sends a mouse click with button at pos (widget coordinates).
func ClickAt(w qtwidgets.QWidgetLike, button qtcore.MouseButton, pos qtcore.QPoint) {
	mouse(w, "qutetest.ClickAt", mousePress, button, 0, pos)
	mouse(w, "qutetest.ClickAt", mouseRelease, button, 0, pos)
}

// DoubleClick sends a left double click to the centre of w.
func DoubleClick(w qtwidgets.QWidgetLike) {
	C.qute_tools_dblclick(widgetPtr(w, "qutetest.DoubleClick"), C.int(qtcore.LeftButton))
	rt.RethrowCallbackPanic()
}

// Type sends text to w as key presses.
func Type(w qtwidgets.QWidgetLike, text string) {
	C.qute_tools_type(widgetPtr(w, "qutetest.Type"), (*C.char)(unsafe.Pointer(unsafe.StringData(text))), C.ptrdiff_t(len(text)))
	rt.RethrowCallbackPanic()
}

// Key sends one key press with modifiers, e.g. Key(edit, qtcore.Key_Return, 0).
func Key(w qtwidgets.QWidgetLike, key qtcore.Key, mods qtcore.KeyboardModifiers) {
	C.qute_tools_key(widgetPtr(w, "qutetest.Key"), C.int(key), C.int(mods))
	rt.RethrowCallbackPanic()
}

// Wait runs the event loop for ms milliseconds (timers, deferred deletes).
// Call it on the GUI thread.
func Wait(ms int) {
	rt.GUIOnly("qutetest.Wait")
	C.qute_tools_wait(C.int(ms))
	rt.RethrowCallbackPanic()
}

const (
	mousePress = iota
	mouseMove
	mouseRelease
)

func mouse(w qtwidgets.QWidgetLike, method string, action int, button qtcore.MouseButton, mods qtcore.KeyboardModifiers, pos qtcore.QPoint) {
	C.qute_tools_mouse(widgetPtr(w, method), C.int(action), C.int(button), C.int(mods), C.int(pos.X), C.int(pos.Y))
	rt.RethrowCallbackPanic()
}

// Press presses button at pos (widget coordinates) with mods held, and keeps
// it held: the moves that follow carry it until Release.
func Press(w qtwidgets.QWidgetLike, button qtcore.MouseButton, mods qtcore.KeyboardModifiers, pos qtcore.QPoint) {
	mouse(w, "qutetest.Press", mousePress, button, mods, pos)
}

// Move moves the mouse to pos (widget coordinates). With a button held
// (after Press) w gets a move event carrying it, as the widget a drag began
// on does wherever the mouse goes; pos may lie outside w. With none held it
// sends w a plain move, which it sees only if it tracks the mouse.
func Move(w qtwidgets.QWidgetLike, pos qtcore.QPoint) {
	mouse(w, "qutetest.Move", mouseMove, 0, 0, pos)
}

// Release releases button at pos (widget coordinates).
func Release(w qtwidgets.QWidgetLike, button qtcore.MouseButton, mods qtcore.KeyboardModifiers, pos qtcore.QPoint) {
	mouse(w, "qutetest.Release", mouseRelease, button, mods, pos)
}

// dragSteps is how many moves Drag makes, so a widget that waits for the
// mouse to pass QApplication::startDragDistance sees it go there gradually.
const dragSteps = 5

// Drag presses button at from, moves to to in a few steps and releases there,
// all in w's coordinates; to may lie outside w. It sends mouse events, as
// for dragging a splitter handle or a header: it does not drive drag and
// drop through QDrag::exec, which runs a loop of its own.
func Drag(w qtwidgets.QWidgetLike, button qtcore.MouseButton, from, to qtcore.QPoint) {
	mouse(w, "qutetest.Drag", mousePress, button, 0, from)
	for i := 1; i <= dragSteps; i++ {
		p := qtcore.QPoint{X: from.X + (to.X-from.X)*i/dragSteps, Y: from.Y + (to.Y-from.Y)*i/dragSteps}
		mouse(w, "qutetest.Drag", mouseMove, 0, 0, p)
	}
	mouse(w, "qutetest.Drag", mouseRelease, button, 0, to)
}
