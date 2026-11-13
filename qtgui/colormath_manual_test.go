// Hand-written; qutegen never overwrites *_manual files.

package qtgui_test

import (
	"math/rand/v2"
	"testing"

	"github.com/nomad-software/qute/internal/colorref"
	"github.com/nomad-software/qute/qtgui"
)

func pack(c qtgui.QColor) int32 {
	return int32(uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A))
}

func unpack(p int32) qtgui.QColor {
	u := uint32(p)
	return qtgui.QColor{R: uint8(u >> 24), G: uint8(u >> 16), B: uint8(u >> 8), A: uint8(u)}
}

// fusedProbe is a colour whose HSV-to-RGB conversion tells the two kinds of
// Qt build apart: built as written (generic x86-64), QColor gives red 127;
// built where the compiler fuses multiply-adds into FMA instructions
// (x86-64-v3 packages such as CachyOS's, and GCC on ARM64 by default), 128.
var fusedProbe = [4]float32{0.77822953, 0.7154649, 0.65494317, 0.6249812}

// qtFuses asks the installed Qt which kind of build it is.
func qtFuses(t *testing.T) bool {
	fhsv, _, _, _ := colorref.FromFloats(fusedProbe[:])
	switch r := unpack(fhsv[0]).R; r {
	case 127:
		return false
	case 128:
		return true
	default:
		t.Fatalf("Qt gives red %d for the FMA probe, want 127 (unfused) or 128 (fused)", r)
		return false
	}
}

