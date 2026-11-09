// Package rt is the goqt runtime. It is the only layer that decides who frees
// a Qt object: every wrapped C++ object has exactly one Handle, and the
// handle's ownership state changes only through the transitions in this file.
package rt

import (
	"fmt"
	"reflect"
	"runtime"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"
)

// State is the ownership state of a handle.
type State uint32

const (
	// Owned objects are freed by Go: by a GC cleanup or by Delete().
	Owned State = iota
	// Parented objects are freed by their Qt parent or container.
	Parented
	// Borrowed objects belong to someone else; Go never frees them.
	Borrowed
	// Dead objects have already been destroyed.
	Dead
)

func (s State) String() string {
	switch s {
	case Owned:
		return "Owned"
	case Parented:
		return "Parented"
	case Borrowed:
		return "Borrowed"
	case Dead:
		return "Dead"
	}
	return fmt.Sprintf("State(%d)", uint32(s))
}

// Kind says how the runtime tracks an object's lifetime.
type Kind uint8

const (
	// KindQObject objects are registered and watched through QObject::destroyed.
	KindQObject Kind = iota
	// KindNotify objects are registered; a shim destructor reports their death.
	KindNotify
	// KindPlain objects have no death notice, so they are never registered and
	// every wrap yields a fresh handle.
	KindPlain
	// KindValue objects are heap copies of Qt value classes, always owned by
	// the handle that created them.
	KindValue
)

// Handle is the Go-side identity of one C++ object.
type Handle struct {
	ptr     unsafe.Pointer // registry key: QObject* or the object pointer
	class   string
	kind    Kind
	state   atomic.Uint32
	pins    atomic.Int32
	serial  uint64
	deleter func(unsafe.Pointer)

	mu         sync.Mutex
	cleanup    runtime.Cleanup
	hasCleanup bool
	visiblePin bool
	keepPins   int32
	cache      []any
	selfID     cgo.Handle
	deathNote  string

	// Self is the Go value that overrides virtuals (nil for plain wrappers).
	Self any
	// VTable is the generated dispatch table for Self.
	VTable []Virtual

	created string // call site of creation (debug builds)
	died    string // call site of death (debug builds)
}

// Ref is the only field of every generated wrapper root struct. All wrapper
// structs in the primary-base chain start with it, so a pointer to any wrapper
// can be reinterpreted as *Ref.
type Ref struct {
	H *Handle
	P unsafe.Pointer // pointer to the C++ object as this wrapper's class
}

// RefOf reinterprets a pointer to a generated wrapper struct as its Ref.
func RefOf(wrapper unsafe.Pointer) *Ref { return (*Ref)(wrapper) }

var serialCounter atomic.Uint64

type regEntry struct {
	w       weak.Pointer[Handle]
	serial  uint64
	watched bool
}

var (
	regMu    sync.Mutex
	registry = map[unsafe.Pointer]*regEntry{}
	strongMu sync.Mutex
	strong   = map[*Handle]struct{}{}
)

// State returns the handle's current ownership state.
func (h *Handle) State() State {
	if h == nil {
		return Dead
	}
	return State(h.state.Load())
}

// Class is the C++ class name the handle was created with.
func (h *Handle) Class() string { return h.class }

// Key is the registry key (QObject* for QObjects).
func (h *Handle) Key() unsafe.Pointer { return h.ptr }

// Alive reports whether the C++ object still exists.
func (h *Handle) Alive() bool { return h != nil && h.State() != Dead }

// Lookup returns the live handle registered for key, or nil.
func Lookup(key unsafe.Pointer) *Handle {
	regMu.Lock()
	defer regMu.Unlock()
	if e, ok := registry[key]; ok {
		return e.w.Value()
	}
	return nil
}

// WrapMode says what to do with the state of an already-wrapped object.
type WrapMode uint8

const (
	// KeepOrBorrow keeps an existing handle's state; new handles are Borrowed.
	KeepOrBorrow WrapMode = iota
	// TakeOwned forces Owned (transfer-to-go returns and constructors).
	TakeOwned
	// TakeParented forces Parented.
	TakeParented
	// NewScoped makes an unregistered handle that dies when the callback returns.
	NewScoped
)

