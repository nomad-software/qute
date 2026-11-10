package rt

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Qt often keeps a pointer to an object it does not own: a view's model, a
// stream's device, a button's menu. Go code that drops its last reference to
// such an object would let the GC free it underneath Qt, so the overlay rules
// `keep`, `keep-each` and `release` make the receiver hold a strong reference
// instead (PySide's reference-keeping). A kept reference lasts until the
// receiver dies, the slot is replaced, or a release rule drops it.
//
// The references live on the owner's handle. A registered owner (a QObject
// or notify-dtor object) can outlive every Go variable that refers to it, so
// while it keeps anything its handle is anchored (see anchor.go): held by the
// root of its object tree, or globally. The references then last exactly as
// long as the object, and a window whose view keeps a model that refers back
// to the window can still be collected as a whole.

var keepMu sync.Mutex

// A kept object and its keeper often become unreachable in the same GC cycle
// (a window whose page keeps a QWebEngineProfile), and Go runs their cleanups
// in no particular order. Qt may need the kept object while the keeper is
// destroyed (a page must not outlive its profile), so the cleanup queue
// frees a kept object only once no live keeper holds it (see Drain). Both
// maps are keyed by handle serial, so they outlive the handles: a keeper's
// handle may be collected long before Qt destroys the object.
var (
	keptBy = map[uint64]int32{}    // kept handle serial -> keepers holding it
	holds  = map[uint64][]uint64{} // keeper serial -> serials it keeps, one per slot
)

// holdLocked records that keeper holds kept. The caller holds keepMu.
func holdLocked(keeper, kept uint64) {
	holds[keeper] = append(holds[keeper], kept)
	keptBy[kept]++
}

// unholdLocked drops one hold of keeper on kept and reports whether kept is
// now free to go. The caller holds keepMu.
func unholdLocked(keeper, kept uint64) bool {
	hs := holds[keeper]
	for i, s := range hs {
		if s == kept {
			hs[i] = hs[len(hs)-1]
			hs = hs[:len(hs)-1]
			break
		}
	}
	if len(hs) == 0 {
		delete(holds, keeper)
	} else {
		holds[keeper] = hs
	}
	if keptBy[kept]--; keptBy[kept] <= 0 {
		delete(keptBy, kept)
		return true
	}
	return false
}

// releaseHolds drops every hold of a keeper that died or whose handle was
// collected, and wakes the cleanup queue if a deferred free may now run.
func releaseHolds(keeper uint64) {
	keepMu.Lock()
	freed := false
	for _, s := range holds[keeper] {
		if keptBy[s]--; keptBy[s] <= 0 {
			delete(keptBy, s)
			freed = true
		}
	}
	delete(holds, keeper)
	keepMu.Unlock()
	if freed {
		wakeDeferred()
	}
}

// isKept reports whether a live keeper still holds the object with serial.
func isKept(serial uint64) bool {
	keepMu.Lock()
	defer keepMu.Unlock()
	return keptBy[serial] > 0
}

func (h *Handle) registeredKind() bool { return h.kind == KindQObject || h.kind == KindNotify }

// slotsFor returns the keep slots of owner, creating them if create is set.
// The caller holds keepMu.
func slotsFor(owner *Handle, create bool) map[string]any {
	if owner.keeps == nil && create {
		owner.keeps = map[string]any{}
		if owner.registeredKind() {
			owner.anchorPin()
		} else if !owner.hasCleanupNow() {
			// Go never frees this owner, so nothing would report its end:
			// release what it holds once its handle is collected.
			runtime.AddCleanup(owner, releaseHolds, owner.serial)
		}
	}
	return owner.keeps
}

// unslotLocked drops one slot and the owner's anchor once nothing is kept.
// The caller holds keepMu.
func unslotLocked(owner *Handle, slot string) {
	if owner.keeps == nil {
		return
	}
	if old, ok := owner.keeps[slot].(*Handle); ok && unholdLocked(owner.serial, old.serial) {
		defer wakeDeferred()
	}
	delete(owner.keeps, slot)
	if len(owner.keeps) == 0 {
		owner.keeps = nil
		if owner.registeredKind() {
			owner.anchorUnpin()
		}
	}
}

