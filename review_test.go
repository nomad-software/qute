package qute_test

import (
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

// Tests for defects found in review: misuse that used to crash, and states
// that used to be wrong.

// panicText runs f and returns the text of its panic, or "".
func panicText(f func()) (msg string) {
	defer func() {
		if p := recover(); p != nil {
			msg, _ = p.(string)
			if msg == "" {
				msg = "non-string panic"
			}
		}
	}()
	f()
	return ""
}

// A painter and the large image it draws on, dropped together inside one
// handler, are freed by the memory-pressure path. It used to free the image
// first, and ~QPainter then ran on freed memory.
func TestMemoryPathFreesPainterBeforeImage(t *testing.T) {
	qutetest.Run(t, func() {
		for i := 0; i < 100; i++ {
			img := qtgui.NewQImageWithSize(qtcore.QSize{Width: 1024, Height: 1024}, qtgui.QImageFormat_ARGB32)
			p := qtgui.NewQPainterOn(img)
			p.FillRect(qtcore.QRect{Width: 10, Height: 10}, qtgui.RGB(1, 2, 3))
			time.Sleep(time.Millisecond) // lets GC cleanups queue the pair
		}
	})
	qutetest.CollectGarbage()
}

// Deleting the device a painter still draws on used to leave the painter
// with a dangling pointer. It panics now; after End it is allowed.
func TestDeleteDeviceUnderPainterPanics(t *testing.T) {
	qutetest.Run(t, func() {
		img := qtgui.NewQImageWithSize(qtcore.QSize{Width: 8, Height: 8}, qtgui.QImageFormat_ARGB32)
		p := qtgui.NewQPainterOn(img)
		msg := panicText(img.Delete)
		if !strings.Contains(msg, "QPainter still uses this QImage") {
			t.Errorf("Delete of a painted-on image: panic %q, want the QPainter message", msg)
		}
		p.FillRect(qtcore.QRect{Width: 4, Height: 4}, qtgui.RGB(9, 9, 9))
		p.End()
		if msg := panicText(img.Delete); msg != "" {
			t.Errorf("Delete after End: panic %q", msg)
		}
		p.Delete()
	})
}

// A stream does not notice its device being deleted either.
func TestDeleteStreamDevicePanics(t *testing.T) {
	qutetest.Run(t, func() {
		buf := qtcore.NewQBuffer(nil)
		buf.Open(qtcore.QIODeviceBaseWriteOnly)
		ds := qtcore.NewQDataStreamWithIODevice(buf)
		if msg := panicText(buf.Delete); !strings.Contains(msg, "QDataStream still uses this QBuffer") {
			t.Errorf("Delete of a stream's device: panic %q", msg)
		}
		ds.SetDevice(nil)
		buf.Delete()
		ds.Delete()
	})
}

// A view notices its model being deleted, so that stays allowed.
func TestDeleteViewModelAllowed(t *testing.T) {
	qutetest.Run(t, func() {
		view := qtwidgets.NewQListView(nil)
		m := qtcore.NewQStringListModelWithStrings([]string{"a"}, nil)
		view.SetModel(m)
		if msg := panicText(m.Delete); msg != "" {
			t.Errorf("Delete of a view's model: panic %q", msg)
		}
		view.Delete()
	})
}

// A layout keeps a pointer to a spacer item and does not notice its
// destructor; Delete of a spacer in a layout used to crash the next layout
// pass. It panics now and names TakeAt.
func TestDeleteSpacerInLayoutPanics(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		lay := qtwidgets.NewQVBoxLayout(w)
		sp := qtwidgets.NewQSpacerItemWithW(10, 10)
		lay.AddSpacerItem(sp)
		if msg := panicText(sp.Delete); !strings.Contains(msg, "TakeAt") {
			t.Errorf("Delete of a spacer in a layout: panic %q, want the TakeAt hint", msg)
		}
		if n := lay.Count(); n != 1 {
			t.Errorf("layout has %d items, want 1", n)
		}
		it := lay.TakeAt(0)
		if it.Ownership() != qtcore.Owned {
			t.Errorf("taken item: %v, want Owned", it.Ownership())
		}
		it.Delete()
		qutetest.Wait(1)
		w.Delete()
	})
}

