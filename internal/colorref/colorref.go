// Package colorref runs Qt's own QColor arithmetic, which qtgui's Go colour
// methods (Lighter, Hsv, ColorFromHsl...) are tested against. Only tests
// import it, so programs never link it. QColor is a plain value, so these
// calls need no application and run on any goroutine.
package colorref

// #cgo pkg-config: Qt6Gui
// #cgo CXXFLAGS: -std=c++17 -fPIC -O1 -g0
// #include "colorref.h"
import "C"

import "unsafe"

// Ops returns, for each colour (4 bytes RGBA), 8 ints: HSV h, s, v; HSL
// h, s, l; lighter(lf) and darker(df) packed as 0xRRGGBBAA, and 6 floats:
// HSV-F h, s, v and HSL-F h, s, l.
func Ops(rgba []byte, lf, df int) ([]int32, []float32) {
	n := len(rgba) / 4
	ints := make([]int32, 8*n)
	floats := make([]float32, 6*n)
	if n > 0 {
		C.qute_colorref_ops((*C.uint8_t)(unsafe.Pointer(&rgba[0])), C.int(n), C.int(lf), C.int(df),
			(*C.int32_t)(unsafe.Pointer(&ints[0])), (*C.float)(unsafe.Pointer(&floats[0])))
	}
	return ints, floats
}

// FromInts returns QColor::fromHsv and fromHsl, packed, of each int quad.
func FromInts(in []int32) (hsv, hsl []int32) {
	n := len(in) / 4
	hsv, hsl = make([]int32, n), make([]int32, n)
	if n > 0 {
		C.qute_colorref_from_ints((*C.int32_t)(unsafe.Pointer(&in[0])), C.int(n),
			(*C.int32_t)(unsafe.Pointer(&hsv[0])), (*C.int32_t)(unsafe.Pointer(&hsl[0])))
	}
	return hsv, hsl
}

// FromFloats returns QColor::fromHsvF, fromHslF and fromRgbF of each float
// quad, and opaque white after setAlphaF(quad[0]), packed.
func FromFloats(in []float32) (hsv, hsl, rgb, alpha []int32) {
	n := len(in) / 4
	hsv, hsl, rgb, alpha = make([]int32, n), make([]int32, n), make([]int32, n), make([]int32, n)
	if n > 0 {
		C.qute_colorref_from_floats((*C.float)(unsafe.Pointer(&in[0])), C.int(n),
			(*C.int32_t)(unsafe.Pointer(&hsv[0])), (*C.int32_t)(unsafe.Pointer(&hsl[0])),
			(*C.int32_t)(unsafe.Pointer(&rgb[0])), (*C.int32_t)(unsafe.Pointer(&alpha[0])))
	}
	return hsv, hsl, rgb, alpha
}
