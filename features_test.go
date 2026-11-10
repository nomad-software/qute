package qute_test

import (
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