// KeepArg makes owner hold arg (a handle, or nil to clear) under slot,
// replacing whatever the slot held. Dead owners keep nothing.
func KeepArg(owner *Handle, slot string, arg *Handle) {
	if owner == nil || !owner.Alive() {
		return
	}
	keepMu.Lock()
	defer keepMu.Unlock()
	if arg == nil {
		unslotLocked(owner, slot)
		return
	}
	ks := slotsFor(owner, true)
	if old, ok := ks[slot].(*Handle); ok {
		if old == arg {
			return
		}
		if unholdLocked(owner.serial, old.serial) {
			defer wakeDeferred()
		}
	}
	ks[slot] = arg
	holdLocked(owner.serial, arg.serial)
}

// KeepEach makes owner hold arg until owner dies or Release drops it. Unlike
// KeepArg, each distinct argument gets its own entry (actions added to a widget).
func KeepEach(owner *Handle, arg *Handle) {
	if arg == nil {
		return
	}
	KeepArg(owner, eachSlot(arg), arg)
}

// Release drops a reference KeepEach took.
func Release(owner *Handle, arg *Handle) {
	if owner == nil || arg == nil {
		return
	}
	keepMu.Lock()
	defer keepMu.Unlock()
	unslotLocked(owner, eachSlot(arg))
}

func eachSlot(arg *Handle) string { return fmt.Sprintf("each:%p", arg) }

// Keeps reports whether owner holds a reference to arg (for tests).
func Keeps(owner, arg *Handle) bool {
	if owner == nil || arg == nil {
		return false
	}
	keepMu.Lock()
	defer keepMu.Unlock()
	for _, v := range owner.keeps {
		if v == arg {
			return true
		}
	}
	return false
}

// KeepsArg is Keeps for two generated wrappers (for generated tests).
func KeepsArg(owner, arg unsafe.Pointer) bool {
	if owner == nil || arg == nil {
		return false
	}
	return Keeps(RefOf(owner).H, RefOf(arg).H)
}

// KeepEachArg is KeepEach for two generated wrappers (for generated tests).
func KeepEachArg(owner, arg unsafe.Pointer) {
	if owner != nil && arg != nil {
		KeepEach(RefOf(owner).H, RefOf(arg).H)
	}
}

// dropHandleKeeps forgets the references a dead handle held.
func (h *Handle) dropHandleKeeps() {
	keepMu.Lock()
	had := h.keeps != nil
	h.keeps = nil
	keepMu.Unlock()
	if had && h.registeredKind() {
		h.anchorUnpin()
	}
	releaseHolds(h.serial)
}

// A value that only means something while another object lives (a
// QModelIndex and its model) depends on that object. Use panics once the
// dependency is dead instead of letting Qt follow a dangling pointer, and the
// strong reference keeps the GC from freeing it first.

type depFinder func(p unsafe.Pointer) *Handle

var (
	depMu sync.Mutex
	deps  sync.Map // class name -> []depFinder (copy on write under depMu)
)

// RegisterDependency says that every wrapped object of class depends on the
// object find returns for it (nil for none). A class may register several
// (a QTextTableCell depends on its table and on its document).
func RegisterDependency(class string, find func(p unsafe.Pointer) *Handle) {
	depMu.Lock()
	defer depMu.Unlock()
	var fs []depFinder
	if v, ok := deps.Load(class); ok {
		fs = v.([]depFinder)
	}
	deps.Store(class, append(fs[:len(fs):len(fs)], find))
}

func (h *Handle) attachDependency() {
	v, ok := deps.Load(h.class)
	if !ok {
		return
	}
	for _, find := range v.([]depFinder) {
		if d := find(h.ptr); d != nil && d != h {
			h.deps = append(h.deps, d)
		}
	}
}

// Dependencies returns the objects h depends on (for tests).
func (h *Handle) Dependencies() []*Handle {
	if h == nil {
		return nil
	}
	return h.deps
}

// checkDependency panics when an object h depends on has been destroyed.
func checkDependency(h *Handle, method string) {
	for _, d := range h.deps {
		if State(d.state.Load()) == Dead {
			panic(depMessage(h, d, method))
		}
	}
}

func depMessage(h, dep *Handle, method string) string {
	return fmt.Sprintf("%s: the %s this %s belongs to was destroyed (%s); "+
		"a %s is only valid while its owner lives, so keep the owner alive or fetch a fresh one",
		method, dep.class, h.class, deathNote(dep), h.class)
}

func deathNote(h *Handle) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.deathNote == "" {
		return "destroyed"
	}
	return h.deathNote
}
