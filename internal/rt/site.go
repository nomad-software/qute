package rt

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

const modulePrefix = "github.com/nomad-software/qute/"

// callSite returns the first caller outside this module and the Go runtime,
// formatted as file:line. Used by debug builds to say where an object was
// created or destroyed.
func callSite() string {
	pcs := make([]uintptr, 48)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		fn := f.Function
		if fn != "" && !libraryFrame(fn) && !strings.HasPrefix(fn, "runtime.") &&
			!strings.HasPrefix(fn, "_cgo") && !strings.HasPrefix(fn, "testing.") && !strings.HasPrefix(fn, "main.main") {
			return fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
		}
		if !more {
			return ""
		}
	}
}

// libraryFrame reports frames inside the binding itself (not its tests or
// examples).
func libraryFrame(fn string) bool {
	for _, p := range []string{"internal/rt.", "qtcore.", "qtgui.", "qtwidgets.", "goqt."} {
		if strings.HasPrefix(fn, modulePrefix+p) {
			return true
		}
	}
	return false
}
