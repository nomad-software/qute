package rt

import (
	"strings"
	"testing"
	"unsafe"
)

// These tests cover the runtime logic that needs no QApplication: handle
// states, kept references, dependencies, the cleanup queue and PostSync's
// failure paths. Objects are fake addresses; nothing here dereferences them.

func fakePtr(n int) unsafe.Pointer {
	// distinct, never dereferenced; backed by real memory so the GC is happy
	b := make([]byte, 8)
	return unsafe.Pointer(&b[n%8])
}

func newPlain(class string) *Handle { return Wrap(fakePtr(0), class, KindPlain, KeepOrBorrow, nil) }

func TestStates(t *testing.T) {
	h := Wrap(fakePtr(0), "X", KindValue, TakeOwned, func(unsafe.Pointer) {})
	if h.State() != Owned {
		t.Fatalf("state = %v, want Owned", h.State())
	}
	h.TransferToCpp()
	if h.State() != Parented {
		t.Fatalf("after TransferToCpp: %v, want Parented", h.State())
	}
	h.TransferToGo()
	if h.State() != Owned {
		t.Fatalf("after TransferToGo: %v, want Owned", h.State())
	}
	h.markDead("test")
	h.TransferToCpp()
	if h.State() != Dead {
		t.Fatalf("a Dead handle changed state: %v", h.State())
	}
	if p := newPlain("Y"); p.State() != Borrowed {
		t.Fatalf("plain handle without deleter = %v, want Borrowed", p.State())
	}
}

func TestDeleteBorrowedPanics(t *testing.T) {
	h := newPlain("X")
	defer func() {
		if p := recover(); p == nil || !strings.Contains(p.(string), "rule 4") {
			t.Errorf("Delete on Borrowed: panic %v, want rule 4", p)
		}
	}()
	h.Delete("X.Delete")
}

func TestKeepArgSlots(t *testing.T) {
	owner, a, b := newPlain("Owner"), newPlain("A"), newPlain("B")
	KeepArg(owner, "setModel", a)
	if !Keeps(owner, a) {
		t.Fatal("a not kept")
	}
	KeepArg(owner, "setModel", b) // replaces a
	if Keeps(owner, a) || !Keeps(owner, b) {
		t.Fatalf("after replacing: keeps a=%v b=%v, want false true", Keeps(owner, a), Keeps(owner, b))
	}
	KeepArg(owner, "other", a) // independent slot
	KeepArg(owner, "setModel", nil)
	if Keeps(owner, b) || !Keeps(owner, a) {
		t.Fatalf("after clearing setModel: keeps a=%v b=%v, want true false", Keeps(owner, a), Keeps(owner, b))
	}
}

func TestKeepEachAndRelease(t *testing.T) {
	owner, a, b := newPlain("Owner"), newPlain("A"), newPlain("B")
	KeepEach(owner, a)
	KeepEach(owner, b)
	KeepEach(owner, a) // twice is one entry
	if !Keeps(owner, a) || !Keeps(owner, b) {
		t.Fatal("KeepEach lost an argument")
	}
	Release(owner, a)
	if Keeps(owner, a) || !Keeps(owner, b) {
		t.Fatalf("after Release(a): keeps a=%v b=%v, want false true", Keeps(owner, a), Keeps(owner, b))
	}
	Release(owner, a) // idempotent
	Release(nil, b)
	KeepEach(owner, nil)
}

func TestKeepsDropWhenPlainOwnerDies(t *testing.T) {
	owner, a := newPlain("Owner"), newPlain("A")
	KeepArg(owner, "s", a)
	owner.markDead("test")
	if Keeps(owner, a) {
		t.Fatal("a dead owner still keeps its argument")
	}
	KeepArg(owner, "s", a)
	if Keeps(owner, a) {
		t.Fatal("a dead owner took a new reference")
	}
}

