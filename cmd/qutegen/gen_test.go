package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// These tests load the checked-in IR and specs (no Qt needed) and check the
// decisions the generator makes for shapes that have gone wrong before.

var (
	loadOnce sync.Once
	loaded   *Gen
	loadErr  error
)

func gen(t *testing.T) *Gen {
	t.Helper()
	loadOnce.Do(func() { loaded, loadErr = load(filepath.Join("..", "..")) })
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	return loaded
}

func class(t *testing.T, name string) *Class {
	t.Helper()
	c := gen(t).classes[name]
	if c == nil {
		t.Fatalf("class %s is not bound", name)
	}
	return c
}

// method finds a bound constructor, method or static by its C++ signature prefix.
func method(t *testing.T, cls, sig string) *Method {
	t.Helper()
	c := class(t, cls)
	for _, mm := range append(append(append([]*Method{}, c.Ctors...), c.Methods...), c.Statics...) {
		if strings.Contains(mm.Sig, sig) {
			return mm
		}
	}
	t.Fatalf("%s: no bound method matching %q", cls, sig)
	return nil
}

func notBound(t *testing.T, cls, sig string) {
	t.Helper()
	c := class(t, cls)
	for _, mm := range append(append([]*Method{}, c.Ctors...), c.Methods...) {
		if strings.Contains(mm.Sig, sig) {
			t.Errorf("%s is bound as %s, want it skipped", mm.Sig, mm.GoName)
		}
	}
}

func TestOutConvShapes(t *testing.T) {
	g := gen(t)
	p := func(cpp string) irParam { return irParam{Name: "x", Type: irType{Spelling: cpp, Canonical: cpp}} }
	cases := []struct {
		cpp, rule string
		ctor      bool
		out       bool // recognised at all
		inout     bool
	}{
		{"int *", "", false, true, false},
		{"bool *", "", false, true, false},
		{"int &", "", false, true, true},
		{"QString &", "", false, true, true},
		{"const int *", "", false, false, false},
		{"unsigned char *", "", false, false, false}, // raw memory: unsafe.Pointer
		{"QByteArray *", "", true, false, false},     // a constructor keeps the pointer
		{"int *", "", true, false, false},
		{"QPalette &", "", false, true, false},      // inferred as out ...
		{"QPalette &", "in-out", false, true, true}, // ... unless the overlay says in-out
		{"float *", "out", false, true, false},
		{"QWidget *", "out", false, false, false}, // object pointers are never out-parameters
	}
	for _, c := range cases {
		oc, inout, _, _ := g.outConv(p(c.cpp), c.ctor, c.rule)
		if (oc != nil) != c.out || inout != c.inout {
			t.Errorf("outConv(%q, ctor=%v, rule=%q) = out %v inout %v, want %v %v", c.cpp, c.ctor, c.rule, oc != nil, inout, c.out, c.inout)
		}
	}
}

// Every out-parameter the type alone suggested must be confirmed in an
// overlay: the guess bound QMatrix4x4::copyDataTo to one float.
func TestEveryOutParameterIsConfirmed(t *testing.T) {
	g := gen(t)
	for _, m := range g.modules {
		for _, c := range m.Classes {
			for _, mm := range append(append(append([]*Method{}, c.Ctors...), c.Methods...), c.Statics...) {
				for _, p := range mm.Params {
					if (p.Out || p.InOut) && !p.Ruled {
						t.Errorf("%s: %s is an inferred %s; confirm it with an out or in-out rule", mm.Sig, p.GoName, p.Rule)
					}
				}
			}
		}
	}
}

func TestFixedBindings(t *testing.T) {
	notBound(t, "QMatrix4x4", "copyDataTo")
	notBound(t, "QDataStream", "QDataStream(QByteArray *")
	notBound(t, "QImage", "QImage(uchar *")

	if p := method(t, "QFileDevice", "unmap").Params[0]; p.Out || p.T.k != kRaw {
		t.Errorf("QFileDevice::unmap address: out=%v kind=%v, want an unsafe.Pointer input", p.Out, p.T.k)
	}
	if p := method(t, "QStyle", "polish(QPalette &").Params[0]; !p.InOut {
		t.Error("QStyle::polish(QPalette &) palette is not in-out")
	}
	if p := method(t, "QClipboard", "text(QString & subtype").Params[0]; !p.InOut {
		t.Error("QClipboard::text subtype is not in-out")
	}
	if mm := method(t, "QDataStream", "QDataStream(const QByteArray &"); mm.GoName != "NewQDataStreamWithBytes" {
		t.Errorf("read-only byte stream is named %s, want NewQDataStreamWithBytes", mm.GoName)
	}
}

func TestKeepRules(t *testing.T) {
	cases := []struct{ cls, sig, keep string }{
		{"QAbstractItemView", "setModel(", "setModel.model"},
		{"QDataStream", "QDataStream(QIODevice *", "device"},
		{"QDataStream", "setDevice(", "device"},
		{"QGraphicsView", "QGraphicsView(QGraphicsScene *", "scene"},
		{"QWidget", "addAction(QAction * action)", "each"},
		{"QWidget", "removeAction(", "release"},
	}
	for _, c := range cases {
		mm := method(t, c.cls, c.sig)
		var got string
		for _, p := range mm.Params {
			if p.T.k == kObj && p.Keep != "" {
				got = p.Keep
			}
		}
		if got != c.keep {
			t.Errorf("%s: keep %q, want %q", mm.Sig, got, c.keep)
		}
		if mm.Params[0].Rule != "borrowed" {
			t.Errorf("%s: rule %q, want borrowed (keep does not transfer ownership)", mm.Sig, mm.Params[0].Rule)
		}
	}
}

