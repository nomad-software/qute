package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// childTags are the build tags this test binary was built with, passed on to
// the starter app's builds: with other tags they would compile every Qt
// package a second time, which on a cold CI cache (the qute_debug job) took
// longer than the test's timeout.
var childTags []string

// TestNewAppPasses writes a starter app with `qute new`, points it at this
// checkout and runs its own tests: the templates must build, pass and pass
// qute vet from the start. It needs no network (GOPROXY=off).
func TestNewAppPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a whole app")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "my-app")
	if err := newApp(dir, "example.com/myapp"); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal("no go.mod written outside a module:", err)
	}
	mod = append(mod, []byte("\nrequire github.com/nomad-software/qute v0.0.0\n\nreplace github.com/nomad-software/qute => "+repo+"\n")...)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod", "QT_QPA_PLATFORM=offscreen")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}
	run("go", append(append([]string{"vet"}, childTags...), "./...")...)
	run("go", append(append([]string{"test"}, childTags...), "./...")...)
	run("go", "run", "github.com/nomad-software/qute/cmd/qute", "vet", "./...") // exits non-zero on a finding
	if b, _ := os.ReadFile(filepath.Join(dir, "window.go")); !strings.Contains(string(b), `SetWindowTitle("My App")`) {
		t.Error("the window title is not taken from the directory name")
	}
}