// Wrap returns the handle for the C++ object at key, creating it if needed.
// Wrapping the same pointer twice returns the same handle, so its state never
// diverges.
func Wrap(key unsafe.Pointer, class string, kind Kind, mode WrapMode, deleter func(unsafe.Pointer)) *Handle {
	if key == nil {
		return nil
	}
	if mode == NewScoped {
		h := newHandle(key, class, KindPlain, Borrowed, deleter)
		return h
	}
	if kind == KindPlain || kind == KindValue {
		st := Borrowed
		switch mode {
		case TakeOwned:
			st = Owned
		case TakeParented:
			st = Parented
		}
		if kind == KindPlain && st == Owned && deleter == nil {
			st = Borrowed
		}
		h := newHandle(key, class, kind, st, deleter)
		if st == Owned {
			h.attachCleanup()
		}
		return h
	}

	regMu.Lock()
	e := registry[key]
	if e != nil {
		if h := e.w.Value(); h != nil && h.State() != Dead {
			regMu.Unlock()
			switch mode {
			case TakeOwned:
				h.TransferToGo()
			case TakeParented:
				h.TransferToCpp()
			}
			return h
		}
	}
	st := Borrowed
	switch mode {
	case TakeOwned:
		st = Owned
	case TakeParented:
		st = Parented
	}
	h := newHandle(key, class, kind, st, deleter)
	needWatch := kind == KindQObject
	if e != nil {
		needWatch = needWatch && !e.watched
		e.w = weak.Make(h)
		e.serial = h.serial
		e.watched = e.watched || needWatch
	} else {
		registry[key] = &regEntry{w: weak.Make(h), serial: h.serial, watched: needWatch}
	}
	regMu.Unlock()
	if needWatch {
		watch(key)
	}
	if st == Owned {
		h.attachCleanup()
	}
	return h
}

func newHandle(key unsafe.Pointer, class string, kind Kind, st State, deleter func(unsafe.Pointer)) *Handle {
	h := &Handle{ptr: key, class: class, kind: kind, deleter: deleter, serial: serialCounter.Add(1)}
	h.state.Store(uint32(st))
	if debugEnabled {
		h.created = callSite()
	}
	return h
}

// View returns the cached wrapper of type T for (h, p), creating it once.
func View[T any](h *Handle, p unsafe.Pointer) *T {
	if h == nil || p == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.cache {
		if v, ok := c.(*T); ok && RefOf(unsafe.Pointer(v)).P == p {
			return v
		}
	}
	// An upcast of a more-derived wrapper returns its embedded base, so
	// label.AsQWidget() and a QWidget* returned by Qt are the same Go pointer.
	want := reflect.TypeFor[T]()
	for _, c := range h.cache {
		if RefOf(reflect.ValueOf(c).UnsafePointer()).P != p {
			continue
		}
		f := reflect.ValueOf(c).Elem()
		for f.Kind() == reflect.Struct && f.NumField() > 0 && f.Type().Field(0).Anonymous {
			f = f.Field(0)
			if f.Type() == want {
				return (*T)(f.Addr().UnsafePointer())
			}
		}
	}
	v := new(T)
	*RefOf(unsafe.Pointer(v)) = Ref{H: h, P: p}
	h.cache = append(h.cache, v)
	return v
}

// TransferToCpp marks the object as freed by its Qt parent or container and
// cancels its GC cleanup.
func (h *Handle) TransferToCpp() {
	if h == nil {
		return
	}
	for {
		s := h.State()
		if s == Dead || s == Parented {
			return
		}
		if h.state.CompareAndSwap(uint32(s), uint32(Parented)) {
			h.stopCleanup()
			return
		}
	}
}

// TransferToGo makes Go responsible for freeing the object again.
func (h *Handle) TransferToGo() {
	if h == nil {
		return
	}
	for {
		s := h.State()
		if s == Dead || s == Owned {
			return
		}
		if h.deleter == nil {
			return
		}
		if h.state.CompareAndSwap(uint32(s), uint32(Owned)) {
			h.attachCleanup()
			return
		}
	}
}

// SetParented applies a transfer-this rule: Parented when the new parent is
// non-nil, Owned when it is nil.
func (h *Handle) SetParented(hasParent bool) {
	if hasParent {
		h.TransferToCpp()
	} else {
		h.TransferToGo()
	}
}

type cleanupArg struct {
	key        unsafe.Pointer
	serial     uint64
	kind       Kind
	deleter    func(unsafe.Pointer)
	registered bool
}

func (h *Handle) attachCleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hasCleanup || h.deleter == nil {
		return
	}
	arg := cleanupArg{key: h.ptr, serial: h.serial, kind: h.kind, deleter: h.deleter,
		registered: h.kind == KindQObject || h.kind == KindNotify}
	h.cleanup = runtime.AddCleanup(h, enqueueCleanup, arg)
	h.hasCleanup = true
}