func TestKeepRuleWarnings(t *testing.T) {
	g := &Gen{report: &Report{}}
	c := &Class{Name: "QX", Cpp: "QX"}
	obj := &conv{k: kObj, cls: c}
	im := &irMethod{Name: "setThing", Static: true, Params: []irParam{{Name: "thing"}}}
	p := &Param{Name: "thing", GoName: "thing", T: obj}
	g.applyArgRule(c, im, &overlay{Args: map[string]string{"thing": "keep"}}, p, 0, false)
	if p.Keep != "" || len(g.report.warnings) != 1 {
		t.Errorf("keep on a static: Keep=%q warnings=%v, want none kept and one warning", p.Keep, g.report.warnings)
	}
	im.Static = false
	p = &Param{Name: "thing", GoName: "thing", T: obj}
	g.applyArgRule(c, im, &overlay{Args: map[string]string{"thing": "keep-forever"}}, p, 0, false)
	if len(g.report.warnings) != 2 {
		t.Errorf("unknown rule did not warn: %v", g.report.warnings)
	}
	p = &Param{Name: "thing", GoName: "thing", T: obj}
	g.applyArgRule(c, im, &overlay{Args: map[string]string{"thing": "keep:slot"}}, p, 0, false)
	if p.Keep != "slot" || p.Rule != "borrowed" || !p.Ruled {
		t.Errorf("keep:slot gave Keep=%q Rule=%q Ruled=%v", p.Keep, p.Rule, p.Ruled)
	}
}

// A virtual's non-const value-class pointer is Qt's own object: the override
// must get it in place (scoped), not a copy Qt never reads back.
func TestVirtualValuePointerInPlace(t *testing.T) {
	c := class(t, "QPushButton")
	var v *Method
	for _, x := range c.Virtuals {
		if x.IR.Name == "initStyleOption" {
			v = x
		}
	}
	if v == nil {
		t.Fatal("QPushButton::initStyleOption is not overridable")
	}
	if p := v.Params[0]; !p.View || p.Rule != "scoped" {
		t.Errorf("option: View=%v Rule=%q, want an in-place scoped argument", p.View, p.Rule)
	}
	var b buf
	gen(t).emitShimClass(c, &b)
	shim := b.String()
	i := strings.Index(shim, "void initStyleOption(")
	if i < 0 {
		t.Fatal("no initStyleOption override in the shim")
	}
	body := shim[i : i+strings.Index(shim[i:], "\n\t}")]
	if strings.Contains(body, "new QStyleOptionButton") || !strings.Contains(body, "static_cast<void *>(a0)") {
		t.Errorf("initStyleOption override does not pass the option in place:\n%s", body)
	}
}

func TestDependsOnEmitted(t *testing.T) {
	src := string(gen(t).emitClass(class(t, "QModelIndex")))
	if !strings.Contains(src, `rt.RegisterDependency("QModelIndex"`) || !strings.Contains(src, `"QAbstractItemModel"`) {
		t.Error("QModelIndex does not register its model as a dependency")
	}
}

// depends-on also accepts a getter declared on a base class and C++
// expressions, and a class may list several.
func TestDependsOnForms(t *testing.T) {
	g := gen(t)
	item := string(g.emitClass(class(t, "QWidgetItem")))
	if !strings.Contains(item, `rt.RegisterDependency("QWidgetItem"`) || !strings.Contains(item, "qute_QLayoutItem_widget") {
		t.Error("QWidgetItem does not register its widget (getter on the base) as a dependency")
	}
	cell := class(t, "QTextTableCell")
	src := string(g.emitClass(cell))
	for _, want := range []string{"C.qute_QTextTableCell_dep0(p), \"QTextTable\"", "C.qute_QTextTableCell_dep1(p), \"QTextDocument\""} {
		if !strings.Contains(src, want) {
			t.Errorf("QTextTableCell: no %s", want)
		}
	}
	var dep1 string
	for _, f := range g.classCFuncs(cell) {
		if strings.Contains(f.decl, "qute_QTextTableCell_dep1(") {
			dep1 = f.body
		}
	}
	if !strings.Contains(dep1, "static_cast<const QTextTableCell *>(self)->firstCursorPosition().document()") {
		t.Errorf("dep1 body does not expand $self:\n%s", dep1)
	}
}

func TestDependsOnYAML(t *testing.T) {
	var o classOpts
	src := "depends-on:\n  - model\n  - { cpp: \"$self->x()\", class: QFoo }\n"
	if err := yaml.Unmarshal([]byte(src), &o); err != nil {
		t.Fatal(err)
	}
	want := depList{{Getter: "model"}, {Cpp: "$self->x()", Class: "QFoo"}}
	if fmt.Sprint(o.DependsOn) != fmt.Sprint(want) {
		t.Errorf("DependsOn = %v, want %v", o.DependsOn, want)
	}
	if err := yaml.Unmarshal([]byte("depends-on: { cpp: x }\n"), &o); err == nil {
		t.Error("a cpp entry without class was accepted")
	}
	o = classOpts{}
	if err := yaml.Unmarshal([]byte("depends-on: model\n"), &o); err != nil || len(o.DependsOn) != 1 || o.DependsOn[0].Getter != "model" {
		t.Errorf("plain string form: %v, %v", o.DependsOn, err)
	}
}

// The report counts an inferred out-parameter as open until it is confirmed.
func TestReportListsInferredOutParameters(t *testing.T) {
	g := gen(t)
	mm := method(t, "QDate", "getDate")
	saved := mm.Params[0].Ruled
	mm.Params[0].Ruled = false
	defer func() { mm.Params[0].Ruled = saved }()
	path := filepath.Join(t.TempDir(), "COVERAGE.md")
	if err := g.writeReport(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "`year`: out (inferred)") {
		t.Error("the report does not list an unconfirmed out-parameter")
	}
}
