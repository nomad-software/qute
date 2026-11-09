package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type Module struct {
	Name       string // QtWidgets
	Pkg        string // qtwidgets
	ImportPath string
	PkgConfig  string
	IR         *irModule
	Spec       *classSpec
	Overlays   overlayFile
	Classes    []*Class
	Enums      []*Enum
	Deps       []*Module

	imports  map[string]bool
	pkgNames map[string]string // package-level Go identifier -> what claimed it
	usedOver map[string]bool   // overlay keys that matched something
}

type ClassKind int

const (
	KindObject ClassKind = iota // QObject-derived
	KindPlain                   // polymorphic or plain non-QObject class
	KindValue                   // copyable value class held in an owned handle
)

type Class struct {
	Name      string
	Module    *Module
	IR        *irClass
	Opts      *classOpts
	Kind      ClassKind
	QObject   bool
	Event     bool
	Base      *Class
	Secondary []*Class
	Root      *Class

	Ctors     []*Method
	ShimCtors []*Method // constructors of an abstract class, used only by its shim
	Methods   []*Method
	Statics   []*Method
	Signals   []*Method
	Virtuals  []*Method // flattened, for the shim
	Protected []*Method // flattened protected non-virtuals, for the Base type
	Shim      bool
	Abstract  bool
	DefCtor   bool // has a public constructor callable with no arguments
	HasDtor   bool
	Skipped   []skipNote
	cNames    map[string]int
	goNames   map[string]string
}

type skipNote struct {
	Sig, Why string
}

type Enum struct {
	cpp       string // Qt::AlignmentFlag, QLineEdit::EchoMode
	module    *Module
	ir        *irEnum
	goName    string
	flagsName string // Go name of the QFlags type, if any
	emitted   bool
	anon      bool
}

func (e *Enum) goTypeFor(flags bool) string {
	if flags && e.flagsName != "" {
		return e.flagsName
	}
	if e.flagsName != "" {
		return e.flagsName
	}
	return e.goName
}

type Param struct {
	Name   string // C++ name (may be empty)
	GoName string
	CName  string
	T      *conv
	Rule   string // transfer-to-cpp, transfer-this, transfer-to-go, borrowed, scoped
	Ruled  bool   // rule came from an overlay
}

type Method struct {
	Class    *Class
	Owner    *Class // class that declares it (for flattened virtuals)
	IR       *irMethod
	GoName   string
	CName    string
	Params   []*Param
	Ret      *conv
	RetRule  string
	RetRuled bool
	Overlay  *overlay
	Error    bool
	Numbered bool
	Full     bool // generated with all default args
	Emit     bool // emits a signal
	VIndex   int
	Sig      string
}

type Gen struct {
	root    string
	modules []*Module
	byName  map[string]*Module
	classes map[string]*Class
	enums   map[string]*Enum
	structs map[string]*structDef
	report  *Report
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true, "goto": true,
	"if": true, "import": true, "interface": true, "map": true, "package": true, "range": true,
	"return": true, "select": true, "struct": true, "switch": true, "type": true, "var": true,
	// identifiers the generated code uses
	"C": true, "rt": true, "unsafe": true, "this": true, "qtcore": true, "qtgui": true, "qtwidgets": true,
	"runtime": true, "fmt": true, "self": true, "args": true, "ret": true, "string": true, "len": true,
	"error": true, "bool": true, "int": true, "f": true, "e": true, "i": true, "v": true, "r": true, "p": true, "h": true,
	"new": true, "nil": true, "true": true, "false": true, "id": true, "mask": true, "b": true, "s": true, "a": true,
}

