package rt

import (
	"fmt"
	"sync"
	"unsafe"
)

// Inline values. A value class that is trivially copyable and destructible
// (QModelIndex: two ints, an id and a model pointer) needs no C++ heap copy:
// its bytes are kept in Go memory that holds no Go pointers, which C++ may
// read and write during a call like any other object of the class. The GC
// frees it with its handle, so such a value needs no cleanup, no queued
// delete and no call into C to free it. Qt hands them to Go overrides and
// signal handlers for every row it asks about, so this is the hot path of
// models.
//
// The bytes never go where C keeps them: a list of inline values crosses as
// one block of the values themselves, and an override's result is copied
// into storage the shim provides (CopyValueOut), never stored as a pointer.

// CopyInline wraps a value Qt lent for the call (an override's or signal's
// const reference argument) by copying it into Go memory: the C++ object is
// neither kept nor freed.
const CopyInline WrapMode = 20

// RegisterInline records that class is an inline value of size bytes;
// construct builds a default value at p (what a nil element of a list means).
func RegisterInline(class string, size int, construct func(p unsafe.Pointer)) {
	zero := make([]uint64, (size+7)/8)
	construct(unsafe.Pointer(unsafe.SliceData(zero)))
	inlineZero.Store(class, unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(zero))), size))
	updateClass(class, func(ci *classInfo) { ci.inline = size })
}

var inlineZero sync.Map // class -> the bytes of a default-constructed value

// newInline returns an Owned handle of a copy of the value at p, in Go memory.
func newInline(p unsafe.Pointer, class string, size int) *Handle {
	buf := make([]uint64, (size+7)/8) // pointer-free and 8-byte aligned
	dst := unsafe.Pointer(unsafe.SliceData(buf))
	copy(unsafe.Slice((*byte)(dst), size), unsafe.Slice((*byte)(p), size))
	h := newHandle(dst, class, KindValue, Owned, nil)
	h.inline = true
	h.attachDependency()
	return h
}

// InlineSize returns the size of an inline value class, or 0.
func InlineSize(class string) int {
	if v, ok := classes.Load(class); ok {
		return v.(classInfo).inline
	}
	return 0
}

// CopyValueOut copies the inline value a Go override returned into the
// storage the shim provides (dst). A nil result leaves dst as the shim made
// it, a default-constructed value.
func CopyValueOut(dst, wrapper unsafe.Pointer, class, method string) {
	if wrapper == nil {
		return
	}
	src := Use(wrapper, method)
	n := InlineSize(class)
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}

// InlineBlock copies inline values into one block of n values, which C++
// reads as an array of the class (a list argument, or a list an override
// returns). alloc provides the block: Go memory for an argument, C memory for
// a result the shim takes over.
func InlineBlock(class, method string, n int, at func(i int) unsafe.Pointer, alloc func(bytes int) unsafe.Pointer) unsafe.Pointer {
	size := InlineSize(class)
	if size == 0 {
		panic(fmt.Sprintf("qute: %s is not an inline value class", class))
	}
	block := alloc(max(n, 1) * size)
	var zero []byte
	for i := range n {
		dst := unsafe.Slice((*byte)(unsafe.Add(block, i*size)), size)
		w := at(i)
		if w == nil {
			// a nil element is a default value, as a nil argument is
			if zero == nil {
				v, _ := inlineZero.Load(class)
				zero = v.([]byte)
			}
			copy(dst, zero)
			continue
		}
		copy(dst, unsafe.Slice((*byte)(Use(w, method)), size))
	}
	return block
}