func (h *Handle) stopCleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hasCleanup {
		h.cleanup.Stop()
		h.hasCleanup = false
	}
}

// Pin holds the handle strongly so GC cannot free it or its Go state.
func (h *Handle) Pin() {
	if h.pins.Add(1) == 1 {
		strongMu.Lock()
		strong[h] = struct{}{}
		strongMu.Unlock()
	}
}

// Unpin releases one Pin.
func (h *Handle) Unpin() {
	if n := h.pins.Add(-1); n == 0 {
		strongMu.Lock()
		delete(strong, h)
		strongMu.Unlock()
	} else if n < 0 {
		h.pins.Store(0)
	}
}

// Pins returns the current pin count.
func (h *Handle) Pins() int { return int(h.pins.Load()) }

// Keep pins the object explicitly until Unkeep.
func (h *Handle) Keep() {
	if !h.Alive() {
		return
	}
	h.mu.Lock()
	h.keepPins++
	h.mu.Unlock()
	h.Pin()
}

// Unkeep releases one Keep.
func (h *Handle) Unkeep() {
	h.mu.Lock()
	if h.keepPins == 0 {
		h.mu.Unlock()
		return
	}
	h.keepPins--
	h.mu.Unlock()
	h.Unpin()
}

// SetVisiblePin pins or unpins the handle for being a visible top-level window.
func (h *Handle) SetVisiblePin(visible bool) {
	h.mu.Lock()
	if h.visiblePin == visible {
		h.mu.Unlock()
		return
	}
	h.visiblePin = visible
	h.mu.Unlock()
	if visible {
		h.Pin()
	} else {
		h.Unpin()
	}
}

// SetSelf attaches a Go subclass value and its dispatch table, pins the handle
// for as long as the object lives, and returns the id the shim dispatches with.
func (h *Handle) SetSelf(self any, vt []Virtual) uintptr {
	h.Self = self
	h.VTable = vt
	h.selfID = cgo.NewHandle(h)
	h.Pin()
	return uintptr(h.selfID)
}

// Destroyed is called when Qt (or a shim destructor) reports that the object
// at key is gone. The registry entry is removed before the memory is freed, so
// a new object at the same address gets a fresh handle.
func Destroyed(key unsafe.Pointer, why string) {
	regMu.Lock()
	e := registry[key]
	delete(registry, key)
	regMu.Unlock()
	if e == nil {
		return
	}
	if h := e.w.Value(); h != nil {
		h.markDead(why)
	}
}

// Kill marks an unregistered handle Dead (scoped arguments, plain deletes).
func (h *Handle) Kill(why string) {
	if h != nil {
		h.markDead(why)
	}
}

func (h *Handle) markDead(why string) {
	old := State(h.state.Swap(uint32(Dead)))
	if old == Dead {
		return
	}
	h.stopCleanup()
	h.mu.Lock()
	if h.deathNote == "" {
		h.deathNote = why
	}
	if debugEnabled {
		h.died = callSite()
	}
	visible, keeps, id := h.visiblePin, h.keepPins, h.selfID
	h.visiblePin, h.keepPins, h.selfID = false, 0, 0
	h.mu.Unlock()
	if visible {
		h.Unpin()
	}
	for ; keeps > 0; keeps-- {
		h.Unpin()
	}
	if id != 0 {
		id.Delete()
		h.Unpin()
	}
}

// Delete frees the object now. It is idempotent and works on Owned and
// Parented objects; on Borrowed objects it panics.
func (h *Handle) Delete(method string) {
	if h == nil {
		return
	}
	switch h.State() {
	case Dead:
		return
	case Borrowed:
		panic(fmt.Sprintf("%s: object is Borrowed, so Go does not own it and may not delete it (rule 4); "+
			"let its owner delete it, or call DeleteLater() on a QObject", method))
	}
	if debugEnabled && InCallback() && h.kind == KindQObject {
		panic(fmt.Sprintf("%s: Delete() inside a signal or event handler (rule 5); Qt may still be using the object, "+
			"call DeleteLater() instead", method))
	}
	if h.deleter == nil {
		panic(fmt.Sprintf("%s: %s has no public destructor", method, h.class))
	}
	h.stopCleanup()
	h.mu.Lock()
	h.deathNote = "deleted by Delete()"
	h.mu.Unlock()
	key, del, kind := h.ptr, h.deleter, h.kind
	if kind == KindQObject || kind == KindNotify {
		del(key) // the death notice marks the handle Dead
		Destroyed(key, "deleted by Delete()")
	}
	h.markDead("deleted by Delete()")
	if kind != KindQObject && kind != KindNotify {
		del(key)
	}
}

