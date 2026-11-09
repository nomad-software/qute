package rt

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Cleanups never call delete directly: GC cleanups run on a GC goroutine, and
// Qt objects must be freed on the GUI thread. They push onto this lock-free
// stack, which the GUI thread drains from a posted event.

type qnode struct {
	arg  cleanupArg
	next *qnode
}

var (
	queueHead      atomic.Pointer[qnode]
	drainScheduled atomic.Bool
	appRunning     atomic.Bool
	appExited      atomic.Bool

	pendingMu    sync.Mutex
	pendingPosts []func()

	// Drained counts objects freed by the cleanup queue (for tests).
	Drained atomic.Int64
)

func enqueueCleanup(arg cleanupArg) {
	if appExited.Load() {
		return
	}
	n := &qnode{arg: arg}
	for {
		old := queueHead.Load()
		n.next = old
		if queueHead.CompareAndSwap(old, n) {
			break
		}
	}
	scheduleDrain()
}

func scheduleDrain() {
	if !appRunning.Load() || appExited.Load() {
		return
	}
	if drainScheduled.CompareAndSwap(false, true) {
		if !postDrainC() {
			drainScheduled.Store(false)
		}
	}
}

// Drain frees every queued object. It must run on the GUI thread.
func Drain() {
	n := queueHead.Swap(nil)
	for ; n != nil; n = n.next {
		if appExited.Load() {
			return // deleting after the app is gone crashes inside Qt destructors
		}
		a := n.arg
		if a.registered {
			regMu.Lock()
			e := registry[a.key]
			ok := e != nil && e.serial == a.serial
			regMu.Unlock()
			if !ok {
				continue // already destroyed, or the address was reused
			}
			if a.kind == KindQObject && HasParent(a.key) {
				// Safety net: an ownership rule was missed and Qt now owns the
				// object. Never delete something with a parent.
				continue
			}
		}
		a.deleter(a.key)
		if a.registered {
			Destroyed(a.key, "freed by GC")
		}
		Drained.Add(1)
	}
}

// AppStarted is called once the Q*Application exists and the GUI thread can
// receive posted events.
func AppStarted() {
	appExited.Store(false)
	appRunning.Store(true)
	pendingMu.Lock()
	p := pendingPosts
	pendingPosts = nil
	pendingMu.Unlock()
	for _, f := range p {
		Post(f)
	}
	if queueHead.Load() != nil {
		scheduleDrain()
	}
}

// AppExited is called after the event loop returns. Queued cleanups are
// dropped, not run, from now on.
func AppExited() {
	appRunning.Store(false)
	appExited.Store(true)
	queueHead.Store(nil)
}

// Running reports whether the application event loop can take posted work.
func Running() bool { return appRunning.Load() && !appExited.Load() }

// Post queues f to run on the GUI thread.
func Post(f func()) {
	if !appRunning.Load() {
		if appExited.Load() {
			return
		}
		pendingMu.Lock()
		pendingPosts = append(pendingPosts, f)
		pendingMu.Unlock()
		return
	}
	h := cgo.NewHandle(f)
	if !postC(uintptr(h)) {
		h.Delete()
	}
}

// PostSync runs f on the GUI thread and waits for it. Called on the GUI
// thread it runs f directly. A panic in f is re-raised in the caller.
func PostSync(f func()) {
	if IsGUIThread() {
		f()
		return
	}
	if !Running() {
		panic("goqt.PostSync: the application is not running; call it from inside goqt.Main or a goqt.TestMain test")
	}
	done := make(chan any, 1)
	Post(func() {
		defer func() { done <- recover() }()
		f()
	})
	if p := <-done; p != nil {
		panic(p)
	}
}

func checkThread(method string) {
	if !IsGUIThread() {
		panic(fmt.Sprintf("%s: called off the GUI thread; Qt objects may only be used on the GUI thread, "+
			"wrap the call in goqt.Post(func() { ... })", method))
	}
}

var _ = unsafe.Pointer(nil)
