package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewAppFiles checks that `qute new` writes the starter app's files, that
// each is Go that parses, and that the window title comes from the directory
// name. Nothing is built.
func TestNewAppFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-app")
	if err := newApp(dir, "example.com/myapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Error("no go.mod written:", err)
	}
	fset := token.NewFileSet()
	for _, name := range []string{"main.go", "window.go", "window_test.go", "screenshot_test.go"} {
		if _, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "window.go")); !strings.Contains(string(b), `SetWindowTitle("My App")`) {
		t.Error("the window title is not taken from the directory name")
	}
	if err := newApp(dir, "example.com/myapp"); err == nil {
		t.Error("newApp wrote into a directory that is not empty")
	}
}

// Inside an existing module no go.mod is written, so a module path would be
// ignored: newApp says so instead of dropping it.
func TestNewModuleInsideModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/outer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "app")
	if err := newApp(dir, "example.com/inner"); err == nil || !strings.Contains(err.Error(), "would be ignored") {
		t.Fatalf("newApp inside a module with a module path: %v, want an error", err)
	}
	if err := newApp(dir, ""); err != nil {
		t.Fatalf("newApp inside a module: %v", err)
	}
}
