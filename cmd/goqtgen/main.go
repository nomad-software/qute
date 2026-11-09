// Command goqtgen generates the qtcore, qtgui and qtwidgets packages from the
// checked-in IR (spec/ir) and the class and ownership specs (spec/classes,
// spec/overlays). It needs neither Qt nor libclang.
//
//	go run ./cmd/goqtgen -root .
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, "goqtgen:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	g := &Gen{root: root, byName: map[string]*Module{}, classes: map[string]*Class{},
		enums: map[string]*Enum{}, structs: map[string]*structDef{}, report: &Report{}}
	mods := []struct{ name, pkg, pc string }{
		{"QtCore", "qtcore", "Qt6Core"},
		{"QtGui", "qtgui", "Qt6Gui"},
		{"QtWidgets", "qtwidgets", "Qt6Widgets"},
	}
	var prev []*Module
	for _, md := range mods {
		ir, err := loadIR(filepath.Join(root, "spec", "ir", md.pkg+".json"))
		if err != nil {
			return err
		}
		spec := &classSpec{}
		if err := loadYAML(filepath.Join(root, "spec", "classes", md.pkg+".yaml"), spec); err != nil {
			return err
		}
		ov := overlayFile{}
		if err := loadYAML(filepath.Join(root, "spec", "overlays", md.pkg+".yaml"), &ov); err != nil {
			return err
		}
		m := &Module{Name: md.name, Pkg: md.pkg, ImportPath: modulePath + "/" + md.pkg, PkgConfig: md.pc,
			IR: ir, Spec: spec, Overlays: ov, Deps: append([]*Module{}, prev...),
			imports: map[string]bool{}, pkgNames: map[string]string{}, usedOver: map[string]bool{}}
		g.modules = append(g.modules, m)
		g.byName[md.name] = m
		prev = append(prev, m)
	}
	g.initStructs()
	if err := g.build(); err != nil {
		return err
	}
	for _, m := range g.modules {
		if err := g.emitModule(m); err != nil {
			return err
		}
	}
	if err := g.writeReport(filepath.Join(root, "UNREVIEWED.md")); err != nil {
		return err
	}
	for _, w := range g.report.warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	n := 0
	for _, m := range g.modules {
		n += len(m.Classes)
	}
	fmt.Printf("goqtgen: %d classes in %d packages\n", n, len(g.modules))
	return nil
}
