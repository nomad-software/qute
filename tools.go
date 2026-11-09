package qute

// #cgo pkg-config: Qt6Widgets Qt6Test
// #cgo CXXFLAGS: -std=c++17 -fPIC -O1 -g0
// #include "tools.h"
import "C"

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"
)

func widgetPtr(w qtwidgets.QWidgetLike, method string) unsafe.Pointer {
	if w == nil {
		panic(method + ": widget is nil")
	}
	return rt.Use(unsafe.Pointer(w.AsQWidget()), method)
}

// Screenshot renders w (and its children) into an image. It works under the
// offscreen platform (QT_QPA_PLATFORM=offscreen), so a layout can be rendered,
// looked at and fixed with no display. A widget that was never shown is laid
// out and sized to its size hint first.
func Screenshot(w qtwidgets.QWidgetLike) *image.NRGBA {
	p := widgetPtr(w, "qute.Screenshot")
	r := C.qute_tools_grab(p)
	defer C.free(unsafe.Pointer(r.data))
	img := image.NewNRGBA(image.Rect(0, 0, int(r.w), int(r.h)))
	copy(img.Pix, unsafe.Slice((*byte)(unsafe.Pointer(r.data)), int(r.w)*int(r.h)*4))
	return img
}

// SaveScreenshot writes Screenshot(w) to path as a PNG.
func SaveScreenshot(w qtwidgets.QWidgetLike, path string) error {
	img := Screenshot(w)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Node is one object in a DumpTree result.
type Node struct {
	Class      string        `json:"class"`
	ObjectName string        `json:"objectName,omitempty"`
	Ownership  string        `json:"ownership"`
	Pins       int           `json:"pins,omitempty"`
	Geometry   *qtcore.QRect `json:"geometry,omitempty"`
	Visible    *bool         `json:"visible,omitempty"`
	Window     bool          `json:"window,omitempty"`
	Children   []*Node       `json:"children,omitempty"`
}

// Tree returns the object tree under o with each object's class, objectName,
// ownership state and (for widgets) geometry. Objects Go has never wrapped
// show ownership "untracked".
func Tree(o qtcore.QObjectLike) *Node {
	if o == nil {
		return nil
	}
	return tree(rt.Use(unsafe.Pointer(o.AsQObject()), "qute.Tree"))
}

func tree(p unsafe.Pointer) *Node {
	var info C.qute_objinfo
	C.qute_tools_info(p, &info)
	defer C.qute_tools_free_info(&info)
	n := &Node{Class: C.GoString(info.cls), ObjectName: C.GoString(info.name), Ownership: "untracked"}
	if h := rt.Lookup(p); h != nil {
		n.Ownership = h.State().String()
		n.Pins = h.Pins()
	}
	if info.widget {
		g := qtcore.QRect{X: int(info.x), Y: int(info.y), Width: int(info.w), Height: int(info.h)}
		v := bool(info.visible)
		n.Geometry, n.Visible, n.Window = &g, &v, bool(info.window)
	}
	kids := unsafe.Slice(info.children, int(info.nchildren))
	for _, k := range kids {
		n.Children = append(n.Children, tree(k))
	}
	return n
}

// DumpTree returns Tree(o) as indented JSON.
func DumpTree(o qtcore.QObjectLike) string {
	b, err := json.MarshalIndent(Tree(o), "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// Click sends a left mouse click to the centre of w, as a user would.
func Click(w qtwidgets.QWidgetLike) {
	C.qute_tools_click(widgetPtr(w, "qute.Click"), C.int(qtcore.LeftButton), 0, 0, false)
}

// ClickAt sends a mouse click with button at pos (widget coordinates).
func ClickAt(w qtwidgets.QWidgetLike, button qtcore.MouseButton, pos qtcore.QPoint) {
	C.qute_tools_click(widgetPtr(w, "qute.ClickAt"), C.int(button), C.int(pos.X), C.int(pos.Y), true)
}

// DoubleClick sends a left double click to the centre of w.
func DoubleClick(w qtwidgets.QWidgetLike) {
	C.qute_tools_dblclick(widgetPtr(w, "qute.DoubleClick"), C.int(qtcore.LeftButton))
}

// Type sends text to w as key presses.
func Type(w qtwidgets.QWidgetLike, text string) {
	C.qute_tools_type(widgetPtr(w, "qute.Type"), (*C.char)(unsafe.Pointer(unsafe.StringData(text))), C.ptrdiff_t(len(text)))
}

// Key sends one key press with modifiers, e.g. Key(edit, qtcore.Key_Return, 0).
func Key(w qtwidgets.QWidgetLike, key qtcore.Key, mods qtcore.KeyboardModifiers) {
	C.qute_tools_key(widgetPtr(w, "qute.Key"), C.int(key), C.int(mods))
}

// Wait runs the event loop for ms milliseconds (timers, deferred deletes).
// Call it on the GUI thread.
func Wait(ms int) {
	rt.CheckThread("qute.Wait")
	C.qute_tools_wait(C.int(ms))
}

// ProcessEvents handles pending events once. Call it on the GUI thread.
func ProcessEvents() {
	rt.CheckThread("qute.ProcessEvents")
	C.qute_tools_process()
}
