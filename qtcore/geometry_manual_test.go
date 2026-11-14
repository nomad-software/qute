package qtcore_test

import (
	"testing"

	"github.com/nomad-software/qute/internal/georef"
	"github.com/nomad-software/qute/qtcore"
)

// QRect's Go methods agree with Qt's own over a grid that includes empty and
// negative sizes, where a naive port goes wrong (Center of {-1, 2} is 0,
// rounded toward zero).
func TestRectMatchesQt(t *testing.T) {
	vals := []int{-3, -2, -1, 0, 1, 2, 3}
	var rects []qtcore.QRect
	for _, x := range []int{-2, 0, 1} {
		for _, w := range vals {
			for _, h := range []int{-2, 0, 1, 3} {
				rects = append(rects, qtcore.QRect{X: x, Y: x - 1, Width: w, Height: h})
			}
		}
	}
	bad := 0
	for _, r := range rects {
		for _, o := range rects {
			px, py := o.X+o.Width, o.Y-o.Height // points around r, from the grid
			q := georef.RectOps([4]int32{int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height)},
				[2]int32{int32(px), int32(py)}, [4]int32{int32(o.X), int32(o.Y), int32(o.Width), int32(o.Height)})
			c := r.Center()
			got := georef.Rect{CenterX: c.X, CenterY: c.Y, Contains: r.Contains(qtcore.QPoint{X: px, Y: py}),
				Intersects: r.Intersects(o), Empty: r.IsEmpty()}
			if got != q && bad < 10 {
				bad++
				t.Errorf("%v with point (%d,%d) and %v: Go %+v, Qt %+v", r, px, py, o, got, q)
			}
		}
	}
}

// QRectF's Contains and ToRect agree with Qt's, which rounds the size so no
// edge moves by more than 0.75 and contains nothing for a zero width.
func TestRectFMatchesQt(t *testing.T) {
	vals := []float64{-2.5, -1.25, -0.5, 0, 0.4, 0.5, 1.4, 2.5}
	bad := 0
	for _, x := range vals {
		for _, w := range vals {
			for _, p := range vals {
				r := qtcore.QRectF{X: x, Y: -x, Width: w, Height: 1.5}
				pt := qtcore.QPointF{X: p, Y: -x + 0.75}
				tr, contains := georef.RectFOps([4]float64{r.X, r.Y, r.Width, r.Height}, [2]float64{pt.X, pt.Y})
				g := r.ToRect()
				if ([4]int{g.X, g.Y, g.Width, g.Height} != tr || r.Contains(pt) != contains) && bad < 10 {
					bad++
					t.Errorf("%+v, point %+v: Go ToRect %v Contains %v; Qt %v %v", r, pt, g, r.Contains(pt), tr, contains)
				}
			}
		}
	}
}

// The point and rectangle helpers with no Qt counterpart to compare with.
func TestGeometryHelpers(t *testing.T) {
	p := qtcore.QPoint{X: 3, Y: -4}
	if got := p.Sub(qtcore.QPoint{X: 1, Y: 1}); got != (qtcore.QPoint{X: 2, Y: -5}) {
		t.Errorf("Sub = %v", got)
	}
	if got := p.ToF(); got != (qtcore.QPointF{X: 3, Y: -4}) {
		t.Errorf("QPoint.ToF = %v", got)
	}
	if got := (qtcore.QPointF{X: 2.5, Y: -2.5}).ToPoint(); got != (qtcore.QPoint{X: 3, Y: -3}) {
		t.Errorf("ToPoint = %v, want halves rounded away from zero", got)
	}
	r := qtcore.QRect{X: 1, Y: 2, Width: 10, Height: 20}
	if got := r.TopLeft(); got != (qtcore.QPoint{X: 1, Y: 2}) {
		t.Errorf("TopLeft = %v", got)
	}
	if got := r.Translated(qtcore.QPoint{X: -1, Y: 3}); got != (qtcore.QRect{X: 0, Y: 5, Width: 10, Height: 20}) {
		t.Errorf("Translated = %v", got)
	}
	if got := r.Adjusted(1, 2, -3, 4); got != (qtcore.QRect{X: 2, Y: 4, Width: 6, Height: 22}) {
		t.Errorf("Adjusted = %v", got)
	}
	if got := r.ToF(); got != (qtcore.QRectF{X: 1, Y: 2, Width: 10, Height: 20}) {
		t.Errorf("QRect.ToF = %v", got)
	}
}
