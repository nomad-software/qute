package rt

import (
	"fmt"
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
// Registered objects (QObjects and notify-dtor classes) can lose their Go
// handle while the C++ object lives on (a Parented widget nobody references),
// so their references are stored by object address and dropped by Destroyed.
// Other objects keep them on the handle, which lives as long as the object
// does for Owned values.

type keepSet struct {
	serial uint64
	slots  map[string]any
}

var (
	keepMu sync.Mutex
	kept   = map[unsafe.Pointer]*keepSet{}
)

func (h *Handle) registeredKind() bool { return h.kind == KindQObject || h.kind == KindNotify }

// slotsFor returns the keep slots of owner, creating them if create is set.
// The caller holds keepMu.
func slotsFor(owner *Handle, create bool) map[string]any {
	if owner.registeredKind() {
		ks := kept[owner.ptr]
		if ks != nil && ks.serial != owner.serial {
			// a stale set from an object that died at this address without a
			// notice reaching us; never let it leak into the new object
			ks = nil
			delete(kept, owner.ptr)
		}
		if ks == nil && create {
			ks = &keepSet{serial: owner.serial, slots: map[string]any{}}
			kept[owner.ptr] = ks
		}
		if ks == nil {
			return nil
		}
		return ks.slots
	}
	if owner.keeps == nil && create {
		owner.keeps = map[string]any{}
	}
	return owner.keeps
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
		if s := slotsFor(owner, false); s != nil {
			delete(s, slot)
		}
		return
	}
	slotsFor(owner, true)[slot] = arg
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
	if s := slotsFor(owner, false); s != nil {
		delete(s, eachSlot(arg))
	}
}

func eachSlot(arg *Handle) string { return fmt.Sprintf("each:%p", arg) }

// Keeps reports whether owner holds a reference to arg (for tests).
func Keeps(owner, arg *Handle) bool {
	if owner == nil || arg == nil {
		return false
	}
	keepMu.Lock()
	defer keepMu.Unlock()
	for _, v := range slotsFor(owner, false) {
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

// dropKeeps forgets the references held by the object at key.
func dropKeeps(key unsafe.Pointer) {
	keepMu.Lock()
	delete(kept, key)
	keepMu.Unlock()
}

// dropHandleKeeps forgets the references a handle held itself.
func (h *Handle) dropHandleKeeps() {
	keepMu.Lock()
	h.keeps = nil
	if h.registeredKind() {
		if ks := kept[h.ptr]; ks != nil && ks.serial == h.serial {
			delete(kept, h.ptr)
		}
	}
	keepMu.Unlock()
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
