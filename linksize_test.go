package qute_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A program links only the methods it can reach. One reflect
// MethodByName with a computed name would make the linker keep every
// exported method of every type (rt.MethodLookup): examples/hello, which
// overrides nothing per object, would carry thousands of Override setters
// and Base methods, more than doubling its stripped size.
func TestLinkerDropsUnusedMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("builds examples/hello")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command:", err)
	}
	bin := filepath.Join(t.TempDir(), "hello")
	if out, err := exec.Command(gobin, "build", "-o", bin, "./examples/hello").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command(gobin, "tool", "nm", bin).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	var kept []string
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if name := f[len(f)-1]; strings.HasPrefix(name, "github.com/nomad-software/qute/qtwidgets.") && strings.Contains(name, ").Override") {
			kept = append(kept, name)
		}
	}
	if len(kept) > 0 {
		t.Errorf("examples/hello links %d Override setters it never calls (first: %s); something calls reflect's Method or MethodByName with a computed name",
			len(kept), kept[0])
	}
}
