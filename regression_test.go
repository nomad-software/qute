package qute_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"

	"github.com/nomad-software/qute"
	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
)

// Regression tests for bindings whose signature was wrong: out-parameters
// guessed from the type alone, pointers Qt keeps without owning, and an
// override that saw a copy of Qt's object.

// copyDataTo writes 16 floats; the old binding pointed it at one stack float.
func TestMatrixCopyDataTo(t *testing.T) {
	qute.Run(t, func() {
		m := qtgui.NewQMatrix4x4()
		m.TranslateFloat32Float32(1, 2, 3)
		got := m.CopyDataTo()
		want := [16]float32{
			1, 0, 0, 1,
			0, 1, 0, 2,
			0, 0, 1, 3,
			0, 0, 0, 1,
		}
		if got != want {
			t.Errorf("CopyDataTo = %v, want row-major %v", got, want)
		}
	})
}

// The image-over-data constructor used to build the image over a dead stack
// byte and dropped the data argument. It now takes the caller's buffer.
func TestImageOverCallerBuffer(t *testing.T) {
	qute.Run(t, func() {
		const w, h = 4, 2
		buf := make([]uint32, w*h)
		for i := range buf {
			buf[i] = 0xff112233
		}
		// Qt keeps the pointer for the image's life, so the Go memory must be
		// pinned until the image is gone.
		var pin runtime.Pinner
		pin.Pin(&buf[0])
		defer pin.Unpin()
		img := qtgui.NewQImageWithPtrBytesPerLine(unsafe.Pointer(&buf[0]), w, h, w*4, qtgui.QImageFormat_ARGB32)
		if got := img.Pixel(3, 1); got != 0xff112233 {
			t.Errorf("Pixel = %#x, want 0xff112233", got)
		}
		img.Delete()
	})
}

// The byte-array stream constructor kept a pointer to a temporary; it is
// skipped now, and the read-only one has its proper name.
func TestDataStreamOverBytes(t *testing.T) {
	qute.Run(t, func() {
		ds := qtcore.NewQDataStreamWithBytes([]byte("hello"))
		buf := make([]byte, 5)
		if n := ds.ReadRawData(buf); n != 5 || string(buf) != "hello" {
			t.Errorf("ReadRawData = %d %q, want 5 \"hello\"", n, buf)
		}
		if !ds.AtEnd() {
			t.Error("AtEnd = false after reading everything")
		}
		ds.Delete()
	})
}

// Unmap takes the address Map returned (it used to pass a stack byte).
func TestFileUnmap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.bin")
	if err := os.WriteFile(path, []byte("mapped bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	qute.Run(t, func() {
		f := qtcore.NewQFileWithName(path, nil)
		defer f.Delete()
		if !f.Open(qtcore.QIODeviceBaseReadOnly) {
			t.Error("Open failed")
			return
		}
		p := f.Map(0, 6)
		if p == nil {
			t.Error("Map returned nil")
			return
		}
		if got := string(unsafe.Slice((*byte)(p), 6)); got != "mapped" {
			t.Errorf("mapped %q, want \"mapped\"", got)
		}
		if !f.Unmap(p) {
			t.Error("Unmap of the mapped address failed")
		}
	})
}

// polish(QPalette &) reads the palette it adjusts; it was bound as out-only,
// so the caller's palette never reached Qt.
func TestStylePolishPaletteReadsInput(t *testing.T) {
	qute.Run(t, func() {
		red := qtgui.RGB(255, 0, 0)
		pal := qtgui.NewQPalette()
		pal.SetColor(qtgui.QPaletteWindow, red)
		got := qtwidgets.QApplicationStyle().PolishPalette(pal)
		if c := got.Color(qtgui.QPaletteWindow); c != red {
			t.Errorf("polished Window colour = %v, want the input's %v", c, red)
		}
	})
}

type longLabelButton struct{ qtwidgets.QPushButtonBase }

const longLabel = "a label much longer than the button's own text"

func (b *longLabelButton) InitStyleOption(opt *qtwidgets.QStyleOptionButton) {
	b.Super().InitStyleOption(opt)
	opt.SetText(longLabel)
}

