package qute_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

// These tests exercise each kind of type the generator maps beyond the basics.

func TestOutParameters(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		defer w.Delete()
		lay := qtwidgets.NewQVBoxLayout(w)
		lay.SetContentsMargins(qtcore.QMargins{Left: 1, Top: 2, Right: 3, Bottom: 4})
		l, tp, r, b := lay.GetContentsMargins()
		if l != 1 || tp != 2 || r != 3 || b != 4 {
			t.Errorf("GetContentsMargins = %d %d %d %d", l, tp, r, b)
		}
		g := qtwidgets.NewQGridLayout(nil)
		defer g.Delete()
		g.AddWidgetSpan(qtwidgets.NewQLabelWithText("x", nil), 2, 3, 1, 2)
		row, col, rs, cs := g.GetItemPosition(0)
		if row != 2 || col != 3 || rs != 1 || cs != 2 {
			t.Errorf("GetItemPosition = %d %d %d %d", row, col, rs, cs)
		}
	})
}

func TestChainingReturnsReceiver(t *testing.T) {
	qutetest.Run(t, func() {
		tr := qtgui.NewQTransform()
		if got := tr.Translate(10, 20).Rotate(0); got != tr {
			t.Error("Translate did not return the receiver")
		}
		if tr.Dx() != 10 || tr.Dy() != 20 {
			t.Errorf("translation = %v,%v", tr.Dx(), tr.Dy())
		}
	})
}

func TestByteBuffersAndPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	qutetest.Run(t, func() {
		f := qtcore.NewQFileWithName(path, nil)
		defer f.Delete()
		if !f.Open(qtcore.QIODeviceBaseWriteOnly) {
			t.Fatal("open for write failed")
		}
		if n := f.Write([]byte("hello, qute")); n != 11 {
			t.Errorf("Write = %d", n)
		}
		f.Close()
		if !f.Open(qtcore.QIODeviceBaseReadOnly) {
			t.Fatal("open for read failed")
		}
		buf := make([]byte, 5)
		if n := f.Read(buf); n != 5 || string(buf) != "hello" {
			t.Errorf("Read = %d %q", n, buf)
		}
		f.Close()
		if got := f.FilesystemFileName(); got != path {
			t.Errorf("FilesystemFileName = %q, want %q", got, path)
		}
	})
	if b, _ := os.ReadFile(path); string(b) != "hello, qute" {
		t.Errorf("file holds %q", b)
	}
}

func TestMapsAndJSON(t *testing.T) {
	qutetest.Run(t, func() {
		m := qtcore.NewQStringListModelWithStrings([]string{"a"}, nil)
		defer m.Delete()
		roles := m.RoleNames()
		if string(roles[int(qtcore.DisplayRole)]) != "display" {
			t.Errorf("RoleNames()[DisplayRole] = %q", roles[int(qtcore.DisplayRole)])
		}
		v := qtcore.NewQVariantWithMap(map[string]*qtcore.QVariant{"n": qtcore.NewQVariantOf(7)})
		if got := v.ToMap()["n"].Value(); got != int64(7) {
			t.Errorf("round-tripped map value = %v", got)
		}
		doc := qtcore.QJsonDocumentFromJson([]byte(`{"name": "qute"}`))
		if got := doc.Object().Value("name").ToString(); got != "qute" {
			t.Errorf("JSON name = %q", got)
		}
	})
}

func TestDurationsAndTimes(t *testing.T) {
	qutetest.Run(t, func() {
		tm := qtcore.NewQTimer(nil)
		defer tm.Delete()
		tm.SetIntervalDuration(1500 * time.Millisecond)
		if got := tm.IntervalAsDuration(); got != 1500*time.Millisecond {
			t.Errorf("IntervalAsDuration = %v", got)
		}
		when := time.UnixMilli(1700000000123)
		dt := qtcore.QDateTimeFromStdTimePoint(when)
		if got := dt.ToMSecsSinceEpoch(); got != when.UnixMilli() {
			t.Errorf("ToMSecsSinceEpoch = %d", got)
		}
	})
}

