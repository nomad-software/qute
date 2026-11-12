// Hand-written; qutegen never overwrites *_manual.go files.

package qtgui

import (
	"math"
	"runtime"
)

// Colour arithmetic of Qt's QColor, done in Go on the 8-bit QColor struct.
// It follows QColor's algorithms (Qt 6.12) step for step: an 8-bit channel
// becomes 16 bits (x * 257), the conversion runs in the integer and float32
// steps Qt's does, and the 16-bit result is rounded back to 8 bits as
// QColor::red() does (qt_div_257), so a
// single operation gives the colour Qt gives (TestColorMathMatchesQt
// compares them over a sweep of colours). Chained operations can differ from
// Qt, which keeps 16 bits between them. None of it calls Qt, so it works
// on any goroutine and costs no cgo call.

const ushrtMax = 65535

// qRound is Qt's qRound(float) for the platforms Qt builds it for: ARM64
// rounds half away from zero in one instruction, others add ±0.5 and
// truncate (which differs where f + 0.5 rounds up in float32).
func qRound(f float32) int {
	if runtime.GOARCH == "arm64" {
		return int(float32(math.Round(float64(f))))
	}
	if f >= 0 {
		return int(f + 0.5)
	}
	return int(f - 0.5)
}

// qtDiv257 rounds a 16-bit component to 8 bits as QColor's getters do
// (qt_div_257: x / 257 to the nearest).
func qtDiv257(x int) uint8 {
	x += 128
	return uint8((x - (x >> 8)) >> 8)
}

// hsv16 and hsl16 are Qt's 16-bit HSV and HSL: hue in hundredths of a
// degree (USHRT_MAX for an achromatic colour), the rest 0-65535.
type hsv16 struct{ h, s, v int }
type hsl16 struct{ h, s, l int }

// channels16 are the colour's 16-bit channels, as QColor stores 8-bit ones.
func (c QColor) channels16() (r, g, b int) { return int(c.R) * 257, int(c.G) * 257, int(c.B) * 257 }

// hue16Of is the hue step of QColor::toHsv and toHsl (Qt 6.12): from the
// 16-bit channels, their maximum and the chroma.
func hue16Of(r, g, b, max int, chroma float32) int {
	var hue float32
	switch max {
	case r:
		hue = float32(g-b) / chroma
		if hue < 0 {
			hue += 6
		}
	case g:
		hue = 2 + float32(b-r)/chroma
	default:
		hue = 4 + float32(r-g)/chroma
	}
	return qRound(float32(hue * (60 * 100)))
}

func (c QColor) toHsv() hsv16 {
	r, g, b := c.channels16()
	mn, mx := min(r, g, b), max(r, g, b)
	if mn == mx {
		return hsv16{h: ushrtMax, s: 0, v: mx}
	}
	chroma := float32(mx - mn)
	return hsv16{
		h: hue16Of(r, g, b, mx, chroma),
		s: qRound(float32(chroma/float32(mx)) * ushrtMax),
		v: mx,
	}
}

func (c QColor) toHsl() hsl16 {
	r, g, b := c.channels16()
	mn, mx := min(r, g, b), max(r, g, b)
	if mn == mx {
		return hsl16{h: ushrtMax, s: 0, l: mx}
	}
	chroma := float32(mx - mn)
	lightness := float32(0.5 * float32(mx+mn))
	sat := float32(0.5*chroma) / min(lightness, ushrtMax-lightness)
	return hsl16{
		h: hue16Of(r, g, b, mx, chroma),
		s: qRound(float32(sat * ushrtMax)),
		l: qRound(lightness),
	}
}

// rgb16 rounds 16-bit channels to a QColor with alpha a (0-255).
func rgb16(r, g, b int, a uint8) QColor {
	return QColor{qtDiv257(r), qtDiv257(g), qtDiv257(b), a}
}

func (x hsv16) rgb(a uint8) QColor {
	if x.s == 0 || x.h == ushrtMax {
		return rgb16(x.v, x.v, x.v, a)
	}
	var h float32
	if x.h != 36000 {
		h = float32(x.h) / 6000
	}
	s := float32(x.s) / ushrtMax
	v := float32(x.v) / ushrtMax
	i := int(h)
	f := h - float32(i)
	p := float32(v * float32(1-s))
	vi := qRound(float32(v * ushrtMax))
	pi := qRound(float32(p * ushrtMax))
	if i&1 != 0 {
		q := float32(v * (1 - float32(s*f)))
		qi := qRound(float32(q * ushrtMax))
		switch i {
		case 1:
			return rgb16(qi, vi, pi, a)
		case 3:
			return rgb16(pi, qi, vi, a)
		default:
			return rgb16(vi, pi, qi, a)
		}
	}
	t := float32(v * (1 - float32(s*(1-f))))
	ti := qRound(float32(t * ushrtMax))
	switch i {
	case 0:
		return rgb16(vi, ti, pi, a)
	case 2:
		return rgb16(pi, vi, ti, a)
	default:
		return rgb16(ti, pi, vi, a)
	}
}