// Use checks that the wrapper is usable and returns its C++ pointer. It is the
// one atomic load every generated method pays.
func Use(wrapper unsafe.Pointer, method string) unsafe.Pointer {
	if wrapper == nil {
		panic(fmt.Sprintf("%s: called on a nil pointer; create the object with its New function first", method))
	}
	r := (*Ref)(wrapper)
	if r.H == nil {
		panic(fmt.Sprintf("%s: called on a zero-value wrapper; create the object with its New function, "+
			"or for a Go subclass pass it to the NewXWith constructor first", method))
	}
	if State(r.H.state.Load()) == Dead {
		panic(deadMessage(r.H, method))
	}
	if debugEnabled {
		checkThread(method)
	}
	return r.P
}

// UseArg is Use for an argument that may be nil.
func UseArg(wrapper unsafe.Pointer, method string) unsafe.Pointer {
	if wrapper == nil {
		return nil
	}
	return Use(wrapper, method)
}

// CheckThread panics when called off the GUI thread in debug builds.
func CheckThread(method string) {
	if debugEnabled {
		checkThread(method)
	}
}

func deadMessage(h *Handle, method string) string {
	h.mu.Lock()
	why, site, created := h.deathNote, h.died, h.created
	h.mu.Unlock()
	if why == "" {
		why = "destroyed"
	}
	if site != "" {
		why += " at " + site
	}
	msg := fmt.Sprintf("%s: object destroyed (%s); ", method, why)
	if h.deathNote == scopedNote {
		msg = fmt.Sprintf("%s: %s argument used after its callback returned (rule 6); "+
			"copy the values you need inside the handler", method, h.class)
	} else {
		msg += "keep its owner alive while you use it, or check Alive() first"
	}
	if created != "" {
		msg += fmt.Sprintf(" [created at %s]", created)
	}
	return msg
}

const scopedNote = "scoped argument expired"

// Expire marks a scoped argument handle Dead when its callback returns.
func (h *Handle) Expire() {
	if h != nil {
		h.markDead(scopedNote)
	}
}

// Stats reports registry and pin counts (for tests and DumpTree).
func Stats() (registered, pinned int) {
	regMu.Lock()
	registered = len(registry)
	regMu.Unlock()
	strongMu.Lock()
	pinned = len(strong)
	strongMu.Unlock()
	return
}

// Constructor modes: the pointer is fresh from a Go constructor.
const (
	NewOwned WrapMode = iota + 10
	NewParented
)

type classInfo struct {
	kind    Kind
	deleter func(unsafe.Pointer)
}

var classes sync.Map // name -> classInfo

// RegisterClass records how a generated class is tracked and freed.
func RegisterClass(name string, kind Kind, deleter func(unsafe.Pointer)) {
	classes.Store(name, classInfo{kind: kind, deleter: deleter})
}

// WrapClass is Wrap for a registered class.
func WrapClass(p unsafe.Pointer, class string, mode WrapMode) *Handle {
	if p == nil {
		return nil
	}
	v, ok := classes.Load(class)
	if !ok {
		panic("goqt: class " + class + " is not registered")
	}
	ci := v.(classInfo)
	kind := ci.kind
	switch mode {
	case NewOwned:
		return Wrap(p, class, kind, TakeOwned, ci.deleter)
	case NewParented:
		return Wrap(p, class, kind, TakeParented, ci.deleter)
	}
	if kind == KindNotify {
		// Only objects built by our shim constructors report their death, so
		// only those may be registered. Others get a fresh plain handle.
		if h := Lookup(p); h != nil {
			switch mode {
			case TakeOwned:
				h.TransferToGo()
			case TakeParented:
				h.TransferToCpp()
			}
			return h
		}
		kind = KindPlain
	}
	return Wrap(p, class, kind, mode, ci.deleter)
}

// ModeFor maps a public ownership request to a wrap mode.
func ModeFor(s State) WrapMode {
	switch s {
	case Owned:
		return TakeOwned
	case Parented:
		return TakeParented
	}
	return KeepOrBorrow
}

// Arg checks a wrapper argument and returns its pointer and handle.
func Arg(wrapper unsafe.Pointer, method string) (unsafe.Pointer, *Handle) {
	if wrapper == nil {
		return nil, nil
	}
	p := Use(wrapper, method)
	return p, (*Ref)(wrapper).H
}
