// Package georef runs Qt's own QRect and QRectF arithmetic, which qtcore's
// Go geometry methods are tested against. Only tests import it, so programs
// never link it. QRect and QRectF are plain values, so these calls need no
// application and run on any goroutine.
package georef

// #cgo pkg-config: Qt6Core
// #cgo CXXFLAGS: -std=c++17 -fPIC -O1 -g0
// #include "georef.h"
import "C"

// Rect is what Qt says of the rectangle (x, y, w, h) r, the point p and the
// rectangle o.
type Rect struct {
	CenterX, CenterY     int
	Contains, Intersects bool
	Empty                bool
}

// RectOps runs QRect::center, contains(p), intersects(o) and isEmpty.
func RectOps(r [4]int32, p [2]int32, o [4]int32) Rect {
	var out [5]C.int32_t
	C.qute_georef_rect((*C.int32_t)(&r[0]), (*C.int32_t)(&p[0]), (*C.int32_t)(&o[0]), &out[0])
	return Rect{int(out[0]), int(out[1]), out[2] != 0, out[3] != 0, out[4] != 0}
}

// RectFOps runs QRectF::toRect (x, y, w, h) and contains(p).
func RectFOps(r [4]float64, p [2]float64) (toRect [4]int, contains bool) {
	var out [5]C.int32_t
	C.qute_georef_rectf((*C.double)(&r[0]), (*C.double)(&p[0]), &out[0])
	return [4]int{int(out[0]), int(out[1]), int(out[2]), int(out[3])}, out[4] != 0
}