// Registered owners store references by address, so they survive the loss of
// the Go handle and go away with the object's death notice.
func TestKeepsOfRegisteredOwnerDropOnDestroyed(t *testing.T) {
	key := fakePtr(1)
	owner := Wrap(key, "Owner", KindNotify, KeepOrBorrow, nil)
	a := newPlain("A")
	KeepArg(owner, "s", a)
	// a new handle for the same live object sees the same references
	keepMu.Lock()
	_, ok := kept[key]
	keepMu.Unlock()
	if !ok {
		t.Fatal("registered owner's references are not stored by address")
	}
	Destroyed(key, "test")
	keepMu.Lock()
	_, ok = kept[key]
	keepMu.Unlock()
	if ok {
		t.Fatal("references survived the owner's death notice")
	}
	if owner.State() != Dead {
		t.Fatalf("owner = %v, want Dead", owner.State())
	}
}

// A stale reference set at a reused address must not leak into a new object.
func TestKeepsIgnoreStaleSerial(t *testing.T) {
	key := fakePtr(2)
	keepMu.Lock()
	kept[key] = &keepSet{serial: 0, slots: map[string]any{"s": newPlain("Old")}}
	keepMu.Unlock()
	owner := Wrap(key, "Owner", KindNotify, KeepOrBorrow, nil)
	defer Destroyed(key, "test")
	keepMu.Lock()
	s := slotsFor(owner, false)
	keepMu.Unlock()
	if len(s) != 0 {
		t.Fatalf("new object at a reused address sees %d stale references", len(s))
	}
}

type fakeWrapper struct{ r Ref }

func TestKeepsArgWrappers(t *testing.T) {
	owner, a := newPlain("Owner"), newPlain("A")
	ow, aw := &fakeWrapper{Ref{H: owner}}, &fakeWrapper{Ref{H: a}}
	KeepEachArg(unsafe.Pointer(ow), unsafe.Pointer(aw))
	if !KeepsArg(unsafe.Pointer(ow), unsafe.Pointer(aw)) {
		t.Fatal("KeepsArg = false after KeepEachArg")
	}
	if KeepsArg(nil, unsafe.Pointer(aw)) || KeepsArg(unsafe.Pointer(ow), nil) {
		t.Fatal("KeepsArg with nil = true")
	}
}

// A value class with a dependency panics once the dependency is dead.
func TestDependency(t *testing.T) {
	model := newPlain("Model")
	RegisterDependency("Index", func(unsafe.Pointer) *Handle { return model })
	defer deps.Delete("Index")
	idx := Wrap(fakePtr(3), "Index", KindValue, TakeOwned, func(unsafe.Pointer) {})
	if d := idx.Dependencies(); len(d) != 1 || d[0] != model {
		t.Fatal("dependency not attached")
	}
	// checkDependency is the part of Use under test; Use itself also checks
	// the GUI thread in qute_debug builds, and tests run off it
	checkDependency(idx, "Index.Data") // fine while the model lives
	model.markDead("deleted by Delete()")
	defer func() {
		p := recover()
		msg, _ := p.(string)
		if !strings.Contains(msg, "Index.Data") || !strings.Contains(msg, "Model this Index belongs to was destroyed") ||
			!strings.Contains(msg, "deleted by Delete()") {
			t.Errorf("Use after the dependency died: panic %v", p)
		}
	}()
	checkDependency(idx, "Index.Data")
}

func TestNoDependencyForNil(t *testing.T) {
	RegisterDependency("Index2", func(unsafe.Pointer) *Handle { return nil })
	defer deps.Delete("Index2")
	if h := Wrap(fakePtr(4), "Index2", KindValue, TakeOwned, func(unsafe.Pointer) {}); h.Dependencies() != nil {
		t.Fatal("dependency attached for a nil result")
	}
}