func (x hsl16) rgb(a uint8) QColor {
	if x.s == 0 || x.h == ushrtMax {
		return rgb16(x.l, x.l, x.l, a)
	}
	if x.l == 0 {
		return QColor{0, 0, 0, a}
	}
	var h float32
	if x.h != 36000 {
		h = float32(x.h) / 36000
	}
	s := float32(x.s) / ushrtMax
	l := float32(x.l) / ushrtMax
	var temp2 float32
	if l < 0.5 {
		temp2 = float32(l * (1 + s))
	} else {
		temp2 = l + s - float32(l*s)
	}
	temp1 := float32(2*l) - temp2
	var ch [3]int
	for i, t := range [3]float32{h + float32(1.0)/3, h, h - float32(1.0)/3} {
		if t < 0 {
			t += 1
		} else if t > 1 {
			t -= 1
		}
		six := float32(t * 6)
		switch {
		case six < 1:
			ch[i] = qRound(float32((temp1 + float32((temp2-temp1)*six)) * ushrtMax))
		case float32(t*2) < 1:
			ch[i] = qRound(float32(temp2 * ushrtMax))
		case float32(t*3) < 2:
			ch[i] = qRound(float32((temp1 + float32(float32((temp2-temp1)*(float32(2.0)/3-t))*6)) * ushrtMax))
		default:
			ch[i] = qRound(float32(temp1 * ushrtMax))
		}
		if ch[i] == 1 {
			ch[i] = 0
		}
	}
	return rgb16(ch[0], ch[1], ch[2], a)
}

// Lighter returns a lighter colour, like QColor::lighter: factor 150 is 50%
// brighter, 100 the same colour, below 100 darker (Darker(10000 / factor)),
// and 0 or less the colour unchanged.
func (c QColor) Lighter(factor int) QColor {
	if factor <= 0 {
		return c
	}
	if factor < 100 {
		return c.Darker(10000 / factor)
	}
	x := c.toHsv()
	s, v := x.s, factor*x.v/100
	if v > ushrtMax {
		// overflow: lighten towards white by taking saturation out
		s -= v - ushrtMax
		if s < 0 {
			s = 0
		}
		v = ushrtMax
	}
	return hsv16{x.h, s, v}.rgb(c.A)
}

// Darker returns a darker colour, like QColor::darker: factor 300 is a third
// as bright, 100 the same colour, below 100 lighter (Lighter(10000 /
// factor)), and 0 or less the colour unchanged.
func (c QColor) Darker(factor int) QColor {
	if factor <= 0 {
		return c
	}
	if factor < 100 {
		return c.Lighter(10000 / factor)
	}
	x := c.toHsv()
	return hsv16{x.h, x.s, x.v * 100 / factor}.rgb(c.A)
}

// Hsv returns the HSV hue (0-359, -1 for an achromatic colour), saturation
// and value (0-255), like QColor::getHsv.
func (c QColor) Hsv() (h, s, v int) {
	x := c.toHsv()
	return hue100(x.h), int(qtDiv257(x.s)), int(qtDiv257(x.v))
}

// HsvF returns the HSV hue, saturation and value as 0-1 (hue -1 for an
// achromatic colour), like QColor::getHsvF.
func (c QColor) HsvF() (h, s, v float32) {
	x := c.toHsv()
	return hueF(x.h), float32(x.s) / ushrtMax, float32(x.v) / ushrtMax
}

// Hsl returns the HSL hue (0-359, -1 for an achromatic colour), saturation
// and lightness (0-255), like QColor::getHsl.
func (c QColor) Hsl() (h, s, l int) {
	x := c.toHsl()
	return hue100(x.h), int(qtDiv257(x.s)), int(qtDiv257(x.l))
}

// HslF returns the HSL hue, saturation and lightness as 0-1 (hue -1 for an
// achromatic colour), like QColor::getHslF.
func (c QColor) HslF() (h, s, l float32) {
	x := c.toHsl()
	return hueF(x.h), float32(x.s) / ushrtMax, float32(x.l) / ushrtMax
}

// Hue is the HSV hue, 0-359 or -1 for an achromatic colour (QColor::hue).
func (c QColor) Hue() int { return hue100(c.toHsv().h) }

// Saturation is the HSV saturation, 0-255 (QColor::saturation).
func (c QColor) Saturation() int { return int(qtDiv257(c.toHsv().s)) }

// Value is the HSV value, 0-255 (QColor::value).
func (c QColor) Value() int { return int(qtDiv257(c.toHsv().v)) }

// Lightness is the HSL lightness, 0-255 (QColor::lightness).
func (c QColor) Lightness() int { return int(qtDiv257(c.toHsl().l)) }

// LightnessF is the HSL lightness, 0-1 (QColor::lightnessF).
func (c QColor) LightnessF() float32 { return float32(c.toHsl().l) / ushrtMax }

// RedF, GreenF, BlueF and AlphaF are the channels as 0-1 (QColor::redF...).
func (c QColor) RedF() float32   { return float32(int(c.R)*257) / ushrtMax }
func (c QColor) GreenF() float32 { return float32(int(c.G)*257) / ushrtMax }
func (c QColor) BlueF() float32  { return float32(int(c.B)*257) / ushrtMax }
func (c QColor) AlphaF() float32 { return float32(int(c.A)*257) / ushrtMax }

