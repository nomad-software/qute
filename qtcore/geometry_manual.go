// Hand-written; qutegen never overwrites *_manual.go files.

package qtcore

import "fmt"

// Small Qt value types become plain Go values at the boundary, so they never
// need freeing.

// QPoint is Qt's QPoint: an integer point.
type QPoint struct{ X, Y int }

// QPointF is Qt's QPointF: a floating-point point.
type QPointF struct{ X, Y float64 }

// QSize is Qt's QSize. Qt's invalid size is {-1, -1}.
type QSize struct{ Width, Height int }

// QSizeF is Qt's QSizeF.
type QSizeF struct{ Width, Height float64 }

// QRect is Qt's QRect, stored as origin plus size.
type QRect struct{ X, Y, Width, Height int }

// QRectF is Qt's QRectF, stored as origin plus size.
type QRectF struct{ X, Y, Width, Height float64 }

// QMargins is Qt's QMargins.
type QMargins struct{ Left, Top, Right, Bottom int }

func (p QPoint) String() string  { return fmt.Sprintf("(%d,%d)", p.X, p.Y) }
func (s QSize) String() string   { return fmt.Sprintf("%dx%d", s.Width, s.Height) }
func (r QRect) String() string   { return fmt.Sprintf("%dx%d+%d+%d", r.Width, r.Height, r.X, r.Y) }
func (p QPointF) String() string { return fmt.Sprintf("(%g,%g)", p.X, p.Y) }

// Add returns p+q.
func (p QPoint) Add(q QPoint) QPoint { return QPoint{p.X + q.X, p.Y + q.Y} }

// Sub returns p-q.
func (p QPoint) Sub(q QPoint) QPoint { return QPoint{p.X - q.X, p.Y - q.Y} }

// ToF converts to a QPointF.
func (p QPoint) ToF() QPointF { return QPointF{float64(p.X), float64(p.Y)} }

// ToPoint rounds to a QPoint.
func (p QPointF) ToPoint() QPoint { return QPoint{roundInt(p.X), roundInt(p.Y)} }

// IsValid reports whether both dimensions are non-negative.
func (s QSize) IsValid() bool { return s.Width >= 0 && s.Height >= 0 }

// IsEmpty reports whether either dimension is zero or negative.
func (s QSize) IsEmpty() bool { return s.Width <= 0 || s.Height <= 0 }

// IsEmpty reports whether the rectangle has no area.
func (r QRect) IsEmpty() bool { return r.Width <= 0 || r.Height <= 0 }

// Left, Top, Right and Bottom follow Qt: Right is X+Width-1.
func (r QRect) Left() int   { return r.X }
func (r QRect) Top() int    { return r.Y }
func (r QRect) Right() int  { return r.X + r.Width - 1 }
func (r QRect) Bottom() int { return r.Y + r.Height - 1 }

// Size returns the rectangle's size.
func (r QRect) Size() QSize { return QSize{r.Width, r.Height} }

// TopLeft returns the origin.
func (r QRect) TopLeft() QPoint { return QPoint{r.X, r.Y} }

// The QRect methods below follow Qt's code, which keeps the corners x1, y1
// and x2, y2 = x1+width-1, y1+height-1, so they agree with Qt for negative
// sizes too (geometry_manual_test.go compares them with Qt's).

// Center returns the centre point: Qt's (x1+x2)/2, rounded toward zero.
func (r QRect) Center() QPoint {
	return QPoint{int((int64(r.X) + int64(r.Right())) / 2), int((int64(r.Y) + int64(r.Bottom())) / 2)}
}

// span returns the inclusive range a rectangle side covers, as QRect does
// for a negative size (x2 < x1-1); it is empty (lo > hi) for a zero size.
func span(x1, x2 int) (lo, hi int) {
	if x2 < x1-1 {
		return x2 + 1, x1 - 1
	}
	return x1, x2
}

// Contains reports whether p lies inside the rectangle (on its edge
// included), as QRect::contains(p).
func (r QRect) Contains(p QPoint) bool {
	l, rr := span(r.X, r.Right())
	t, b := span(r.Y, r.Bottom())
	return p.X >= l && p.X <= rr && p.Y >= t && p.Y <= b
}

// Intersects reports whether the rectangles overlap, as QRect::intersects:
// a null rectangle (no width and no height) never does; otherwise each side
// is normalised as for Contains, with no check that it is empty, so a
// rectangle with only a zero width or height can, as in Qt.
func (r QRect) Intersects(o QRect) bool {
	if r.Width == 0 && r.Height == 0 || o.Width == 0 && o.Height == 0 {
		return false
	}
	l1, r1 := span(r.X, r.Right())
	l2, r2 := span(o.X, o.Right())
	t1, b1 := span(r.Y, r.Bottom())
	t2, b2 := span(o.Y, o.Bottom())
	return l1 <= r2 && l2 <= r1 && t1 <= b2 && t2 <= b1
}

// Translated returns the rectangle moved by d.
func (r QRect) Translated(d QPoint) QRect { return QRect{r.X + d.X, r.Y + d.Y, r.Width, r.Height} }

// Adjusted returns the rectangle with dx1/dy1 added to the top-left and
// dx2/dy2 added to the bottom-right corner.
func (r QRect) Adjusted(dx1, dy1, dx2, dy2 int) QRect {
	return QRect{r.X + dx1, r.Y + dy1, r.Width - dx1 + dx2, r.Height - dy1 + dy2}
}

// ToF converts to a QRectF.
func (r QRect) ToF() QRectF {
	return QRectF{float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height)}
}

// Contains reports whether p lies inside the rectangle (on its edge
// included), as QRectF::contains(p): never for a rectangle of no width or
// height, and over the covered area for a negative size.
func (r QRectF) Contains(p QPointF) bool {
	inside := func(x, w, px float64) bool {
		l, rr := x, x
		if w < 0 {
			l += w
		} else {
			rr += w
		}
		return l != rr && px >= l && px <= rr
	}
	return inside(r.X, r.Width, p.X) && inside(r.Y, r.Height, p.Y)
}

// ToRect rounds to a QRect as Qt does: the origin to the nearest integer,
// and the size so that no edge moves by more than 0.75.
func (r QRectF) ToRect() QRect {
	x, y := roundInt(r.X), roundInt(r.Y)
	return QRect{x, y, roundInt(r.Width + (r.X-float64(x))/2), roundInt(r.Height + (r.Y-float64(y))/2)}
}

// roundInt is Qt's qRound: halves away from zero.
func roundInt(f float64) int {
	if f < 0 {
		return int(f - 0.5)
	}
	return int(f + 0.5)
}

// QMarginsF is Qt's QMarginsF.
type QMarginsF struct{ Left, Top, Right, Bottom float64 }

// Pair is a std::pair from Qt: gradient stops, URL query items, and other
// lists of two-part entries.
type Pair[A, B any] struct {
	First  A
	Second B
}