func TestFieldsAndNestedClasses(t *testing.T) {
	qutetest.Run(t, func() {
		opt := qtwidgets.NewQStyleOption()
		opt.SetRect(qtcore.QRect{X: 1, Y: 2, Width: 30, Height: 40})
		if got := opt.Rect(); got != (qtcore.QRect{X: 1, Y: 2, Width: 30, Height: 40}) {
			t.Errorf("Rect = %v", got)
		}
		tag := qtgui.QFontTagFromString("liga")
		if !tag.IsValid() || string(tag.ToString()) != "liga" {
			t.Errorf("QFont::Tag = %q valid=%v", tag.ToString(), tag.IsValid())
		}
		edit := qtwidgets.NewQTextEdit(nil)
		defer edit.Delete()
		edit.SetPlainText("abc")
		sel := qtwidgets.NewQTextEditExtraSelection()
		sel.SetCursor(edit.TextCursor())
		edit.SetExtraSelections([]*qtwidgets.QTextEditExtraSelection{sel})
		if n := len(edit.ExtraSelections()); n != 1 {
			t.Errorf("ExtraSelections len = %d", n)
		}
	})
}

func TestCallbackArgument(t *testing.T) {
	qutetest.Run(t, func() {
		lbl := qtwidgets.NewQLabel(nil)
		defer lbl.Delete()
		asked := ""
		lbl.SetResourceProvider(func(u *qtcore.QUrl) *qtcore.QVariant {
			asked = u.ToString()
			return nil
		})
		lbl.SetText(`<img src="pic.png">`)
		qute.Screenshot(lbl) // rendering asks the provider for the image
		if asked != "pic.png" {
			t.Errorf("resource provider asked for %q", asked)
		}
	})
}

// NewQImageFromBytes copies, so the Go slice may change afterwards.
func TestImageFromBytesCopies(t *testing.T) {
	qutetest.Run(t, func() {
		data := make([]byte, 2*2*4)
		for i := range data {
			data[i] = 0xff
		}
		img := qtgui.NewQImageFromBytes(data, 2, 2, 8, qtgui.QImageFormat_ARGB32)
		for i := range data {
			data[i] = 0
		}
		if got := img.Pixel(1, 1); got != 0xffffffff {
			t.Errorf("Pixel = %#x, want 0xffffffff", got)
		}
		short := qtgui.NewQImageFromBytes(data[:8], 2, 2, 8, qtgui.QImageFormat_ARGB32)
		if got := short.Pixel(0, 1); got != 0 {
			t.Errorf("a row missing from the data = %#x, want 0", got)
		}
	})
}

// GUI values go into a QVariant and come back out, so a Go item model can
// return icons, colours and fonts for their roles.
func TestGUIValuesInVariants(t *testing.T) {
	qutetest.Run(t, func() {
		pix := qtgui.NewQPixmapWithW(4, 4)
		pix.FillFull(qtgui.RGB(10, 20, 30))
		icon := qtgui.NewQIconWithPixmap(pix)
		font := qtgui.NewQFontWithFamilyFull("Sans", 17, -1, false)

		model := qtgui.NewQStandardItemModel(nil)
		item := qtgui.NewQStandardItemWithText("x")
		item.SetDataFull(qtgui.NewQVariantOf(icon), int(qtcore.DecorationRole))
		item.SetDataFull(qtgui.NewQVariantOf(qtgui.RGB(1, 2, 3)), int(qtcore.ForegroundRole))
		item.SetDataFull(qtgui.NewQVariantOf(font), int(qtcore.FontRole))
		model.AppendRow(item)
		idx := model.Index(0, 0)

		if got, ok := qtgui.VariantValue(model.DataFull(idx, int(qtcore.DecorationRole))).(*qtgui.QIcon); !ok || got.IsNull() {
			t.Errorf("DecorationRole = %T, want a non-null *QIcon", got)
		} else if got.Ownership() != qtcore.Owned {
			t.Errorf("icon from a variant: %v, want Owned", got.Ownership())
		}
		if got := qtgui.VariantValue(model.DataFull(idx, int(qtcore.ForegroundRole))); got != qtgui.RGB(1, 2, 3) {
			t.Errorf("ForegroundRole = %v, want RGB(1, 2, 3)", got)
		}
		if got, ok := qtgui.VariantValue(model.DataFull(idx, int(qtcore.FontRole))).(*qtgui.QFont); !ok || got.PointSize() != 17 {
			t.Errorf("FontRole = %v, want a 17pt *QFont", got)
		}
		if got := qtgui.VariantValue(qtgui.NewQVariantOf("plain")); got != "plain" {
			t.Errorf("a plain value = %v, want it passed through", got)
		}
		var nilIcon *qtgui.QIcon
		if v := qtgui.NewQVariantOf(nilIcon); v.IsValid() {
			t.Error("a nil icon gave a valid variant")
		}
		model.Delete()
	})
}