// A sub-layout is a QObject: its destructor takes it out of its parent
// layout, so deleting it stays allowed.
func TestDeleteSubLayoutAllowed(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		lay := qtwidgets.NewQVBoxLayout(w)
		sub := qtwidgets.NewQHBoxLayout(nil)
		lay.AddLayout(sub)
		if msg := panicText(sub.Delete); msg != "" {
			t.Errorf("Delete of a sub-layout: panic %q", msg)
		}
		if n := lay.Count(); n != 0 {
			t.Errorf("layout has %d items after its sub-layout was deleted, want 0", n)
		}
		w.Delete()
	})
}

// Rule 5 holds in every build, not only with -tags qute_debug.
func TestDeleteInHandlerPanics(t *testing.T) {
	qutetest.Run(t, func() {
		btn := qtwidgets.NewQPushButtonWithText("x", nil)
		var msg string
		btn.OnClicked(func(bool) { msg = panicText(btn.Delete) })
		qutetest.Click(btn)
		if !strings.Contains(msg, "DeleteLater") {
			t.Errorf("want the DeleteLater hint, got %q", msg)
		}
		if !btn.Alive() {
			t.Error("the button was deleted inside its handler")
		}
		btn.Delete()
	})
}

// A transfer-to-go result that Qt has given a parent is Parented, not Owned.
func TestTransferToGoResultWithParentIsParented(t *testing.T) {
	qutetest.Run(t, func() {
		parent := qtcore.NewQObject(nil)
		child := qtcore.NewQObject(parent)
		h := rt.Wrap(child.Ptr(), "QObject", rt.KindQObject, rt.TakeOwned, rt.DeleteQObject)
		if h.State() != rt.Parented {
			t.Errorf("transfer-to-go of a child: %v, want Parented", h.State())
		}
		child.SetParent(nil)
		if child.Ownership() != qtcore.Owned {
			t.Errorf("after SetParent(nil): %v, want Owned", child.Ownership())
		}
		child.Delete()
		parent.Delete()
	})
}

// Qt calling a Go handler from another thread is a clear panic, not a
// corrupted runtime.
func TestHandlerOffGUIThreadPanics(t *testing.T) {
	var msg string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		msg = panicText(func() { rt.InvokeCallback(0, nil) })
	}()
	wg.Wait()
	if !strings.Contains(msg, "thread other than the GUI thread") {
		t.Errorf("off-thread handler entry: panic %q", msg)
	}
}

// A text cursor keeps its document from being collected, but Qt copes with
// the document being deleted, so that is allowed (kept-delete: allow).
func TestDeleteCursorDocumentAllowed(t *testing.T) {
	qutetest.Run(t, func() {
		doc := qtgui.NewQTextDocument(nil)
		c := qtgui.NewQTextCursorWithTextDocument(doc)
		if msg := panicText(doc.Delete); msg != "" {
			t.Errorf("Delete of a cursor's document: panic %q", msg)
		}
		if !c.IsNull() {
			t.Error("cursor not null after its document was deleted")
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

// Pin and Unpin from many goroutines leave the count and the strong set in
// step: a handle with no pins can be collected again.
func TestPinUnpinConcurrent(t *testing.T) {
	var obj *qtcore.QObject
	qutetest.Run(t, func() { obj = qtcore.NewQObject(nil) })
	h := rt.RefOf(unsafe.Pointer(obj)).H
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				h.Pin()
				h.Unpin()
			}
		}()
	}
	wg.Wait()
	if h.Pins() != 0 {
		t.Errorf("pins = %d after balanced Pin/Unpin, want 0", h.Pins())
	}
	qutetest.Run(t, func() { obj.Delete() })
}
