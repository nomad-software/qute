package rt

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"
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

// A registered owner can outlive its Go variables, so while it keeps
// references its handle is anchored; its death notice drops both.
func TestKeepsOfRegisteredOwnerDropOnDestroyed(t *testing.T) {
	key := fakePtr(1)
	owner := Wrap(key, "Owner", KindNotify, KeepOrBorrow, nil)
	a := newPlain("A")
	KeepArg(owner, "s", a)
	if owner.Anchored() != "global" {
		t.Fatalf("owner with kept references anchored %q, want global", owner.Anchored())
	}
	Destroyed(key, "test")
	if Keeps(owner, a) {
		t.Fatal("references survived the owner's death notice")
	}
	if owner.Anchored() != "" || owner.Pins() != 0 {
		t.Fatalf("dead owner still anchored (%q, %d pins)", owner.Anchored(), owner.Pins())
	}
	if owner.State() != Dead {
		t.Fatalf("owner = %v, want Dead", owner.State())
	}
}

// Clearing the last kept reference drops the anchor it took.
func TestKeepsAnchorReleased(t *testing.T) {
	key := fakePtr(2)
	owner := Wrap(key, "Owner", KindNotify, KeepOrBorrow, nil)
	defer Destroyed(key, "test")
	a := newPlain("A")
	KeepEach(owner, a)
	if owner.Pins() != 1 {
		t.Fatalf("pins = %d, want 1 while keeping", owner.Pins())
	}
	Release(owner, a)
	if owner.Pins() != 0 || owner.Anchored() != "" {
		t.Fatalf("after Release: pins %d, anchored %q, want 0 and none", owner.Pins(), owner.Anchored())
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

// A post that saw the app not yet running, while AppStarted handed the
// pending list over, used to append to the list after the hand-over and
// never run. Now it sees the app running and posts directly.
func TestPostRacingAppStarted(t *testing.T) {
	defer func() {
		testHookPost = nil
		appRunning.Store(false)
		appExited.Store(false)
		pendingPosts = nil
	}()
	testHookPost = func() {
		testHookPost = nil
		AppStarted() // runs between post's check and its lock
	}
	post(func() {})
	pendingMu.Lock()
	n := len(pendingPosts)
	pendingMu.Unlock()
	if n != 0 {
		t.Fatalf("%d posts stranded in the pending list after the app started", n)
	}
}

// A plain object handed out by a method depends on the receiver and on what
// the receiver depends on; Alive and Use see that.
func TestWrapClassFromDependsOnReceiver(t *testing.T) {
	RegisterClass("PlainItem", KindPlain, nil)
	doc := newPlain("Doc")
	owner := newPlain("Layout")
	owner.deps = []*Handle{doc}
	it := WrapClassFrom(fakePtr(3), "PlainItem", KeepOrBorrow, owner)
	if len(it.deps) != 2 || it.deps[0] != owner || it.deps[1] != doc {
		t.Fatalf("deps = %v, want the receiver and its document", it.deps)
	}
	if !it.Alive() {
		t.Fatal("item not alive while its receiver lives")
	}
	doc.markDead("test")
	if it.Alive() {
		t.Fatal("item alive after what its receiver depends on died")
	}
	defer func() {
		if p, _ := recover().(string); !strings.Contains(p, "the Doc this PlainItem belongs to was destroyed") {
			t.Errorf("Use: panic %q, want the dependency message", p)
		}
	}()
	r := &fakeWrapper{Ref{H: it, P: fakePtr(3)}}
	Use(unsafe.Pointer(r), "PlainItem.Geometry")
}

// An owned plain result needs no dependency: Go frees it.
func TestWrapClassFromOwnedHasNoDependency(t *testing.T) {
	RegisterClass("OwnedItem", KindPlain, func(unsafe.Pointer) {})
	owner := newPlain("Layout")
	it := WrapClassFrom(fakePtr(4), "OwnedItem", TakeOwned, owner)
	if len(it.deps) != 0 {
		t.Fatalf("owned result depends on %v", it.deps)
	}
}

// fakeQObjects registers QObject handles without Qt: a fake tree for the
// anchoring logic.
func fakeQObject(t *testing.T, st State) *Handle {
	t.Helper()
	b := make([]byte, 8)
	h := newHandle(unsafe.Pointer(&b[0]), "QObject", KindQObject, st, nil)
	regMu.Lock()
	registry[h.ptr] = &regEntry{w: weak.Make(h), serial: h.serial}
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		delete(registry, h.ptr)
		regMu.Unlock()
		runtime.KeepAlive(b)
	})
	return h
}

