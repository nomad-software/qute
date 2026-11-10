package rt

import (
	"sync"
	"unsafe"
)

// A transfer-to-cpp rule says a container now owns its argument. Usually Qt
// also makes it a child (a layout installed on a widget reparents what is
// added to it), but a container can hold a QObject without parenting it: a
// layout that has no widget yet keeps its widgets parentless until it is
// installed. If such a container dies first, nothing frees the object: it is
// Parented for Go and parentless for Qt.
//
// So each such transfer is recorded as an adoption. When the container dies
// and the object is still alive with no parent, it goes back to Go: Owned,
// freed by GC once nothing refers to it (rule 7 in reverse).

type adoption struct {
	child  unsafe.Pointer
	serial uint64
	class  string
}

var (
	adoptMu   sync.Mutex
	adoptions = map[unsafe.Pointer][]adoption{} // container -> parentless children it took
)

// TransferTo applies a transfer-to-cpp rule for an argument that container
// takes. The argument is Parented; if Qt did not give it a parent, the
// container's death hands it back to Go.
func TransferTo(container, h *Handle) {
	if h == nil {
		return
	}
	h.TransferToCpp()
	if container == nil || container == h || !container.registeredKind() || !container.Alive() ||
		h.kind != KindQObject || h.State() != Parented || HasParent(h.ptr) {
		return
	}
	adoptMu.Lock()
	adoptions[container.ptr] = append(adoptions[container.ptr], adoption{child: h.ptr, serial: h.serial, class: h.class})
	adoptMu.Unlock()
}

// Adopted reports how many parentless objects container took (for tests).
func Adopted(container *Handle) int {
	adoptMu.Lock()
	defer adoptMu.Unlock()
	return len(adoptions[container.ptr])
}

// orphaned runs when the container at key is destroyed: what it took and
// left without a parent is Go's again.
func orphaned(key unsafe.Pointer) {
	adoptMu.Lock()
	list := adoptions[key]
	delete(adoptions, key)
	adoptMu.Unlock()
	for _, a := range list {
		regMu.Lock()
		e := registry[a.child]
		alive := e != nil && e.serial == a.serial
		var h *Handle
		if alive {
			h = e.w.Value()
		}
		regMu.Unlock()
		if !alive || HasParent(a.child) {
			continue // freed with the container, or Qt gave it a parent
		}
		if h != nil {
			if h.State() == Parented {
				h.TransferToGo()
			}
			continue
		}
		// nobody refers to it from Go: a fresh Owned handle that GC frees
		Wrap(a.child, a.class, KindQObject, TakeOwned, DeleteQObject)
	}
}
