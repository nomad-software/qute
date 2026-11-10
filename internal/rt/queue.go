package rt

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"sync/atomic"
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
	push(&qnode{arg: arg})
	scheduleDrain()
}

func push(n *qnode) {
	for {
		old := queueHead.Load()
		n.next = old
		if queueHead.CompareAndSwap(old, n) {
			return
		}
	}
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

// deferred holds frees of objects a live keeper still holds (see keep.go);
// only the GUI thread touches it. deferredN mirrors its length for wakeDeferred.
var (
	deferred  []cleanupArg
	deferredN atomic.Int32
)

// Drain frees every queued object. It must run on the GUI thread. An object
// another object keeps waits until its keepers are gone: freeing a keeper
// in this same drain releases it, and a keeper Qt destroys later wakes the
// queue again (wakeDeferred).
func Drain() {
	pending := deferred
	deferred = nil
	for n := queueHead.Swap(nil); n != nil; n = n.next {
		pending = append(pending, n.arg)
	}
	for progress := true; progress && len(pending) > 0; {
		progress = false
		rest := pending[:0]
		for _, a := range pending {
			if appExited.Load() {
				deferredN.Store(0)
				return // deleting after the app is gone crashes inside Qt destructors
			}
			if a.serial != 0 && isKept(a.serial) {
				rest = append(rest, a)
				continue
			}
			freeNode(a)
			progress = true
		}
		pending = rest
	}
	deferred = pending
	deferredN.Store(int32(len(pending)))
}

// wakeDeferred schedules a drain when a free was waiting for a keeper.
func wakeDeferred() {
	if deferredN.Load() > 0 {
		scheduleDrain()
	}
}

func freeNode(a cleanupArg) {
	if a.registered {
		regMu.Lock()
		e := registry[a.key]
		ok := e != nil && e.serial == a.serial
		regMu.Unlock()
		if !ok {
			return // already destroyed, or the address was reused
		}
		if a.kind == KindQObject && HasParent(a.key) {
			// Safety net: an ownership rule was missed and Qt now owns the
			// object. Never delete something with a parent.
			return
		}
	}
	a.deleter(a.key)
	if a.mem != 0 {
		memLive.Add(-a.mem)
	}
	if a.registered {
		Destroyed(a.key, "freed by GC")
	} else if a.serial != 0 {
		releaseHolds(a.serial) // what a plain object kept may go now
	}
	if !a.quiet {
		Drained.Add(1)
	}
}

// AppStarted is called once the Q*Application exists and the GUI thread can
// receive posted events.
func AppStarted() {
	if AppExists() {
		installFilterC()
	}
	pendingMu.Lock()
	appExited.Store(false)
	appRunning.Store(true)
	p := pendingPosts
	pendingPosts = nil
	// posted under the lock, so work queued before the start runs first and
	// none can slip in between the check in post and this hand-over
	for _, f := range p {
		postNow(f)
	}
	pendingMu.Unlock()
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
	// Work posted but never run (the loop exited first) would leave its
	// PostSync caller waiting forever; wake every waiter instead.
	syncMu.Lock()
	ws := syncWaiters
	syncWaiters = map[*syncCall]struct{}{}
	syncMu.Unlock()
	for w := range ws {
		w.finish(syncResult{notRun: true})
	}
}

// Running reports whether the application event loop can take posted work.
func Running() bool { return appRunning.Load() && !appExited.Load() }

// Post queues f to run on the GUI thread.
func Post(f func()) { post(f) }

// post queues f and reports whether it was accepted. Work queued before the
// application starts counts as accepted; work after it exits does not.
func post(f func()) bool {
	if !appRunning.Load() {
		if testHookPost != nil {
			testHookPost()
		}
		pendingMu.Lock()
		if !appRunning.Load() {
			// checked again under the lock AppStarted hands the list over in,
			// so f either joins the list before the hand-over or sees the app
			// running
			defer pendingMu.Unlock()
			if appExited.Load() {
				return false
			}
			pendingPosts = append(pendingPosts, f)
			return true
		}
		pendingMu.Unlock()
	}
	return postNow(f)
}

// testHookPost runs between post's first look at appRunning and the lock
// (tests only), where AppStarted used to slip in and strand the work.
var testHookPost func()

func postNow(f func()) bool {
	h := cgo.NewHandle(f)
	if !postC(uintptr(h)) {
		h.Delete()
		return false
	}
	return true
}

type syncResult struct {
	panicked bool
	val      any
	notRun   bool
	goexit   bool
}

type syncCall struct {
	once sync.Once
	done chan syncResult
}

func (s *syncCall) finish(r syncResult) { s.once.Do(func() { s.done <- r }) }

var (
	syncMu      sync.Mutex
	syncWaiters = map[*syncCall]struct{}{}
)

// errNotRun is the panic PostSync raises when f never ran.
const errNotRun = "qute.PostSync: the application exited before the function could run on the GUI thread"

// ErrGoexit is the panic PostSync raises when f called runtime.Goexit.
const ErrGoexit = "qute.PostSync: the function called runtime.Goexit (t.Fatal, t.FailNow or t.Skip) on the GUI thread; " +
	"inside qute.Run that ends the test, elsewhere return from the function instead"

// AfterRescue, when set, runs on the GUI thread once the event loop that
// replaced one ended by runtime.Goexit has quit (qute.TestMain exits there).
var AfterRescue func()

// PostSync runs f on the GUI thread and waits for it. Called on the GUI
// thread it runs f directly. A panic in f is re-raised in the caller, and so
// is a panic in a signal or virtual handler that f triggered: it is recovered
// where Qt called the handler, so Qt's C++ frames return normally, and
// re-raised with the handler's own stack. If the application exits (or the
// post fails) before f runs, PostSync panics instead of waiting forever.
func PostSync(f func()) {
	switch r := postSync(f); {
	case r.goexit:
		panic(ErrGoexit)
	case r.notRun:
		panic(errNotRun)
	case r.panicked:
		panic(r.val)
	}
}

// PostSyncExit is PostSync that reports a runtime.Goexit in f (a t.Fatal
// inside qute.Run) instead of panicking, so the caller can end its own
// goroutine the same way.
func PostSyncExit(f func()) (goexit bool) {
	switch r := postSync(f); {
	case r.goexit:
		return true
	case r.notRun:
		panic(errNotRun)
	case r.panicked:
		panic(r.val)
	}
	return false
}

func postSync(f func()) syncResult {
	if IsGUIThread() {
		stop := CapturePanics()
		returned := false
		defer func() {
			// a panic of f itself keeps unwinding; a handler's is raised here
			if cp := stop(); returned && cp != nil {
				panic(cp)
			}
		}()
		f()
		returned = true
		return syncResult{}
	}
	if !Running() {
		panic("qute.PostSync: the application is not running; call it from inside qute.Main or a qute.TestMain test")
	}
	s := &syncCall{done: make(chan syncResult, 1)}
	syncMu.Lock()
	syncWaiters[s] = struct{}{}
	syncMu.Unlock()
	ok := post(func() {
		syncMu.Lock()
		delete(syncWaiters, s)
		syncMu.Unlock()
		r := syncResult{panicked: true}
		stop := CapturePanics()
		defer func() {
			cp := stop()
			if r.panicked {
				r.val = recover()
				if r.val == nil {
					// recover returns nil only during runtime.Goexit: f
					// called t.Fatal, which would end the GUI goroutine and
					// with it every later test
					r.panicked, r.goexit = false, true
				}
			} else if cp != nil {
				r.panicked, r.val = true, cp
			}
			s.finish(r)
			if r.goexit {
				rescueGUIThread()
			}
		}()
		f()
		r.panicked = false
	})
	if !ok {
		syncMu.Lock()
		delete(syncWaiters, s)
		syncMu.Unlock()
		s.finish(syncResult{notRun: true})
	}
	return <-s.done
}

// rescueGUIThread runs while runtime.Goexit unwinds the GUI goroutine, which
// it cannot stop. It keeps that goroutine serving Qt from a nested event loop
// until the application quits, so later work (the next test) still runs.
func rescueGUIThread() {
	nestedLoopC()
	if AfterRescue != nil {
		AfterRescue()
	}
}

func checkThread(method string) {
	if !IsGUIThread() {
		panic(fmt.Sprintf("%s: called off the GUI thread; Qt objects may only be used on the GUI thread, "+
			"wrap the call in qute.Post(func() { ... })", method))
	}
}