// Every GUI kind NewQVariantOf takes comes back from VariantValue as an
// Owned copy of the same type.
func TestEveryGUIVariantKindRoundTrips(t *testing.T) {
	qutetest.Run(t, func() {
		values := []any{
			qtgui.NewQIcon(), qtgui.NewQFont(), qtgui.NewQBrush(), qtgui.NewQPixmapWithW(2, 2),
			qtgui.NewQImageWithSize(qtcore.QSize{Width: 2, Height: 2}, qtgui.QImageFormat_ARGB32),
			qtgui.NewQPen(), qtgui.NewQKeySequence(), qtgui.NewQCursor(), qtgui.NewQPalette(),
			qtgui.NewQTransform(), qtgui.NewQRegion(),
		}
		for _, v := range values {
			got := qtgui.VariantValue(qtgui.NewQVariantOf(v))
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", v) {
				t.Errorf("%T came back as %T", v, got)
				continue
			}
			if o, ok := got.(interface{ Ownership() qtcore.Ownership }); !ok || o.Ownership() != qtcore.Owned {
				t.Errorf("%T from a variant is not Owned", got)
			}
		}
	})
}

// A defaulted out-parameter (bool *ok) is a result of the method's Full
// form: these used to have no Full form at all.
func TestDefaultedOutParameters(t *testing.T) {
	qutetest.Run(t, func() {
		loc := qtcore.NewQLocale()
		if n, ok := loc.ToIntFull("12"); n != 12 || !ok {
			t.Errorf(`ToIntFull("12") = %d, %v`, n, ok)
		}
		if _, ok := loc.ToIntFull("twelve"); ok {
			t.Error(`ToIntFull("twelve") reported ok`)
		}
		if _, ok := qtcore.NewQVariantWithString("x").ToIntFull(); ok {
			t.Error(`a QVariant of "x" converted to an int`)
		}
		singular := qtgui.NewQTransformAffine(0, 0, 0, 0, 0, 0)
		if _, ok := singular.InvertedFull(); ok {
			t.Error("a singular transform reported invertible")
		}
		l := qtcore.NewQLineFWithPointF(qtcore.QPointF{}, qtcore.QPointF{X: 10, Y: 10})
		kind, at := l.IntersectsFull(qtcore.NewQLineFWithPointF(qtcore.QPointF{Y: 10}, qtcore.QPointF{X: 10}))
		if kind != qtcore.QLineFBoundedIntersection || at != (qtcore.QPointF{X: 5, Y: 5}) {
			t.Errorf("IntersectsFull = %v, %v, want a bounded intersection at (5, 5)", kind, at)
		}
	})
}

