package qute

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	rt "github.com/nomad-software/qute/internal/rt"
)

// TestMain runs a package's tests with a QApplication on the main thread.
// Use it from the package's TestMain:
//
//	func TestMain(m *testing.M) { qute.TestMain(m) }
//
// Test functions run on other goroutines, so wrap Qt calls in Run(t, ...).
// QT_QPA_PLATFORM defaults to offscreen, so tests need no display.
func TestMain(m *testing.M) {
	if os.Getenv("QT_QPA_PLATFORM") == "" {
		os.Setenv("QT_QPA_PLATFORM", "offscreen")
	}
	done := make(chan int, 1)
	Main(func() {
		go func() {
			code := m.Run()
			done <- code
			Quit(code)
		}()
	})
	os.Exit(<-done)
}

// Run executes f on the GUI thread and waits. A panic in f fails t with the
// panic message instead of crashing the test binary. A panic in a signal or
// event handler that f triggers is recovered where Qt called the handler, so
// Qt's own frames return normally; it fails t too, and the input helpers
// (Click, Type, Key, Wait, ProcessEvents) re-raise it as soon as they return.
func Run(t testing.TB, f func()) {
	t.Helper()
	var failure string
	PostSync(func() {
		stop := rt.CapturePanics()
		defer func() {
			cp := stop()
			if p := recover(); p != nil {
				failure = fmt.Sprint(p)
				if _, ok := p.(runtime.Error); ok {
					failure += "\n" + string(debug.Stack())
				}
			}
			if cp != nil {
				if failure != "" {
					failure += "\n"
				}
				failure += cp.String()
			}
		}()
		f()
	})
	if failure != "" {
		t.Fatal(failure)
	}
}

// CollectGarbage runs the GC until queued cleanups have been handed to the
// GUI thread, then lets the GUI thread free them. Use it in tests to check
// that unreachable objects really are freed. Call it off the GUI thread.
func CollectGarbage() {
	if rt.IsGUIThread() {
		panic("qute.CollectGarbage: call it outside Run, so the GUI thread can free objects")
	}
	for i := 0; i < 4; i++ {
		runtime.GC()
		time.Sleep(5 * time.Millisecond) // cleanups run on their own goroutine
		PostSync(func() { rt.Drain() })
	}
}

// Freed returns how many objects GC cleanups have freed so far.
func Freed() int64 { return rt.Drained.Load() }