// near is whether two colours differ by at most one step per channel.
func near(a, b qtgui.QColor) bool {
	d := func(x, y uint8) bool { return x-y <= 1 || y-x <= 1 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && a.A == b.A
}

// The Go colour arithmetic gives what Qt's QColor gives, over a sweep of
// every third value per channel (0 and 255 included) and several factors.
// The port follows Qt's source as written, so it matches a Qt built that way
// exactly. A Qt whose compiler fused multiply-adds (qtFuses) rounds some
// conversions differently; there results built through HSV or HSL may be a
// step off per channel, and everything else must still match exactly.
func TestColorMathMatchesQt(t *testing.T) {
	fused := qtFuses(t)
	if fused {
		t.Log("Qt was built with fused multiply-adds: colours from HSV or HSL may differ by one step")
	}
	same := func(got qtgui.QColor, qt int32) bool {
		return pack(got) == qt || fused && near(got, unpack(qt))
	}
	var rgba []byte
	var cols []qtgui.QColor
	for r := 0; r < 256; r += 3 {
		for g := 0; g < 256; g += 3 {
			for b := 0; b < 256; b += 3 {
				c := qtgui.QColor{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(r + g + b)}
				cols = append(cols, c)
				rgba = append(rgba, c.R, c.G, c.B, c.A)
			}
		}
	}
	errs := 0
	fail := func(format string, a ...any) {
		if errs++; errs <= 20 {
			t.Errorf(format, a...)
		}
	}
	for _, f := range [][2]int{{150, 200}, {120, 300}, {50, 50}, {100, 100}, {400, 101}, {0, -5}} {
		ints, floats := colorref.Ops(rgba, f[0], f[1])
		for i, c := range cols {
			o, fl := ints[8*i:8*i+8], floats[6*i:6*i+6]
			if f[0] == 150 { // the getters do not depend on the factors
				if h, s, v := c.Hsv(); h != int(o[0]) || s != int(o[1]) || v != int(o[2]) {
					fail("%v.Hsv() = %d %d %d, Qt %d %d %d", c, h, s, v, o[0], o[1], o[2])
				}
				if h, s, l := c.Hsl(); h != int(o[3]) || s != int(o[4]) || l != int(o[5]) {
					fail("%v.Hsl() = %d %d %d, Qt %d %d %d", c, h, s, l, o[3], o[4], o[5])
				}
				if h, s, v := c.HsvF(); h != fl[0] || s != fl[1] || v != fl[2] {
					fail("%v.HsvF() = %v %v %v, Qt %v %v %v", c, h, s, v, fl[0], fl[1], fl[2])
				}
				if h, s, l := c.HslF(); h != fl[3] || s != fl[4] || l != fl[5] {
					fail("%v.HslF() = %v %v %v, Qt %v %v %v", c, h, s, l, fl[3], fl[4], fl[5])
				}
				if c.Hue() != int(o[0]) || c.Saturation() != int(o[1]) || c.Value() != int(o[2]) ||
					c.Lightness() != int(o[5]) || c.LightnessF() != fl[5] {
					fail("%v: Hue, Saturation, Value or Lightness differ from Hsv and Hsl", c)
				}
			}
			if got := c.Lighter(f[0]); !same(got, o[6]) {
				fail("%v.Lighter(%d) = %v, Qt %v", c, f[0], got, unpack(o[6]))
			}
			if got := c.Darker(f[1]); !same(got, o[7]) {
				fail("%v.Darker(%d) = %v, Qt %v", c, f[1], got, unpack(o[7]))
			}
		}
	}

	// from HSV and HSL, including achromatic (-1), hue 360 and more, and
	// values out of range (Qt gives an invalid colour)
	var quads []int32
	for h := -2; h <= 725; h += 7 {
		for s := -1; s <= 256; s += 15 {
			for v := -1; v <= 256; v += 15 {
				quads = append(quads, int32(h), int32(s), int32(v), int32((h+s+v)&0xff))
			}
		}
	}
	quads = append(quads, -1, 0, 128, 255, 360, 255, 255, 255, 0, 255, 255, 256)
	hsv, hsl := colorref.FromInts(quads)
	for i := range hsv {
		q := quads[4*i : 4*i+4]
		if got := qtgui.ColorFromHsv(int(q[0]), int(q[1]), int(q[2]), int(q[3])); !same(got, hsv[i]) {
			fail("ColorFromHsv%v = %v, Qt %v", q, got, unpack(hsv[i]))
		}
		if got := qtgui.ColorFromHsl(int(q[0]), int(q[1]), int(q[2]), int(q[3])); !same(got, hsl[i]) {
			fail("ColorFromHsl%v = %v, Qt %v", q, got, unpack(hsl[i]))
		}
	}

	// the float forms, from random components and the edges
	rng := rand.New(rand.NewPCG(1, 2))
	fq := []float32{-1, 0, 0, 0, 1, 1, 1, 1, 0, 0.5, 0.5, 0.5, 1.0000001, 0.5, 0.5, 1, 0.5, -0.1, 0.5, 1,
		1.5, 0.3, 0.7, 1, 0.123, -2, 0.987, 0.4, 0.3, 0.3, 0.3, 1.2, 1, 1, 1, -0.5}
	for range 20000 { // extended (out-of-range) channels, which Qt keeps at half precision
		fq = append(fq, rng.Float32()*3-1, rng.Float32(), rng.Float32()*3-1, rng.Float32())
	}
	for range 200000 {
		fq = append(fq, rng.Float32(), rng.Float32(), rng.Float32(), rng.Float32())
	}
	fhsv, fhsl, frgb, falpha := colorref.FromFloats(fq)
	for i := range fhsv {
		q := fq[4*i : 4*i+4]
		if got := qtgui.ColorFromHsvF(q[0], q[1], q[2], q[3]); !same(got, fhsv[i]) {
			fail("ColorFromHsvF%v = %v, Qt %v", q, got, unpack(fhsv[i]))
		}
		if got := qtgui.ColorFromHslF(q[0], q[1], q[2], q[3]); !same(got, fhsl[i]) {
			fail("ColorFromHslF%v = %v, Qt %v", q, got, unpack(fhsl[i]))
		}
		if got := qtgui.ColorFromRgbF(q[0], q[1], q[2], q[3]); pack(got) != frgb[i] {
			fail("ColorFromRgbF%v = %v, Qt %v", q, got, unpack(frgb[i]))
		}
		if got := qtgui.RGB(255, 255, 255).WithAlphaF(q[0]); pack(got) != falpha[i] {
			fail("WithAlphaF(%v) = %v, Qt %v", q[0], got, unpack(falpha[i]))
		}
	}
	if errs > 20 {
		t.Errorf("... %d differences in all", errs)
	}
}

func TestColorChannelsF(t *testing.T) {
	c := qtgui.RGBAColor(255, 0, 51, 102)
	if c.RedF() != 1 || c.GreenF() != 0 || c.BlueF() != 0.2 || c.AlphaF() != 0.4 {
		t.Errorf("channels F = %v %v %v %v", c.RedF(), c.GreenF(), c.BlueF(), c.AlphaF())
	}
	if got := c.WithAlpha(7); got != (qtgui.QColor{R: 255, B: 51, A: 7}) {
		t.Errorf("WithAlpha(7) = %v", got)
	}
}

// The arithmetic is plain Go: no allocation and no call into Qt.
func BenchmarkColorLighter(b *testing.B) {
	c := qtgui.RGB(30, 120, 200)
	b.ReportAllocs()
	for b.Loop() {
		c = c.Lighter(101).Darker(101)
	}
}
