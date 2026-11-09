package rt

// #cgo pkg-config: Qt6Core
// #cgo CXXFLAGS: -std=c++17 -fPIC -O1 -g0
// #include "rt.h"
import "C"

import (
	"runtime"
	"runtime/cgo"
	"unsafe"
)

func init() {
	// Package init runs on the main goroutine while it is still on the main
	// OS thread. Locking here keeps it there for qute.Main and the event loop.
	runtime.LockOSThread()
	C.qute_rt_init()
}

//export qute_rt_run
func qute_rt_run(id C.uintptr_t) {
	h := cgo.Handle(id)
	f := h.Value().(func())
	h.Delete()
	f()
}

//export qute_rt_drain
func qute_rt_drain() {
	drainScheduled.Store(false)
	Drain()
}

//export qute_rt_destroyed
func qute_rt_destroyed(obj unsafe.Pointer) {
	Destroyed(obj, "destroyed by Qt")
}

// IsGUIThread reports whether the caller runs on the Qt GUI thread.
func IsGUIThread() bool { return bool(C.qute_rt_is_gui_thread()) }

// AppExists reports whether a Q*Application object exists.
func AppExists() bool { return bool(C.qute_rt_app_exists()) }

// ClassName returns the most-derived class name of a live QObject.
func ClassName(obj unsafe.Pointer) string { return C.GoString(C.qute_rt_class_name(obj)) }

// HasParent reports whether a live QObject has a parent.
func HasParent(obj unsafe.Pointer) bool { return bool(C.qute_rt_has_parent(obj)) }

// DeleteQObject is the deleter used for every QObject-derived handle.
func DeleteQObject(obj unsafe.Pointer) { C.qute_rt_delete_qobject(obj) }

// Free releases memory allocated with malloc by a shim.
func Free(p unsafe.Pointer) { C.qute_rt_free(p) }

func watch(obj unsafe.Pointer)            { C.qute_rt_watch(obj) }
func postC(id uintptr) bool               { return bool(C.qute_rt_post(C.uintptr_t(id))) }
func postDrainC() bool                    { return bool(C.qute_rt_post_drain()) }
func disconnectC(conn unsafe.Pointer)     { C.qute_rt_disconnect(conn) }
func freeConnectionC(conn unsafe.Pointer) { C.qute_rt_free_connection(conn) }