// Methods that never become bindings.
var globalSkip = map[string]bool{
	"tr": true, "trUtf8": true, "metaObject": true, "connect": true, "disconnect": true,
	"staticMetaObject": true, "d_func": true, "qt_metacast": true, "qt_metacall": true,
	"connectNotify": true, "disconnectNotify": true, "isSignalConnected": true, "receivers": true,
	"sender": true, "senderSignalIndex": true, "swap": true, "data_ptr": true, "detach": true,
	"isDetached": true, "handle": true, "devType": true, "paintEngine": true, "initPainter": true,
	"redirected": true, "sharedPainter": true, "metric": true, "nativeEvent": true,
	"inputMethodQuery": true, "setupUi": true, "dumpObjectTree": true, "dumpObjectInfo": true,
	"bindableObjectName": true, "qt_static_metacall": true, "qt_check_for_QGADGET_macro": true,
}

func exportName(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// paramNames names a method's parameters, inventing readable names for the
// ones the header leaves unnamed: setText(const QString &) gets "text",
// addWidget(QWidget *) gets "widget".
func paramNames(m *irMethod) []string {
	out := make([]string, len(m.Params))
	used := map[string]bool{}
	for i, p := range m.Params {
		n := p.Name
		if n == "" {
			if len(m.Params) == 1 && len(m.Name) > 3 && strings.HasPrefix(m.Name, "set") && unicode.IsUpper(rune(m.Name[3])) {
				n = lowerFirst(m.Name[3:])
			} else {
				base, _, _, _, _ := parseCpp(p.Type.Canonical)
				if i := strings.LastIndex(base, "::"); i >= 0 {
					base = base[i+2:]
				}
				if strings.HasPrefix(base, "Q") && len(base) > 1 && !strings.ContainsAny(base, "<> ") {
					n = lowerFirst(base[1:])
				}
			}
		}
		if n == "" || used[n] {
			n = fmt.Sprintf("arg%d", i)
		}
		used[n] = true
		out[i] = goParamName(n, i)
	}
	return out
}

func lowerFirst(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func goParamName(s string, i int) string {
	if s == "" {
		return fmt.Sprintf("arg%d", i)
	}
	if goKeywords[s] {
		return s + "Arg"
	}
	return s
}

// ---------------------------------------------------------------------------

func (g *Gen) build() error {
	// 1. classes
	for _, m := range g.modules {
		irc := map[string]*irClass{}
		for i := range m.IR.Classes {
			irc[m.IR.Classes[i].Name] = &m.IR.Classes[i]
		}
		names := make([]string, 0, len(m.Spec.Classes))
		for n := range m.Spec.Classes {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			ic := irc[n]
			if ic == nil {
				return fmt.Errorf("%s: class %s not found in IR", m.Name, n)
			}
			opts := m.Spec.Classes[n]
			if opts == nil {
				opts = &classOpts{}
			}
			c := &Class{Name: n, Module: m, IR: ic, Opts: opts, cNames: map[string]int{}, goNames: map[string]string{}}
			g.classes[n] = c
			m.Classes = append(m.Classes, c)
		}
	}
	// 2. hierarchy and kinds
	for _, m := range g.modules {
		for _, c := range m.Classes {
			for i, b := range c.IR.Bases {
				bc := g.classes[b]
				if bc == nil {
					if i == 0 {
						return fmt.Errorf("%s: primary base %s is not bound; add it to spec/classes", c.Name, b)
					}
					continue
				}
				if i == 0 {
					c.Base = bc
				} else {
					c.Secondary = append(c.Secondary, bc)
				}
			}
		}
	}
	for _, m := range g.modules {
		for _, c := range m.Classes {
			r := c
			for r.Base != nil {
				r = r.Base
			}
			c.Root = r
			c.QObject = r.Name == "QObject"
			for x := c; x != nil; x = x.Base {
				if x.Name == "QEvent" {
					c.Event = true
				}
			}
			switch {
			case c.Opts.Kind == "value":
				c.Kind = KindValue
			case c.QObject:
				c.Kind = KindObject
			default:
				c.Kind = KindPlain
			}
			if c.Opts.Kind == "event" {
				c.Event = true
			}
			c.Abstract = c.IR.Abstract
			for _, im := range c.IR.Methods {
				if im.Kind == "ctor" && im.Access == "public" && !im.Copy && !im.Move {
					all := true
					for _, p := range im.Params {
						all = all && p.Default
					}
					if all {
						c.DefCtor = true
					}
				}
			}
			if !hasCtor(c.IR) {
				c.DefCtor = true // implicit default constructor
			}
			c.HasDtor = c.IR.Dtor == nil || c.IR.Dtor.Access == "public"
		}
	}
	for _, m := range g.modules {
		for _, c := range m.Classes {
			if c.Kind == KindValue && c.Base != nil && c.Base.Kind == KindObject {
				return fmt.Errorf("%s: value class with object base", c.Name)
			}
		}
	}
	// 3. enums
	for _, m := range g.modules {
		g.collectEnums(m)
	}
	// 4. members
	for _, m := range g.modules {
		for _, c := range m.Classes {
			g.buildMembers(c)
		}
	}
	for _, m := range g.modules {
		for _, c := range m.Classes {
			g.buildShim(c)
		}
	}
	// 5. unused overlays
	for _, m := range g.modules {
		for cls, entries := range m.Overlays {
			if g.classes[cls] == nil {
				g.report.warn("%s: overlay for unbound class %s", m.Name, cls)
				continue
			}
			for k := range entries {
				if !m.usedOver[cls+"::"+k] {
					g.report.warn("%s: overlay %s::%s matched no method", m.Name, cls, k)
				}
			}
		}
	}
	return nil
}

func (m *Module) claim(name, what string) bool {
	if prev, ok := m.pkgNames[name]; ok && prev != what {
		return false
	}
	m.pkgNames[name] = what
	return true
}

func (g *Gen) collectEnums(m *Module) {
	add := func(e *irEnum, scope string, flags []irFlags, visible bool) {
		cpp := e.Name
		if scope != "" {
			cpp = scope + "::" + e.Name
		}
		en := &Enum{cpp: cpp, module: m, ir: e}
		if strings.Contains(e.Name, "(") {
			// anonymous enum: its values become untyped constants
			en.anon = true
			en.goName = ""
			en.emitted = true
			m.Enums = append(m.Enums, en)
			return
		}
		g.enums[cpp] = en
		if !visible {
			return
		}
		prefix := scope
		if scope == "Qt" {
			prefix = ""
		}
		en.goName = prefix + e.Name
		for _, f := range flags {
			if f.Enum == cpp {
				en.flagsName = prefix + f.Name
			}
		}
		if !m.claim(en.goName, "enum "+cpp) {
			en.goName = prefix + "Enum" + e.Name
			if !m.claim(en.goName, "enum "+cpp) {
				return
			}
		}
		if en.flagsName != "" && !m.claim(en.flagsName, "flags "+cpp) {
			en.flagsName = ""
		}
		en.emitted = true
		m.Enums = append(m.Enums, en)
	}
	for i := range m.IR.Enums {
		e := &m.IR.Enums[i]
		if e.Scope == "Qt" {
			add(e, "Qt", m.IR.Flags, true)
		}
	}
	for _, c := range m.Classes {
		for i := range c.IR.Enums {
			add(&c.IR.Enums[i], c.Name, c.IR.Flags, true)
		}
	}
}

func (g *Gen) methodSig(c *Class, m *irMethod) string {
	var ps []string
	for _, p := range m.Params {
		s := p.Type.Spelling
		if p.Name != "" {
			s += " " + p.Name
		}
		if p.Default {
			s += " = …"
		}
		ps = append(ps, s)
	}
	name := c.Name + "::" + m.Name
	ret := ""
	if m.Kind != "ctor" {
		ret = m.Return.Spelling + " "
	}
	cst := ""
	if m.Const {
		cst = " const"
	}
	return fmt.Sprintf("%s%s(%s)%s", ret, name, strings.Join(ps, ", "), cst)
}

// paramKey identifies a method's C++ signature for override/dup detection.
func paramKey(m *irMethod) string {
	var ps []string
	for _, p := range m.Params {
		ps = append(ps, p.Type.Canonical)
	}
	c := ""
	if m.Const {
		c = "const"
	}
	return m.Name + "(" + strings.Join(ps, ",") + ")" + c
}

func (g *Gen) skip(c *Class, m *irMethod, why string) {
	c.Skipped = append(c.Skipped, skipNote{Sig: g.methodSig(c, m), Why: why})
}

func (g *Gen) buildMembers(c *Class) {
	m := c.Module
	ancestorKeys := map[string]bool{}
	for b := c.Base; b != nil; b = b.Base {
		for i := range b.IR.Methods {
			im := &b.IR.Methods[i]
			if im.Access == "public" && im.Kind != "ctor" {
				ancestorKeys[paramKey(im)] = true
			}
		}
	}
	var ctors, methods, statics, signals, emits []*Method
	for i := range c.IR.Methods {
		im := &c.IR.Methods[i]
		if im.Access != "public" && !(im.Access == "protected" && im.Kind == "ctor") {
			continue
		}
		if im.Kind == "ctor" && im.Access != "public" {
			continue
		}
		if globalSkip[im.Name] || strings.HasPrefix(im.Name, "operator") || strings.HasPrefix(im.Name, "~") {
			continue
		}
		ov, used := m.Overlays.overlayFor(c.Name, im)
		for _, u := range used {
			m.usedOver[c.Name+"::"+u] = true
		}
		if im.Deprecated || im.Variadic {
			g.skip(c, im, "deprecated or variadic")
			continue
		}
		if stringSlot(im) {
			g.skip(c, im, "string-based SLOT() connection; use the On* signal methods instead")
			continue
		}
		if ov.skipped() {
			g.skip(c, im, fmt.Sprintf("skipped by overlay (%v)", ov.Skip))
			continue
		}
		switch im.Kind {
		case "ctor":
			if im.Copy || im.Move {
				continue
			}
			if c.Opts.NoConstruct || c.Event || !c.HasDtor && c.Kind != KindObject {
				continue
			}
			if c.Abstract {
				// abstract classes are only constructible through their shim
				if c.Opts.Subclass {
					if mm := g.newMethod(c, im, ov, true, false); mm != nil {
						mm.CName = c.cName("new")
						c.ShimCtors = append(c.ShimCtors, mm)
					}
				}
				continue
			}
			if mm := g.newMethod(c, im, ov, true, false); mm != nil {
				ctors = append(ctors, mm)
			}
		case "signal":
			if mm := g.newSignal(c, im, ov); mm != nil {
				signals = append(signals, mm)
			}
			private := false
			for _, p := range im.Params {
				private = private || strings.HasSuffix(p.Type.Canonical, "QPrivateSignal")
			}
			if !private {
				if mm := g.newMethod(c, im, ov, false, false); mm != nil {
					mm.Emit = true
					emits = append(emits, mm)
				}
			}
		default:
			if !im.Static && im.Virtual && ancestorKeys[paramKey(im)] {
				continue // an override of a bound base method: the base binding dispatches virtually
			}
			if !im.Static && ancestorKeys[paramKey(im)] {
				continue
			}
			if mm := g.newMethod(c, im, ov, false, false); mm != nil {
				if im.Static {
					statics = append(statics, mm)
				} else {
					methods = append(methods, mm)
				}
			}
			if ov != nil && ov.Full != "" {
				if mm := g.newMethod(c, im, ov, false, true); mm != nil {
					mm.GoName = ov.Full
					if im.Static {
						statics = append(statics, mm)
					} else {
						methods = append(methods, mm)
					}
				}
			}
		}
	}
	if c.Kind == KindValue {
		// value classes keep their copy constructor internally only
	}
	c.Ctors = g.nameCtors(c, ctors)
	c.Methods = g.nameMethods(c, methods, "", nil)
	c.Statics = g.nameStatics(c, statics)
	c.Signals = g.nameMethods(c, signals, "On", c.Methods)
	c.Methods = append(c.Methods, g.nameMethods(c, emits, "Emit", append(append([]*Method{}, c.Methods...), c.Signals...))...)
}

func (g *Gen) newMethod(c *Class, im *irMethod, ov *overlay, ctor, full bool) *Method {
	mm := &Method{Class: c, Owner: c, IR: im, Overlay: ov, Sig: g.methodSig(c, im), Full: full}
	// Drop trailing default args. Constructors keep a defaulted parent.
	n := len(im.Params)
	if !full && !(ov != nil && ov.KeepDefs) {
		keep := 0
		for i, p := range im.Params {
			if !p.Default {
				keep = i + 1
			}
			if ctor && p.Default && (p.Name == "parent" || ruleFor(ov, p.Name, i) == "transfer-this") {
				keep = i + 1
			}
		}
		n = keep
	}
	names := paramNames(im)
	for i := 0; i < n; i++ {
		p := im.Params[i]
		t, err := g.resolve(p.Type, false)
		if err != nil {
			g.skip(c, im, err.Error())
			return nil
		}
		if t.k == kVoid {
			g.skip(c, im, "void parameter")
			return nil
		}
		pp := &Param{Name: p.Name, GoName: names[i], CName: fmt.Sprintf("a%d", i), T: t}
		g.applyArgRule(c, im, ov, pp, i, ctor)
		mm.Params = append(mm.Params, pp)
	}
	if ov != nil && !full {
		g.checkArgKeys(c, im, ov, names)
	}
	if !ctor {
		t, err := g.resolve(im.Return, true)
		if err != nil {
			g.skip(c, im, "return: "+err.Error())
			return nil
		}
		mm.Ret = t
		if ov != nil && ov.Error {
			if t.k != kPrim || t.prim.goT != "bool" {
				g.report.warn("%s: error flag on non-bool return", mm.Sig)
			} else {
				mm.Error = true
			}
		}
		if t.k == kObj || (t.k == kList && t.elem.k == kObj) {
			mm.RetRule = "borrowed"
			if ov != nil && ov.Return != "" {
				mm.RetRule = ov.Return
				mm.RetRuled = true
			}
		}
	}
	return mm
}

func ruleFor(ov *overlay, name string, idx int) string {
	if ov == nil {
		return ""
	}
	if r, ok := ov.Args[name]; ok && name != "" {
		return r
	}
	if r, ok := ov.Args[fmt.Sprint(idx)]; ok {
		return r
	}
	return ""
}

func (g *Gen) applyArgRule(c *Class, im *irMethod, ov *overlay, p *Param, i int, ctor bool) {
	if p.T.k != kObj && !(p.T.k == kList && p.T.elem.k == kObj) {
		return
	}
	r := ruleFor(ov, p.Name, i)
	if r == "" {
		r = ruleFor(ov, p.GoName, i)
	}
	if r != "" {
		p.Rule, p.Ruled = r, true
		switch r {
		case "transfer-to-cpp", "transfer-this", "transfer-to-go", "borrowed", "scoped":
		default:
			g.report.warn("%s: unknown rule %q", g.methodSig(c, im), r)
		}
		return
	}
	switch {
	case ctor && p.Name == "parent":
		p.Rule = "transfer-this"
	case !ctor && im.Name == "setParent" && p.Name == "parent":
		p.Rule = "transfer-this"
	default:
		p.Rule = "borrowed"
	}
}

func (g *Gen) newSignal(c *Class, im *irMethod, ov *overlay) *Method {
	mm := &Method{Class: c, Owner: c, IR: im, Overlay: ov, Sig: g.methodSig(c, im)}
	for i, p := range im.Params {
		if strings.HasSuffix(p.Type.Canonical, "QPrivateSignal") {
			continue
		}
		t, err := g.resolve(p.Type, true)
		if err != nil {
			g.skip(c, im, "signal: "+err.Error())
			return nil
		}
		pp := &Param{Name: p.Name, GoName: paramNames(im)[i], CName: fmt.Sprintf("a%d", i), T: t, Rule: "borrowed"}
		if r := ruleFor(ov, p.Name, i); r != "" {
			pp.Rule, pp.Ruled = r, true
		} else if t.k == kObj && (t.cls.Event || t.cls.Name == "QPainter") {
			pp.Rule = "scoped"
		}
		mm.Params = append(mm.Params, pp)
	}
	return mm
}

// overloadSig is the Go-visible parameter list used to spot exact duplicates.
func goSig(m *Method) string {
	var ps []string
	for _, p := range m.Params {
		ps = append(ps, fmt.Sprint(p.T.k, p.T.base))
	}
	return strings.Join(ps, ",")
}

var reservedMethods = map[string]bool{
	"Ptr": true, "Alive": true, "Delete": true, "Keep": true, "Unkeep": true, "Ownership": true,
	"Super": true, "String": true,
}

func (g *Gen) nameMethods(c *Class, ms []*Method, prefix string, taken []*Method) []*Method {
	used := map[string]bool{}
	for _, t := range taken {
		used[t.GoName] = true
	}
	for k := range reservedMethods {
		used[k] = true
	}
	for b := c; b != nil; b = b.Base {
		used["As"+b.Name] = true
	}
	for _, s := range c.Secondary {
		used["As"+s.Name] = true
	}
	seen := map[string]bool{}
	var out []*Method
	// named by overlay first
	for _, m := range ms {
		if m.Overlay != nil && m.Overlay.Name != "" && !m.Full {
			m.GoName = prefix + m.Overlay.Name // signals: name: X gives OnX and EmitX
			if used[m.GoName] {
				g.report.warn("%s: overlay name %s already used", m.Sig, m.GoName)
			}
			used[m.GoName] = true
		} else if m.Full {
			used[m.GoName] = true
		}
	}
	for _, m := range ms {
		key := m.IR.Name + "|" + goSig(m)
		if m.Emit {
			key = "emit|" + key
		}
		if !m.Full && seen[key] {
			g.skip(c, m.IR, "duplicate Go signature after dropping default arguments")
			continue
		}
		seen[key] = true
		if m.GoName == "" {
			base := prefix + exportName(m.IR.Name)
			name := base
			for n := 2; used[name]; n++ {
				name = fmt.Sprintf("%s%d", base, n)
				m.Numbered = true
			}
			m.GoName = name
			used[name] = true
		}
		m.CName = c.cName(m.IR.Name)
		out = append(out, m)
	}
	return out
}

func (c *Class) cName(n string) string {
	k := c.cNames[n]
	c.cNames[n] = k + 1
	if k == 0 {
		return fmt.Sprintf("qute_%s_%s", c.Name, n)
	}
	return fmt.Sprintf("qute_%s_%s_%d", c.Name, n, k)
}

func (g *Gen) nameStatics(c *Class, ms []*Method) []*Method {
	m := c.Module
	var out []*Method
	seen := map[string]bool{}
	for _, mm := range ms {
		key := mm.IR.Name + "|" + goSig(mm)
		if !mm.Full && seen[key] {
			g.skip(c, mm.IR, "duplicate Go signature after dropping default arguments")
			continue
		}
		seen[key] = true
		var name string
		if mm.Overlay != nil && mm.Overlay.Name != "" && !mm.Full {
			name = mm.Overlay.Name
			if !m.claim(name, mm.Sig) {
				g.report.warn("%s: overlay name %s already used", mm.Sig, name)
			}
		} else if mm.Full {
			name = c.Name + mm.GoName
			m.claim(name, mm.Sig)
		} else {
			base := c.Name + exportName(mm.IR.Name)
			name = base
			for n := 2; !m.claim(name, mm.Sig); n++ {
				name = fmt.Sprintf("%s%d", base, n)
				mm.Numbered = true
			}
		}
		mm.GoName = name
		mm.CName = c.cName(mm.IR.Name)
		out = append(out, mm)
	}
	return out
}

func (g *Gen) nameCtors(c *Class, ms []*Method) []*Method {
	m := c.Module
	var out []*Method
	seen := map[string]bool{}
	primary := "New" + c.Name
	// A constructor taking only the parent is the primary one; a zero-argument
	// constructor next to it is redundant (pass nil).
	hasParentOnly := false
	for _, mm := range ms {
		if len(mm.Params) == 1 && mm.Params[0].Rule == "transfer-this" && (mm.Overlay == nil || mm.Overlay.Name == "") {
			hasParentOnly = true
		}
	}
	if hasParentOnly {
		var keep []*Method
		for _, mm := range ms {
			if len(mm.Params) == 0 && (mm.Overlay == nil || mm.Overlay.Name == "") {
				g.skip(c, mm.IR, "redundant: call New"+c.Name+"(nil)")
				continue
			}
			keep = append(keep, mm)
		}
		sort.SliceStable(keep, func(i, j int) bool {
			pi := len(keep[i].Params) == 1 && keep[i].Params[0].Rule == "transfer-this"
			pj := len(keep[j].Params) == 1 && keep[j].Params[0].Rule == "transfer-this"
			return pi && !pj
		})
		ms = keep
	}
	for _, mm := range ms {
		key := goSig(mm)
		if seen[key] {
			g.skip(c, mm.IR, "duplicate Go signature after dropping default arguments")
			continue
		}
		seen[key] = true
		var name string
		switch {
		case mm.Overlay != nil && mm.Overlay.Name != "":
			name = mm.Overlay.Name
			if !m.claim(name, mm.Sig) {
				g.report.warn("%s: overlay name %s already used", mm.Sig, name)
			}
		default:
			onlyParent := len(mm.Params) == 0 || (len(mm.Params) == 1 && mm.Params[0].Rule == "transfer-this")
			if onlyParent && m.claim(primary, mm.Sig) {
				name = primary
				break
			}
			base := primary
			if len(mm.Params) > 0 && mm.Params[0].Name != "" {
				base = primary + "With" + exportName(mm.Params[0].Name)
			}
			name = base
			for n := 2; !m.claim(name, mm.Sig); n++ {
				name = fmt.Sprintf("%s%d", base, n)
				mm.Numbered = true
			}
		}
		mm.GoName = name
		mm.CName = c.cName("new")
		out = append(out, mm)
	}
	return out
}

// ---------------------------------------------------------------------------
// Shims: Go-overridable virtuals and destructor notices.

func (g *Gen) buildShim(c *Class) {
	c.Shim = (c.Opts.Subclass || c.Opts.NotifyDtor) && !c.Opts.NoConstruct
	if !c.Shim {
		return
	}
	if !c.Opts.Subclass {
		return
	}
	skipV := map[string]bool{}
	for _, s := range c.Opts.SkipVirtuals {
		skipV[s] = true
	}
	seen := map[string]bool{}
	var chain []*Class
	var walk func(x *Class)
	walk = func(x *Class) {
		chain = append(chain, x)
		if x.Base != nil {
			walk(x.Base)
		}
		for _, s := range x.Secondary {
			walk(s)
		}
	}
	walk(c)
	// Names declared in more-derived classes hide base overloads in C++.
	hidden := map[string]*Class{}
	pureMissing := false
	for _, x := range chain {
		for i := range x.IR.Methods {
			im := &x.IR.Methods[i]
			if im.Kind == "ctor" || im.Kind == "signal" {
				continue
			}
			key := paramKey(im)
			if im.Access == "private" {
				if im.Virtual {
					seen[key] = true // a private override: the base virtual is not ours to override
				}
				continue
			}
			if !im.Virtual {
				if im.Access == "protected" && !im.Static && x != c || (im.Access == "protected" && !im.Static) {
					if !seen["p:"+key] && !globalSkip[im.Name] && !strings.HasPrefix(im.Name, "operator") && !im.Deprecated {
						seen["p:"+key] = true
						if h, ok := hidden[im.Name]; ok && h != x {
							continue
						}
						if mm := g.newProtected(c, x, im); mm != nil {
							c.Protected = append(c.Protected, mm)
						}
					}
				}
				if _, ok := hidden[im.Name]; !ok {
					hidden[im.Name] = x
				}
				continue
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, ok := hidden[im.Name]; !ok {
				hidden[im.Name] = x
			}
			if im.Final || globalSkip[im.Name] || strings.HasPrefix(im.Name, "operator") || skipV[im.Name] || im.Deprecated {
				if im.Pure {
					pureMissing = true
				}
				continue
			}
			mm := g.newVirtual(c, x, im)
			if mm == nil {
				if im.Pure {
					pureMissing = true
				}
				continue
			}
			mm.VIndex = len(c.Virtuals)
			c.Virtuals = append(c.Virtuals, mm)
		}
	}
	if pureMissing {
		g.report.warn("%s: a pure virtual cannot be bound, so the class cannot be subclassed from Go", c.Name)
		c.Opts.Subclass = false
		c.Virtuals = nil
		c.Protected = nil
		c.Shim = c.Opts.NotifyDtor
		return
	}
	// name the Go override methods
	used := map[string]int{}
	for _, v := range c.Virtuals {
		n := exportName(v.IR.Name)
		used[n]++
		if used[n] > 1 {
			n = fmt.Sprintf("%s%d", n, used[n])
			v.Numbered = true
		}
		v.GoName = n
		v.CName = fmt.Sprintf("qute_%s_super_%d", c.Name, v.VIndex)
	}
	pused := map[string]int{}
	for _, v := range c.Protected {
		n := exportName(v.IR.Name)
		pused[n]++
		if pused[n] > 1 {
			n = fmt.Sprintf("%s%d", n, pused[n])
		}
		v.GoName = n
		v.CName = c.cName("prot_" + v.IR.Name)
	}
}

func (g *Gen) newVirtual(c, owner *Class, im *irMethod) *Method {
	mm := &Method{Class: c, Owner: owner, IR: im, Sig: g.methodSig(owner, im)}
	for i, p := range im.Params {
		t, err := g.resolve(p.Type, true)
		if err != nil {
			return nil
		}
		if t.k == kObj && t.ref {
			return nil
		}
		pp := &Param{Name: p.Name, GoName: paramNames(im)[i], CName: fmt.Sprintf("a%d", i), T: t, Rule: "borrowed"}
		if t.k == kObj && (t.cls.Event || t.cls.Name == "QPainter" || strings.HasPrefix(t.cls.Name, "QStyleOption")) {
			pp.Rule = "scoped"
		}
		mm.Params = append(mm.Params, pp)
	}
	t, err := g.resolve(im.Return, true)
	if err != nil || !virtRetSupported(t) {
		return nil
	}
	mm.Ret = t
	return mm
}

func (g *Gen) newProtected(c, owner *Class, im *irMethod) *Method {
	if im.Static {
		return nil
	}
	mm := g.newMethod(owner, im, nil, false, false)
	if mm == nil {
		return nil
	}
	mm.Class = c
	mm.Owner = owner
	return mm
}

func (m *Module) dependsOn(x *Module) bool {
	for _, d := range m.Deps {
		if d == x {
			return true
		}
	}
	return false
}

func hasCtor(c *irClass) bool {
	for _, m := range c.Methods {
		if m.Kind == "ctor" && !m.Copy && !m.Move {
			return true
		}
	}
	return false
}

// stringSlot reports methods that take a receiver plus a SLOT() string, which
// Go code cannot produce.
func stringSlot(m *irMethod) bool {
	for _, p := range m.Params {
		if !p.Default && (p.Name == "member" || p.Name == "slot" || p.Name == "amember") && strings.Contains(p.Type.Canonical, "char *") {
			return true
		}
	}
	return false
}

// checkArgKeys warns about overlay argument keys that match no parameter, so
// a rule keyed by a name the header leaves out can't be silently ignored.
func (g *Gen) checkArgKeys(c *Class, im *irMethod, ov *overlay, names []string) {
	for k := range ov.Args {
		ok := false
		for i, p := range im.Params {
			if k == p.Name && k != "" || k == names[i] || k == fmt.Sprint(i) {
				ok = true
			}
		}
		if !ok {
			g.report.warn("%s: overlay argument %q matches no parameter (use its index)", g.methodSig(c, im), k)
		}
	}
}
