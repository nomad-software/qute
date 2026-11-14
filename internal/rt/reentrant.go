package rt

import (
	"fmt"
	"sync"
	"unsafe"
)

// Reentrant classes (class option `reentrant` in the spec, for classes Qt
// documents as reentrant: QRegularExpression) may be used on any goroutine,
// as long as no two goroutines use the same object at once. Their generated
// C functions skip the GUI-thread check, and so does Use in qute_debug
// builds. A GC cleanup still frees them on the GUI thread once the event
// loop runs, which is fine: by then nothing else uses the object. Before it
// runs (or in a program that never starts one) the cleanup frees them where
// it runs, since no drain would (enqueueCleanup).
//
// Qt promises reentrancy, not thread safety, so qute_debug builds also catch
// two goroutines inside methods of the same object at once: a generated
// method of a reentrant class brackets its C call with Enter and Leave, and
// Delete() panics on an object another goroutine is using.

// RegisterReentrant marks class as usable on any goroutine.
func RegisterReentrant(class string) {
	updateClass(class, func(ci *classInfo) { ci.reentrant = true })
}

func isReentrant(class string) bool {
	// by name, not through classOf (see parentedDeleteHint)
	if v, ok := classes.Load(class); ok {
		return v.(classInfo).reentrant
	}
	return false
}

// busy maps a handle to the method a goroutine is running on it
// (qute_debug builds only).
var busy sync.Map

// Enter records that a method of a reentrant class is running on the
// wrapper's object, and panics if another goroutine is already inside one.
// It does nothing outside qute_debug builds.
func Enter(wrapper unsafe.Pointer, method string) {
	if !debugEnabled {
		return
	}
	h := (*Ref)(wrapper).H
	if other, loaded := busy.LoadOrStore(h, method); loaded {
		panic(fmt.Sprintf("%s: called while another goroutine is inside %s on the same %s; "+
			"Qt's reentrant classes may be used on any goroutine, but not by two at once: "+
			"give each goroutine its own copy", method, other, h.class))
	}
}

// Leave ends what Enter began.
func Leave(wrapper unsafe.Pointer) {
	if debugEnabled {
		busy.Delete((*Ref)(wrapper).H)
	}
}

func checkNotBusy(h *Handle, method string) {
	if other, ok := busy.Load(h); ok {
		panic(fmt.Sprintf("%s: another goroutine is inside %s on this %s, and deleting it would free it under that call; "+
			"wait for that goroutine, or give each goroutine its own copy", method, other, h.class))
	}
}