// WithAlpha returns the colour with alpha a (0-255).
func (c QColor) WithAlpha(a uint8) QColor { c.A = a; return c }

// WithAlphaF returns the colour with alpha a (0-1, clamped), rounded as
// QColor::setAlphaF does.
func (c QColor) WithAlphaF(a float32) QColor {
	c.A = unit8(a)
	return c
}

func hue100(h int) int {
	if h == ushrtMax {
		return -1
	}
	return h / 100
}

func hueF(h int) float32 {
	if h == ushrtMax {
		return -1
	}
	return float32(h) / 36000
}

// unit8 converts a 0-1 component (clamped) to 8 bits through Qt's 16 bits.
func unit8(f float32) uint8 {
	return qtDiv257(qRound(float32(clamp01(f) * ushrtMax)))
}

func clamp01(f float32) float32 {
	if f != f || f < 0 { // NaN as 0
		return 0
	}
	return min(f, 1)
}

// ColorFromRgbF returns a colour from 0-1 channels, like QColor::fromRgbF
// rounded to 8 bits. Alpha outside 0-1 gives the invalid (zero) colour; a
// channel outside it makes Qt keep the colour at half precision, extended
// beyond 0-1, and its 8 bits are then those of the clamped half-precision
// channels, as here.
func ColorFromRgbF(r, g, b, a float32) QColor {
	if !in01(a) {
		return QColor{}
	}
	if in01(r) && in01(g) && in01(b) {
		return QColor{unit8(r), unit8(g), unit8(b), unit8(a)}
	}
	return QColor{unit8(half(r)), unit8(half(g)), unit8(half(b)), unit8(half(a))}
}

// half rounds f to the nearest half-precision (IEEE binary16) value, ties to
// even, as qfloat16 does; only its clamped 0-1 range matters here.
func half(f float32) float32 {
	if f != f || f <= 0 {
		return 0 // clamped to 0 afterwards anyway (NaN as 0)
	}
	if f >= 1 {
		return 1
	}
	// 10 mantissa bits; below 2^-14 the step is that of the subnormals
	exp := math.Floor(math.Log2(float64(f)))
	exp = max(exp, -14)
	step := math.Ldexp(1, int(exp)-10)
	return float32(math.RoundToEven(float64(f)/step) * step)
}

// ColorFromHsv returns a colour from HSV, like QColor::fromHsv: hue 0-359
// (-1 for achromatic), saturation, value and alpha 0-255.
// Out-of-range values give the invalid (zero) colour, as in Qt.
func ColorFromHsv(h, s, v, a int) QColor {
	if h < -1 || h >= 360 || uint(s) > 255 || uint(v) > 255 || uint(a) > 255 {
		return QColor{}
	}
	return hsv16{hue16(h), s * 257, v * 257}.rgb(uint8(a))
}

// ColorFromHsvF returns a colour from HSV components 0-1 (hue -1 for
// achromatic), like QColor::fromHsvF. Out-of-range values give the invalid
// (zero) colour, as in Qt.
func ColorFromHsvF(h, s, v, a float32) QColor {
	if !validHueF(h) || !in01(s) || !in01(v) || !in01(a) {
		return QColor{}
	}
	return hsv16{hue16F(h), qRound(float32(s * ushrtMax)), qRound(float32(v * ushrtMax))}.rgb(unit8(a))
}

// ColorFromHsl returns a colour from HSL, like QColor::fromHsl: hue 0-359
// (-1 for achromatic), saturation, lightness and alpha 0-255. Out-of-range values give the invalid (zero) colour, as in Qt.
func ColorFromHsl(h, s, l, a int) QColor {
	if h < -1 || h >= 360 || uint(s) > 255 || uint(l) > 255 || uint(a) > 255 {
		return QColor{}
	}
	return hsl16{hue16(h), s * 257, l * 257}.rgb(uint8(a))
}

// ColorFromHslF returns a colour from HSL components 0-1 (hue -1 for
// achromatic), like QColor::fromHslF. Out-of-range values give the invalid
// (zero) colour, as in Qt.
func ColorFromHslF(h, s, l, a float32) QColor {
	if !validHueF(h) || !in01(s) || !in01(l) || !in01(a) {
		return QColor{}
	}
	hue := hue16F(h)
	if hue == 36000 {
		hue = 0 // as QColor::fromHslF
	}
	return hsl16{hue, qRound(float32(s * ushrtMax)), qRound(float32(l * ushrtMax))}.rgb(unit8(a))
}

func hue16(h int) int {
	if h == -1 {
		return ushrtMax
	}
	return h * 100
}

func hue16F(h float32) int {
	if h == -1 {
		return ushrtMax
	}
	return qRound(float32(h * 36000))
}

func validHueF(h float32) bool { return h == -1 || in01(h) }

func in01(f float32) bool { return f >= 0 && f <= 1 }
