// Hand-written; goqtgen never overwrites *_manual.go files.

package qtwidgets

// #include "application_manual.h"
import "C"

import (
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
)

// NewQApplication creates the application object. Most programs call
// goqt.Main instead, which creates it, runs the event loop and handles
// shutdown. The application object is never freed by GC.
func NewQApplication(args []string) *QApplication {
	rt.CheckThread("NewQApplication")
	if rt.AppExists() {
		panic("NewQApplication: an application object already exists; use QApplicationInstance()")
	}
	d, l := goqtStrListArg(args)
	p := C.goqt_application_new((*C.char)(unsafe.Pointer(unsafe.SliceData(d))), (*C.ptrdiff_t)(unsafe.SliceData(l)), C.ptrdiff_t(len(args)))
	app := rt.View[QApplication](rt.WrapClass(p, "QApplication", rt.KeepOrBorrow), p)
	app.Keep()
	return app
}

//export goqt_qtwidgets_visibility
func goqt_qtwidgets_visibility(obj unsafe.Pointer, visible C.bool) {
	if h := rt.Lookup(obj); h != nil {
		h.SetVisiblePin(bool(visible))
	}
}
