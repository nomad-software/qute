package rt

import (
	"fmt"
	"runtime"
	"runtime/cgo"
	"sync/atomic"
	"unsafe"
)

var callbackDepth atomic.Int32

// InCallback reports whether a signal or virtual handler is running.
func InCallback() bool { return callbackDepth.Load() > 0 }

type callback struct {
	fn     func(args unsafe.Pointer)
	sender *Handle
}

// NewCallback stores fn for a C++ lambda that captures only the returned id.
// The sender stays pinned while the connection is live.
func NewCallback(sender *Handle, fn func(args unsafe.Pointer)) uintptr {
	if sender != nil {
		sender.Pin()
	}
	return uintptr(cgo.NewHandle(&callback{fn: fn, sender: sender}))
}

// InvokeCallback runs the closure stored under id with the shim's argument
// array.
func InvokeCallback(id uintptr, args unsafe.Pointer) {
	cb := cgo.Handle(id).Value().(*callback)
	callbackDepth.Add(1)
	defer callbackDepth.Add(-1)
	cb.fn(args)
}

// ReleaseCallback is called when Qt destroys the slot object, on Disconnect or
// when the sender dies. It frees the closure and drops the sender's pin.
func ReleaseCallback(id uintptr) {
	h := cgo.Handle(id)
	cb := h.Value().(*callback)
	h.Delete()
	if cb.sender != nil {
		cb.sender.Unpin()
	}
}

// Connection is a live signal connection.
type Connection struct {
	c *connState
}

type connState struct {
	conn unsafe.Pointer // QMetaObject::Connection*
	done atomic.Bool
}

// NewConnection wraps a heap QMetaObject::Connection*.
func NewConnection(conn unsafe.Pointer) Connection {
	s := &connState{conn: conn}
	runtime.AddCleanup(s, func(p unsafe.Pointer) { freeConnectionC(p) }, conn)
	return Connection{c: s}
}

// Disconnect breaks the connection and releases its closure. It is safe to
// call more than once, and after the sender has died.
func (c Connection) Disconnect() {
	if c.c == nil || c.c.done.Swap(true) {
		return
	}
	disconnectC(c.c.conn)
}

// Virtual is one overridable virtual method of a generated shim class.
type Virtual struct {
	Name string
	// Has reports whether the Go value overrides this virtual.
	Has func(self any) bool
	// Call converts the shim's arguments, calls the override and writes ret.
	Call func(self any, args, ret unsafe.Pointer)
}

// Dispatch is called by a shim for an overridden virtual.
func Dispatch(id uintptr, idx int, args, ret unsafe.Pointer) {
	h := cgo.Handle(id).Value().(*Handle)
	callbackDepth.Add(1)
	defer callbackDepth.Add(-1)
	if idx < 0 || idx >= len(h.VTable) {
		panic(fmt.Sprintf("qute: virtual index %d out of range for %s", idx, h.class))
	}
	h.VTable[idx].Call(h.Self, args, ret)
}

// Mask computes the override bitmask for self against vt.
func Mask(self any, vt []Virtual) []uint64 {
	m := make([]uint64, (len(vt)+63)/64)
	for i, v := range vt {
		if v.Has(self) && UserDefined(self, v.Name) {
			m[i/64] |= 1 << (i % 64)
		}
	}
	return m
}