// Classes with Qt's operator QVariant() get ToQVariant, and NewQVariantOf
// accepts any of them (a QTextFormat for an input method's preedit colour).
func TestConversionsToVariant(t *testing.T) {
	qutetest.Run(t, func() {
		cf := qtgui.NewQTextCharFormat()
		cf.SetForeground(qtgui.NewQBrushWithColor(qtgui.RGB(255, 0, 0)))
		for name, v := range map[string]interface{ ToQVariant() *qtcore.QVariant }{
			"QTextFormat": cf.AsQTextFormat(),
			"QTextLength": qtgui.NewQTextLengthWithType(qtgui.QTextLengthFixedLength, 3),
			"QMatrix4x4":  qtgui.NewQMatrix4x4(),
			"QVector2D":   qtgui.NewQVector2D(),
			"QSizePolicy": qtwidgets.NewQSizePolicy(),
		} {
			if got := v.ToQVariant().TypeName(); got != name {
				t.Errorf("%s.ToQVariant().TypeName() = %q", name, got)
			}
			if got := qtgui.NewQVariantOf(v).TypeName(); got != name {
				t.Errorf("qtgui.NewQVariantOf(%s).TypeName() = %q", name, got)
			}
			if got := qtcore.NewQVariantOf(v).TypeName(); got != name {
				t.Errorf("qtcore.NewQVariantOf(%s).TypeName() = %q", name, got)
			}
		}
		var none *qtgui.QTextFormat
		if qtcore.NewQVariantOf(none).IsValid() {
			t.Error("a nil *QTextFormat gave a valid variant")
		}

		// the preedit text an input method shows, coloured by its format
		e := qtwidgets.NewQLineEdit(nil)
		defer e.Delete()
		e.Resize(200, 40)
		e.SetFont(qtgui.NewQFontWithFamily("Monospace"))
		attr := qtgui.NewQInputMethodEventAttributeWithValue(qtgui.QInputMethodEventTextFormat, 0, 8, cf.ToQVariant())
		ev := qtgui.NewQInputMethodEventWithPreeditText("WWWWWWWW", []*qtgui.QInputMethodEventAttribute{attr})
		qtcore.QCoreApplicationSendEvent(e, ev)
		img := qute.Screenshot(e)
		red := 0
		for i := 0; i < len(img.Pix); i += 4 {
			if r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]; r > 150 && g < 80 && b < 80 {
				red++
			}
		}
		if red == 0 {
			t.Error("the preedit text was not drawn in its format's red")
		}
	})
}

// NewQVariantOf of a *QVariant used to go through Value(), which only knows
// the basic types: a colour came back as its name, a map as a null variant.
// It is a real copy now, and a nil one a null variant, as for other handles.
func TestVariantOfVariantKeepsItsType(t *testing.T) {
	qutetest.Run(t, func() {
		colour := qtgui.NewQVariantOf(qtgui.RGB(1, 2, 3))
		cp := qtcore.NewQVariantOf(colour)
		if cp == colour || cp.Ptr() == colour.Ptr() {
			t.Error("the copy is the same object as the original")
		}
		if got := qtgui.VariantValue(cp); got != qtgui.RGB(1, 2, 3) {
			t.Errorf("copy of a colour variant = %#v (%s), want RGB(1, 2, 3)", got, cp.TypeName())
		}
		// the copy does not depend on the original
		colour.Delete()
		if got := qtgui.VariantValue(cp); got != qtgui.RGB(1, 2, 3) {
			t.Errorf("copy after the original was deleted = %#v, want RGB(1, 2, 3)", got)
		}

		m := qtcore.NewQVariantWithMap(map[string]*qtcore.QVariant{"n": qtcore.NewQVariantOf(7)})
		if got := qtcore.NewQVariantOf(m).ToMap(); len(got) != 1 || got["n"] == nil || got["n"].ToInt() != 7 {
			t.Errorf("copy of a map variant = %v, want one entry n = 7", got)
		}

		if v := qtcore.NewQVariantOf((*qtcore.QVariant)(nil)); v == nil || v.IsValid() {
			t.Errorf("NewQVariantOf of a nil *QVariant = %v, want an invalid variant", v)
		}
	})
}