// An InitStyleOption override used to fill in a copy Qt never saw. The paint
// path draws from the option, so the override's label must show on screen.
func TestInitStyleOptionOverrideReachesQt(t *testing.T) {
	qute.Run(t, func() {
		shot := func(b *qtwidgets.QPushButton) []byte {
			b.Resize(360, 40)
			return qute.Screenshot(b).Pix
		}
		over := qtwidgets.NewQPushButtonWith(&longLabelButton{}, nil)
		defer over.Delete()
		over.SetText("x")
		plainShort := qtwidgets.NewQPushButtonWithText("x", nil)
		defer plainShort.Delete()
		plainLong := qtwidgets.NewQPushButtonWithText(longLabel, nil)
		defer plainLong.Delete()
		got, short, long := shot(over), shot(plainShort), shot(plainLong)
		if bytes.Equal(got, short) {
			t.Error("the override's label never reached Qt: the button paints its own text")
		}
		if !bytes.Equal(got, long) {
			t.Error("the button does not paint the label the override set")
		}
	})
}

// Scoped style option: the override's argument is Qt's object, so it must
// expire when the override returns.
type keepOptionButton struct {
	qtwidgets.QPushButtonBase
	opt *qtwidgets.QStyleOptionButton
}

func (b *keepOptionButton) InitStyleOption(opt *qtwidgets.QStyleOptionButton) {
	b.Super().InitStyleOption(opt)
	b.opt = opt
}

func TestInitStyleOptionArgumentIsScoped(t *testing.T) {
	qute.Run(t, func() {
		v := &keepOptionButton{}
		btn := qtwidgets.NewQPushButtonWith(v, nil)
		defer btn.Delete()
		btn.SizeHint()
		if v.opt == nil {
			t.Error("InitStyleOption was not called")
			return
		}
		if v.opt.Alive() {
			t.Error("option still alive after the override returned; it is Qt's stack object")
		}
	})
}

// A stream keeps a raw pointer to its device: the device must survive a GC
// while only the stream refers to it.
func TestStreamKeepsDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ds *qtcore.QDataStream
	qute.Run(t, func() {
		f := qtcore.NewQFileWithName(path, nil)
		f.Open(qtcore.QIODeviceBaseReadOnly)
		ds = qtcore.NewQDataStreamWithIODevice(f)
	})
	qute.CollectGarbage()
	qute.Run(t, func() {
		if ds.AtEnd() {
			t.Error("AtEnd = true on an unread 11-byte file")
		}
		if ds.Device() == nil {
			t.Error("Device() = nil")
		}
		// replacing the device drops the old reference
		ds.SetDevice(nil)
		ds.Delete()
	})
}

// A model index keeps its model alive, and panics (not segfaults) once the
// model is deleted.
func TestIndexKeepsModel(t *testing.T) {
	var idx *qtcore.QModelIndex
	qute.Run(t, func() {
		m := qtcore.NewQStringListModelWithStrings([]string{"a"}, nil)
		idx = m.Index(0, 0)
	})
	qute.CollectGarbage()
	qute.Run(t, func() {
		if got := idx.Data().ToString(); got != "a" {
			t.Errorf("Data = %q, want \"a\"", got)
		}
		idx.Model().AsQObject().Delete()
		defer func() {
			p := recover()
			msg, _ := p.(string)
			if !strings.Contains(msg, "QModelIndex.Data") || !strings.Contains(msg, "QStringListModel") {
				t.Errorf("after deleting the model: panic %v, want a QModelIndex.Data message naming the model", p)
			}
		}()
		idx.Data()
	})
}

// A view keeps a model nobody else references (it used to empty silently).
func TestViewKeepsModel(t *testing.T) {
	var v *qtwidgets.QListView
	qute.Run(t, func() {
		v = qtwidgets.NewQListView(nil)
		v.SetModel(qtcore.NewQStringListModelWithStrings([]string{"a", "b"}, nil))
	})
	qute.CollectGarbage()
	qute.Run(t, func() {
		m := v.Model()
		if m == nil {
			t.Error("model was garbage collected while the view used it")
			return
		}
		if n := m.RowCount(); n != 2 {
			t.Errorf("RowCount = %d, want 2", n)
		}
		v.Delete()
	})
}