func withFakeTree(t *testing.T, parent map[*Handle]*Handle) {
	t.Helper()
	oldRoot, oldSub := rootOfC, subtreeC
	t.Cleanup(func() { rootOfC, subtreeC = oldRoot, oldSub })
	byPtr := map[unsafe.Pointer]*Handle{}
	for c, p := range parent {
		byPtr[c.ptr] = c
		if p != nil {
			byPtr[p.ptr] = p
		}
	}
	rootOfC = func(p unsafe.Pointer) unsafe.Pointer {
		h := byPtr[p]
		for parent[h] != nil {
			h = parent[h]
		}
		return h.ptr
	}
	subtreeC = func(p unsafe.Pointer) []unsafe.Pointer {
		out := []unsafe.Pointer{p}
		for c, par := range parent {
			if par != nil && par.ptr == p {
				out = append(out, subtreeC(c.ptr)...)
			}
		}
		return out
	}
}

// Go state of an object in a tree is held by the tree's Owned root, so the
// whole tree can be collected; a root that stops being Owned hands it to the
// global set, and so does a move between parents.
func TestAnchoring(t *testing.T) {
	root := fakeQObject(t, Owned)
	child := fakeQObject(t, Parented)
	withFakeTree(t, map[*Handle]*Handle{root: nil, child: root})

	child.anchorPin()
	if got := child.Anchored(); got != "root" {
		t.Fatalf("child anchored %q, want root", got)
	}
	if child.Pins() != 0 {
		t.Fatalf("child anchored by its root is also pinned globally (%d)", child.Pins())
	}
	if _, ok := root.anchored[child]; !ok {
		t.Fatal("root does not hold the child")
	}

	root.anchorPin() // a root's own state is global: rule "pinned while connected"
	if got := root.Anchored(); got != "global" || root.Pins() != 1 {
		t.Fatalf("root anchored %q with %d pins, want global and 1", got, root.Pins())
	}
	root.anchorUnpin()

	reanchorSubtree(child.ptr, true) // between parents
	if got := child.Anchored(); got != "global" || child.Pins() != 1 {
		t.Fatalf("moving child anchored %q with %d pins, want global and 1", got, child.Pins())
	}
	reanchorSubtree(child.ptr, false) // arrived (back) under root
	if got := child.Anchored(); got != "root" || child.Pins() != 0 {
		t.Fatalf("re-added child anchored %q with %d pins, want root and 0", got, child.Pins())
	}

	root.TransferToCpp() // a Parented root may be collected: it holds nothing
	if got := child.Anchored(); got != "global" || child.Pins() != 1 {
		t.Fatalf("after root became Parented: child anchored %q with %d pins, want global", got, child.Pins())
	}
	child.anchorUnpin()
	if child.Anchored() != "" || child.Pins() != 0 {
		t.Fatalf("released child anchored %q with %d pins", child.Anchored(), child.Pins())
	}
}

// A connection's closure lives on its sender's handle; the id table holds the
// handle weakly, and releasing the connection drops the closure and anchor.
func TestCallbackSlots(t *testing.T) {
	sender := Wrap(fakePtr(5), "Sender", KindNotify, KeepOrBorrow, nil)
	defer Destroyed(sender.ptr, "test")
	ran := 0
	id := NewCallback(sender, func(unsafe.Pointer) { ran++ })
	if sender.Pins() != 1 {
		t.Fatalf("connected sender pins = %d, want 1", sender.Pins())
	}
	InvokeCallback(id, nil)
	if ran != 1 {
		t.Fatalf("callback ran %d times, want 1", ran)
	}
	ReleaseCallback(id)
	InvokeCallback(id, nil) // released: nothing runs
	if ran != 1 || sender.Pins() != 0 || len(sender.callbacks) != 0 {
		t.Fatalf("after release: ran %d, pins %d, %d closures", ran, sender.Pins(), len(sender.callbacks))
	}
	// std::function ids never collide with slot ids
	fid := NewFunc(func(_, _ unsafe.Pointer) {})
	if fid&funcBit == 0 {
		t.Fatal("NewFunc id lacks the func bit")
	}
	ReleaseCallback(fid)
}