// Several dependencies: the panic names whichever one died.
func TestSeveralDependencies(t *testing.T) {
	table, doc := newPlain("Table"), newPlain("Doc")
	RegisterDependency("Cell", func(unsafe.Pointer) *Handle { return table })
	RegisterDependency("Cell", func(unsafe.Pointer) *Handle { return doc })
	defer deps.Delete("Cell")
	cell := Wrap(fakePtr(5), "Cell", KindValue, TakeOwned, func(unsafe.Pointer) {})
	if d := cell.Dependencies(); len(d) != 2 || d[0] != table || d[1] != doc {
		t.Fatalf("dependencies = %v, want [table doc]", d)
	}
	checkDependency(cell, "Cell.Row")
	doc.markDead("deleted by Delete()")
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "Doc this Cell belongs to was destroyed") {
			t.Errorf("Use after the document died: panic %q", msg)
		}
	}()
	checkDependency(cell, "Cell.Row")
}

// Connections are freed on the GUI thread through the cleanup queue, never
// from the GC cleanup goroutine, and are not counted as freed objects.
func TestConnectionFreedThroughQueue(t *testing.T) {
	queueHead.Store(nil)
	defer queueHead.Store(nil)
	p := fakePtr(5)
	freeConnectionLater(p)
	n := queueHead.Load()
	if n == nil {
		t.Fatal("connection free was not queued")
	}
	if n.arg.key != p || !n.arg.quiet || n.arg.registered {
		t.Fatalf("queued %+v, want a quiet unregistered free of the connection", n.arg)
	}
}

func TestDrainSkipsQuietCount(t *testing.T) {
	queueHead.Store(nil)
	freed := 0
	before := Drained.Load()
	enqueueCleanup(cleanupArg{key: fakePtr(6), kind: KindPlain, deleter: func(unsafe.Pointer) { freed++ }, quiet: true})
	enqueueCleanup(cleanupArg{key: fakePtr(6), kind: KindPlain, deleter: func(unsafe.Pointer) { freed++ }})
	Drain()
	if freed != 2 {
		t.Fatalf("Drain freed %d, want 2", freed)
	}
	if got := Drained.Load() - before; got != 1 {
		t.Fatalf("Drained counted %d, want 1 (quiet frees are not objects)", got)
	}
}

// PostSync must not wait forever when its function can never run.
func TestPostSyncPostFails(t *testing.T) {
	// No QApplication exists in this test binary, so the post itself fails.
	AppStarted()
	defer func() {
		appRunning.Store(false)
		appExited.Store(false)
	}()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		PostSync(func() { t.Error("f ran without an application") })
	}()
	if p := <-done; p != errNotRun {
		t.Fatalf("PostSync panic = %v, want %q", p, errNotRun)
	}
}

func TestAppExitedWakesPostSyncWaiters(t *testing.T) {
	s := &syncCall{done: make(chan syncResult, 1)}
	syncMu.Lock()
	syncWaiters[s] = struct{}{}
	syncMu.Unlock()
	AppExited()
	defer appExited.Store(false)
	r := <-s.done
	if !r.notRun {
		t.Fatalf("waiter got %+v, want notRun", r)
	}
	if post(func() {}) {
		t.Fatal("post after exit reported success")
	}
}

func TestCallbackPanicCapture(t *testing.T) {
	stop := CapturePanics()
	func() {
		defer catchPanic()
		panic("handler failed")
	}()
	cp := stop()
	if cp == nil || cp.Value != "handler failed" || !strings.Contains(cp.Stack, "TestCallbackPanicCapture") {
		t.Fatalf("captured %+v, want the handler's panic with its stack", cp)
	}
	// without a capture the panic propagates as before
	defer func() {
		if p := recover(); p != "uncaptured" {
			t.Fatalf("panic = %v, want it to propagate", p)
		}
	}()
	func() {
		defer catchPanic()
		panic("uncaptured")
	}()
}

func TestRethrowCallbackPanic(t *testing.T) {
	stop := CapturePanics()
	defer stop()
	func() {
		defer catchPanic()
		panic("first")
	}()
	func() {
		defer catchPanic()
		panic("second") // only the first is kept
	}()
	defer func() {
		cp, _ := recover().(*CallbackPanic)
		if cp == nil || cp.Value != "first" {
			t.Fatalf("rethrown %+v, want the first panic", cp)
		}
	}()
	RethrowCallbackPanic()
}
