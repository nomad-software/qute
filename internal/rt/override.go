package rt

import (
	"fmt"
	"reflect"
	"runtime"
	"unsafe"
)

// Override makes f the object's implementation of the virtual called name
// (a generated OverrideX setter), or with f nil restores the C++ one. f is
// a func(super, args...) where super calls the C++ implementation. It works
// on objects qute's constructors built, which are all shim objects of a
// class Go can subclass (RegisterShim); an object Qt created is not, and
// panics with the alternative.
//
// The function is Go state of the object, held like a signal's closure: the
// handle is anchored (anchor.go) while it has a dispatch id, so a parentless
// object with an override must be deleted, as one with connections. When the
// last override goes and no Go subclass value remains, the id and the anchor
// go with it.
func Override(wrapper unsafe.Pointer, method, name string, f any) {
	if f != nil && reflect.ValueOf(f).Kind() == reflect.Func && reflect.ValueOf(f).IsNil() {
		f = nil // a nil func of the setter's type
	}
	Use(wrapper, method)
	GUIOnly(method)
	h := (*Ref)(wrapper).H
	ci, _ := classes.Load(h.class) // by name: see parentedDeleteHint
	info, _ := ci.(classInfo)
	if !h.shim || info.shimInit == nil {
		panic(fmt.Sprintf("%s: this %s was not made by a qute constructor (Qt created it, or its class cannot be "+
			"subclassed: see COVERAGE.md), so Go cannot override its virtuals; for an event handler, "+
			"install an event filter on it (InstallEventFilter) instead", method, h.class))
	}
	vt := info.vtable
	idx := -1
	for i := range vt {
		if vt[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		panic(fmt.Sprintf("%s: %s has no overridable virtual %s; it cannot be overridden on this class (see COVERAGE.md)",
			method, h.class, name))
	}
	if f != nil && !vt[idx].Fits(f) {
		panic(fmt.Sprintf("%s: %s declares %s with a different signature (%T does not fit); call Override%s on the %s wrapper",
			method, h.class, name, f, name, h.class))
	}

	h.mu.Lock()
	e := h.x()
	sub := h.subLocked()
	if sub.self != nil && f != nil && vt[idx].Has(sub.self) && UserDefined(sub.self, name) {
		h.mu.Unlock()
		panic(fmt.Sprintf("%s: the Go type %T of this %s already defines %s; change that method instead of overriding it per object",
			method, sub.self, h.class, name))
	}
	if sub.vtable == nil {
		sub.vtable = vt
	}
	if len(sub.overrides) < len(vt) {
		sub.overrides = append(sub.overrides, make([]any, len(vt)-len(sub.overrides))...)
	}
	sub.overrides[idx] = f
	has := false
	for _, o := range sub.overrides {
		if o != nil {
			has = true
			break
		}
	}
	var newID, oldID uintptr
	switch {
	case has && e.selfSlot == 0:
		e.selfSlot = newSlot(h)
		newID = e.selfSlot
	case !has && sub.self == nil && e.selfSlot != 0:
		// nothing left to dispatch to: drop the id, so the object is no
		// longer anchored for it (and can be collected as before)
		oldID, e.selfSlot = e.selfSlot, 0
		sub.overrides = nil
	}
	id := e.selfSlot
	mask := make([]uint64, (len(vt)+63)/64)
	if len(mask) == 0 {
		mask = []uint64{0}
	}
	if sub.self != nil {
		copy(mask, Mask(sub.self, sub.vtable))
	}
	for i, o := range sub.overrides {
		if o != nil {
			mask[i/64] |= 1 << (i % 64)
		}
	}
	h.mu.Unlock()

	if newID != 0 {
		// as SetSelf: the death notice or the handle's collection drops it
		runtime.AddCleanup(h, dropSlot, newID)
		h.anchorPin()
	}
	if oldID != 0 {
		slots.Delete(oldID)
		h.anchorUnpin()
	}
	info.shimInit(h.ptr, id, &mask[0])
}

// OverrideCount is the number of per-object overrides the handle has
// (tests).
func (h *Handle) OverrideCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	if e := h.ext.Load(); e != nil && e.sub != nil {
		for _, o := range e.sub.overrides {
			if o != nil {
				n++
			}
		}
	}
	return n
}

// IsShim reports whether a qute constructor built the object as its class's
// shim subclass, so Go can override its virtuals per object.
func (h *Handle) IsShim() bool { return h != nil && h.shim }

// UseProtected checks that the object behind wrapper may have class's
// protected methods called on it: the shim's C functions cast to class's
// shim, so the object must have been built as exactly that shim (by any of
// class's constructors, plain or New...With). An object Qt created, or one
// of a derived class reached through a base wrapper, would make that cast
// undefined behaviour in C++, so both panic.
func UseProtected(wrapper unsafe.Pointer, method, class string) {
	Use(wrapper, method)
	GUIOnly(method)
	h := (*Ref)(wrapper).H
	if !h.shim {
		panic(fmt.Sprintf("%s: this %s was not made by a qute constructor (Qt created it), so Go cannot call its protected methods",
			method, h.class))
	}
	if h.class != class {
		panic(fmt.Sprintf("%s: the object is a %s; call Protected on its *%s wrapper (a %sCast or the wrapper its constructor returned)",
			method, h.class, h.class, h.class))
	}
}
