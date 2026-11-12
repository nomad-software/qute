package rt

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// Every wrap looks up its class's classInfo by name (WrapClass, WrapNew,
// a signal argument, an inline result). The names are string literals of
// the generated code, which the linker stores once per program, so their
// data pointer identifies the class: this cache maps it to the classInfo
// with one atomic load and an integer-keyed map lookup instead of hashing
// the name in the classes sync.Map. Entries also hold the name, which is
// compared, so a name built at run time whose memory is later reused can
// never return another class. The cache is a copy-on-write snapshot,
// dropped whenever a class's record changes (registration at init, a
// shim's on first construction), and stops growing at classCacheMax
// entries, so names built at run time cannot grow it without bound.

type classEntry struct {
	name string
	ci   classInfo
}

var (
	classCache   atomic.Pointer[map[*byte]*classEntry]
	classCacheMu sync.Mutex
)

const classCacheMax = 4096

// classOf returns the record of a registered class. The record is shared
// and must not be changed: a change makes a new one (changeClass).
func classOf(name string) (*classInfo, bool) {
	key := unsafe.StringData(name)
	if m := classCache.Load(); m != nil {
		if e := (*m)[key]; e != nil && e.name == name {
			return &e.ci, true
		}
	}
	return cacheClass(key, name)
}

// cacheClass looks name up and adds its record, read under the lock that
// every change of a record takes (storeClass), so the cache never keeps one
// that changed meanwhile.
func cacheClass(key *byte, name string) (*classInfo, bool) {
	classCacheMu.Lock()
	defer classCacheMu.Unlock()
	v, ok := classes.Load(name)
	if !ok {
		return nil, false
	}
	e := &classEntry{name: name, ci: v.(classInfo)}
	old := classCache.Load()
	n := 0
	if old != nil {
		n = len(*old)
	}
	if n >= classCacheMax {
		return &e.ci, true
	}
	m := make(map[*byte]*classEntry, n+1)
	if old != nil {
		for k, x := range *old {
			m[k] = x
		}
	}
	m[key] = e
	classCache.Store(&m)
	return &e.ci, true
}

// storeClass records a class's classInfo and drops the cache, which may
// hold the old record.
func storeClass(name string, ci classInfo) {
	classCacheMu.Lock()
	defer classCacheMu.Unlock()
	classes.Store(name, ci)
	classCache.Store(nil)
}

// changeClass applies f to a registered class's record, all under the lock.
func changeClass(name string, f func(*classInfo)) bool {
	classCacheMu.Lock()
	defer classCacheMu.Unlock()
	v, ok := classes.Load(name)
	if !ok {
		return false
	}
	ci := v.(classInfo)
	f(&ci)
	classes.Store(name, ci)
	classCache.Store(nil)
	return true
}