// Replacing a kept argument releases the old one, so it can be collected.
func TestKeptArgumentReplaced(t *testing.T) {
	var v *qtwidgets.QListView
	var old weak.Pointer[qtcore.QStringListModel]
	qute.Run(t, func() {
		v = qtwidgets.NewQListView(nil)
		m := qtcore.NewQStringListModelWithStrings([]string{"a"}, nil)
		old = weak.Make(m)
		v.SetModel(m)
		v.SetModel(nil)
	})
	qute.CollectGarbage()
	if old.Value() != nil {
		t.Error("the replaced model is still referenced; SetModel(nil) should drop the view's reference")
	}
	qute.Run(t, func() { v.Delete() })
}

// keep-each and release: a menu keeps a parentless action until it is removed.
func TestWidgetKeepsActions(t *testing.T) {
	qute.Run(t, func() {
		menu := qtwidgets.NewQMenu(nil)
		defer menu.Delete()
		a := qtgui.NewQActionWithText("one", nil)
		defer a.Delete()
		menu.AddAction(a)
		if !rt.KeepsArg(unsafe.Pointer(menu), unsafe.Pointer(a)) {
			t.Error("AddAction: action not kept")
		}
		menu.RemoveAction(a)
		if rt.KeepsArg(unsafe.Pointer(menu), unsafe.Pointer(a)) {
			t.Error("RemoveAction: action still kept")
		}
	})
}

// A widget's kept references go when the widget dies, even though its Go
// handle may be long gone (references are stored by object address).
func TestKeptReferencesDropWithOwner(t *testing.T) {
	qute.Run(t, func() {
		root := qtwidgets.NewQWidget(nil)
		view := qtwidgets.NewQListView(root)
		m := qtcore.NewQStringListModelWithStrings([]string{"a"}, nil)
		defer m.Delete()
		view.SetModel(m)
		if !rt.KeepsArg(unsafe.Pointer(view), unsafe.Pointer(m)) {
			t.Error("model not kept")
		}
		root.Delete()
		if rt.KeepsArg(unsafe.Pointer(view), unsafe.Pointer(m)) {
			t.Error("model still kept after the view died")
		}
	})
}

// fakeTB records the failure qute.Run reports instead of failing the test.
type fakeTB struct {
	testing.TB
	failed string
}

func (f *fakeTB) Helper()                      {}
func (f *fakeTB) Fatal(args ...any)            { f.failed = fmt.Sprint(args...) }
func (f *fakeTB) Fatalf(s string, args ...any) { f.failed = fmt.Sprintf(s, args...) }

// A panic in a signal handler is recovered where Qt called it, so Qt's frames
// return normally; Click re-raises it and Run reports it.
func TestHandlerPanicIsReported(t *testing.T) {
	ft := &fakeTB{TB: t}
	reached := false
	qute.Run(ft, func() {
		btn := qtwidgets.NewQPushButtonWithText("boom", nil)
		defer btn.Delete()
		btn.OnClicked(func(bool) { panic("boom from a handler") })
		qute.Click(btn)
		reached = true
	})
	if !strings.Contains(ft.failed, "boom from a handler") || !strings.Contains(ft.failed, "panic in a Qt callback") {
		t.Errorf("Run reported %q, want the handler's panic", ft.failed)
	}
	if reached {
		t.Error("code after Click ran; Click should re-raise the handler's panic")
	}
	// Qt is still usable afterwards.
	qute.Run(t, func() {
		clicks := 0
		btn := qtwidgets.NewQPushButtonWithText("ok", nil)
		defer btn.Delete()
		btn.OnClicked(func(bool) { clicks++ })
		qute.Click(btn)
		if clicks != 1 {
			t.Errorf("clicks = %d after a recovered panic, want 1", clicks)
		}
	})
}
