package main

import (
	"fmt"
	"strings"
)

// conv describes how one C++ type crosses the boundary. Every kind knows six
// conversions: Go arg -> C, C -> C++ arg, C++ result -> C, C -> Go result,
// and for virtual returns Go -> C-owned C and C-owned C -> C++.

type tkind int

const (
	kVoid tkind = iota
	kPrim
	kEnum
	kFlags
	kString
	kBytes
	kStrList
	kList
	kStruct
	kValue
	kObj
	kCStr
	kChar
)

type primDef struct {
	c    string // C type in the header
	cgo  string // cgo name (C.int)
	goT  string // Go type
	cast string // C++ type for static_cast
}

var prims = map[string]primDef{
	"bool":               {"bool", "C.bool", "bool", "bool"},
	"int":                {"int", "C.int", "int", "int"},
	"unsigned int":       {"unsigned int", "C.uint", "uint", "unsigned int"},
	"short":              {"short", "C.short", "int16", "short"},
	"unsigned short":     {"unsigned short", "C.ushort", "uint16", "unsigned short"},
	"long":               {"long", "C.long", "int64", "long"},
	"unsigned long":      {"unsigned long", "C.ulong", "uint64", "unsigned long"},
	"long long":          {"long long", "C.longlong", "int64", "long long"},
	"unsigned long long": {"unsigned long long", "C.ulonglong", "uint64", "unsigned long long"},
	"signed char":        {"signed char", "C.schar", "int8", "signed char"},
	"unsigned char":      {"unsigned char", "C.uchar", "uint8", "unsigned char"},
	"char":               {"char", "C.char", "byte", "char"},
	"float":              {"float", "C.float", "float32", "float"},
	"double":             {"double", "C.double", "float64", "double"},
}

// qsizetype is "long long" canonically but reads better as Go int.
var qsizetypePrim = primDef{"long long", "C.longlong", "int", "long long"}

type structField struct {
	goName, cName, goType, cgo string
}

type structDef struct {
	name   string
	module *Module
	cType  string
	fields []structField
}

type conv struct {
	k      tkind
	cpp    string // canonical C++ type as written in the IR
	base   string // without const, &, *
	ptr    bool
	ref    bool
	cnst   bool
	prim   primDef
	enum   *Enum
	st     *structDef
	cls    *Class
	elem   *conv
	wrap   string // QPolygon: a QList subclass built from the list
	reason string
}

func (g *Gen) unsupported(t irType, why string) (*conv, error) {
	return nil, fmt.Errorf("unsupported type %q (%s)", t.Canonical, why)
}

func parseCpp(t string) (base string, cnst, ptr, ref, rref bool) {
	s := strings.TrimSpace(t)
	if strings.HasSuffix(s, "&&") {
		rref = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "&&"))
	} else if strings.HasSuffix(s, "&") {
		ref = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "&"))
	}
	if strings.HasSuffix(s, "*") {
		ptr = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "*"))
	}
	if strings.HasSuffix(s, " const") {
		cnst = true
		s = strings.TrimSpace(strings.TrimSuffix(s, " const"))
	}
	if strings.HasPrefix(s, "const ") {
		cnst = true
		s = strings.TrimSpace(strings.TrimPrefix(s, "const "))
	}
	return s, cnst, ptr, ref, rref
}

// resolve maps an IR type to a conversion. ret says the type is a return
// value (or a signal/virtual argument travelling C++ -> Go).
func (g *Gen) resolve(t irType, ret bool) (*conv, error) {
	can := t.Canonical
	base, cnst, ptr, ref, rref := parseCpp(can)
	if rref {
		return g.unsupported(t, "rvalue reference")
	}
	c := &conv{cpp: can, base: base, ptr: ptr, ref: ref, cnst: cnst}
	if base == "void" && !ptr {
		c.k = kVoid
		return c, nil
	}
	if p, ok := prims[base]; ok {
		if ptr && base == "char" && cnst && !ref {
			c.k = kCStr
			return c, nil
		}
		if ptr || (ref && !cnst) {
			return g.unsupported(t, "pointer or out-parameter to primitive")
		}
		if base == "long long" && strings.Contains(t.Spelling, "qsizetype") {
			p = qsizetypePrim
		}
		c.k = kPrim
		c.prim = p
		return c, nil
	}
	if ptr || (ref && !cnst) {
		// only object pointers (and object references) from here on
		if cls := g.classes[base]; cls != nil && cls.Kind != KindValue && !ref {
			c.k = kObj
			c.cls = cls
			return c, nil
		}
		if cls := g.classes[base]; cls != nil && cls.Kind != KindValue && ref && !ret {
			c.k = kObj
			c.cls = cls
			return c, nil
		}
		return g.unsupported(t, "pointer or out-parameter")
	}
	switch base {
	case "QString":
		c.k = kString
		return c, nil
	case "QStringView", "QAnyStringView":
		if ret {
			return g.unsupported(t, "string view result")
		}
		c.k = kString // a temporary QString converts implicitly for the call
		return c, nil
	case "QPolygon", "QPolygonF":
		el := "QPoint"
		if base == "QPolygonF" {
			el = "QPointF"
		}
		if st := g.structs[el]; st != nil {
			c.k = kList
			c.elem = &conv{k: kStruct, cpp: el, base: el, st: st}
			c.wrap = base
			return c, nil
		}
	case "QByteArray":
		c.k = kBytes
		return c, nil
	case "QList<QString>":
		c.k = kStrList
		return c, nil
	case "QChar":
		c.k = kChar
		return c, nil
	}
	if strings.HasPrefix(base, "QFlags<") && strings.HasSuffix(base, ">") {
		en := g.enums[base[len("QFlags<"):len(base)-1]]
		if en == nil || !en.emitted {
			return g.unsupported(t, "flags of unbound enum")
		}
		c.k = kFlags
		c.enum = en
		return c, nil
	}
	if en := g.enums[base]; en != nil {
		if !en.emitted {
			return g.unsupported(t, "unbound enum")
		}
		c.k = kEnum
		c.enum = en
		return c, nil
	}
	if st := g.structs[base]; st != nil {
		c.k = kStruct
		c.st = st
		return c, nil
	}
	if strings.HasPrefix(base, "QList<") && strings.HasSuffix(base, ">") {
		inner := base[len("QList<") : len(base)-1]
		e, err := g.resolve(irType{Spelling: inner, Canonical: inner}, ret)
		if err != nil {
			return nil, err
		}
		switch e.k {
		case kPrim, kEnum, kFlags, kStruct, kObj, kValue, kString:
		default:
			return g.unsupported(t, "list element")
		}
		if e.k == kObj && e.cls.Kind == KindValue {
			return g.unsupported(t, "list element")
		}
		c.k = kList
		c.elem = e
		return c, nil
	}
	if cls := g.classes[base]; cls != nil {
		if cls.Kind == KindValue {
			c.k = kValue
			c.cls = cls
			return c, nil
		}
		if ref && cnst && !ret {
			c.k = kObj
			c.cls = cls
			return c, nil
		}
		return g.unsupported(t, "object class by value")
	}
	return g.unsupported(t, "unknown")
}

// ---- Go side ----------------------------------------------------------------

func (g *Gen) goType(c *conv, ctx *Module, param bool) string {
	switch c.k {
	case kVoid:
		return ""
	case kPrim:
		return c.prim.goT
	case kEnum, kFlags:
		return g.qual(c.enum.module, ctx, c.enum.goTypeFor(c.k == kFlags))
	case kString, kCStr:
		return "string"
	case kBytes:
		return "[]byte"
	case kStrList:
		return "[]string"
	case kChar:
		return "rune"
	case kStruct:
		return g.qual(c.st.module, ctx, c.st.name)
	case kValue:
		return "*" + g.qual(c.cls.Module, ctx, c.cls.Name)
	case kObj:
		if param {
			return g.qual(c.cls.Module, ctx, c.cls.Name+"Like")
		}
		return "*" + g.qual(c.cls.Module, ctx, c.cls.Name)
	case kList:
		if c.elem.k == kObj {
			return "[]*" + g.qual(c.elem.cls.Module, ctx, c.elem.cls.Name)
		}
		return "[]" + g.goType(c.elem, ctx, false)
	}
	panic("goType")
}

func (g *Gen) qual(m, ctx *Module, name string) string {
	if m == ctx {
		return name
	}
	ctx.imports[m.ImportPath] = true
	return m.Pkg + "." + name
}

// cgoType is the cgo spelling of the C representation.
func (g *Gen) cgoType(c *conv) string {
	switch c.k {
	case kPrim:
		return c.prim.cgo
	case kEnum, kFlags:
		return "C.int64_t"
	case kString, kBytes:
		return "C.qute_str"
	case kStrList, kList:
		return "C.qute_list"
	case kChar:
		return "C.uint"
	case kStruct:
		return "C." + c.st.cType
	case kValue, kObj:
		return "unsafe.Pointer"
	case kCStr:
		return "*C.char"
	}
	panic("cgoType")
}

// cType is the C spelling used in headers for a single-value representation.
func cType(c *conv) string {
	switch c.k {
	case kPrim:
		return c.prim.c
	case kEnum, kFlags:
		return "int64_t"
	case kString, kBytes:
		return "qute_str"
	case kStrList, kList:
		return "qute_list"
	case kChar:
		return "unsigned int"
	case kStruct:
		return c.st.cType
	case kValue, kObj:
		return "void *"
	case kCStr:
		return "const char *"
	case kVoid:
		return "void"
	}
	panic("cType")
}

// cParams returns the C parameter declarations for an argument.
func cParams(c *conv, n string) []string {
	switch c.k {
	case kString, kBytes:
		return []string{"const char *" + n, "ptrdiff_t " + n + "_len"}
	case kStrList:
		return []string{"const char *" + n, "const ptrdiff_t *" + n + "_lens", "ptrdiff_t " + n + "_n"}
	case kList:
		return []string{"const void *" + n, "ptrdiff_t " + n + "_n"}
	}
	return []string{cType(c) + " " + n}
}

// goArg converts Go value v for the C call. It returns statements to run
// before the call and the C argument expressions.
func (g *Gen) goArg(c *conv, v, method string, ctx *Module) (pre []string, args []string) {
	switch c.k {
	case kPrim:
		return nil, []string{fmt.Sprintf("%s(%s)", c.prim.cgo, v)}
	case kEnum, kFlags:
		return nil, []string{fmt.Sprintf("C.int64_t(%s)", v)}
	case kString:
		return nil, []string{fmt.Sprintf("(*C.char)(unsafe.Pointer(unsafe.StringData(%s)))", v), fmt.Sprintf("C.ptrdiff_t(len(%s))", v)}
	case kBytes:
		return nil, []string{fmt.Sprintf("(*C.char)(unsafe.Pointer(unsafe.SliceData(%s)))", v), fmt.Sprintf("C.ptrdiff_t(len(%s))", v)}
	case kStrList:
		d, l := v+"Data", v+"Lens"
		return []string{fmt.Sprintf("%s, %s := quteStrListArg(%s)", d, l, v)},
			[]string{fmt.Sprintf("(*C.char)(unsafe.Pointer(unsafe.SliceData(%s)))", d), fmt.Sprintf("(*C.ptrdiff_t)(unsafe.SliceData(%s))", l), fmt.Sprintf("C.ptrdiff_t(len(%s))", v)}
	case kList:
		tmp := v + "C"
		et := g.cgoType(c.elem)
		var conv string
		switch c.elem.k {
		case kObj, kValue:
			conv = fmt.Sprintf("rt.UseArg(unsafe.Pointer(%s[i]), %q)", v, method)
		case kString:
			conv = fmt.Sprintf("quteCStr(%s[i])", v)
		default:
			_, a := g.goArg(c.elem, v+"[i]", method, ctx)
			conv = a[0]
		}
		pre = []string{
			fmt.Sprintf("%s := make([]%s, len(%s))", tmp, et, v),
			fmt.Sprintf("for i := range %s {\n\t\t%s[i] = %s\n\t}", v, tmp, conv),
		}
		if c.elem.k == kString {
			pre = append(pre, fmt.Sprintf("defer quteFreeStrs(%s)", tmp))
		}
		return pre, []string{fmt.Sprintf("unsafe.Pointer(unsafe.SliceData(%s))", tmp), fmt.Sprintf("C.ptrdiff_t(len(%s))", v)}
	case kChar:
		return nil, []string{fmt.Sprintf("C.uint(%s)", v)}
	case kStruct:
		return nil, []string{fmt.Sprintf("c%s(%s)", c.st.name, v)}
	case kValue:
		if !c.cls.DefCtor {
			return nil, []string{fmt.Sprintf("rt.Use(unsafe.Pointer(%s), %q)", v, method)}
		}
		return nil, []string{fmt.Sprintf("rt.UseArg(unsafe.Pointer(%s), %q)", v, method)}
	case kObj:
		p := v + "Ptr"
		return []string{
			fmt.Sprintf("var %s unsafe.Pointer", p),
			fmt.Sprintf("if %s != nil {\n\t\t%s = rt.UseArg(unsafe.Pointer(%s.As%s()), %q)\n\t}", v, p, v, c.cls.Name, method),
		}, []string{p}
	case kCStr:
		p := v + "CStr"
		return []string{fmt.Sprintf("%s := C.CString(%s)", p, v), fmt.Sprintf("defer C.free(unsafe.Pointer(%s))", p)}, []string{p}
	}
	panic("goArg")
}

// goRet converts a C result expression r to Go. mode is the rt wrap mode for
// object pointers.
func (g *Gen) goRet(c *conv, r string, mode string, ctx *Module) string {
	switch c.k {
	case kPrim:
		return fmt.Sprintf("%s(%s)", c.prim.goT, r)
	case kEnum, kFlags:
		return fmt.Sprintf("%s(%s)", g.goType(c, ctx, false), r)
	case kString:
		return fmt.Sprintf("quteStr(%s)", r)
	case kBytes:
		return fmt.Sprintf("quteBytes(%s)", r)
	case kStrList:
		return fmt.Sprintf("quteStrList(%s)", r)
	case kChar:
		return fmt.Sprintf("rune(%s)", r)
	case kCStr:
		return fmt.Sprintf("C.GoString(%s)", r)
	case kStruct:
		return fmt.Sprintf("go%s(%s)", c.st.name, r)
	case kValue:
		return g.wrapExpr(c.cls, r, "rt.TakeOwned", ctx)
	case kObj:
		return g.wrapExpr(c.cls, r, mode, ctx)
	case kList:
		et := g.cgoType(c.elem)
		gt := g.goType(c.elem, ctx, false)
		inner := g.goRet(c.elem, "e", mode, ctx)
		if c.elem.k == kValue {
			inner = g.wrapExpr(c.elem.cls, "e", "rt.TakeOwned", ctx)
		}
		return fmt.Sprintf("quteList(%s, func(e %s) %s { return %s })", r, et, gt, inner)
	}
	panic("goRet")
}

func (g *Gen) wrapExpr(cls *Class, p, mode string, ctx *Module) string {
	return fmt.Sprintf("rt.View[%s](rt.WrapClass(%s, %q, %s), %s)", g.qual(cls.Module, ctx, cls.Name), p, cls.Name, mode, p)
}

// goVirtRet converts a Go value returned by a virtual override into C memory
// the shim takes ownership of.
func (g *Gen) goVirtRet(c *conv, v, method string, ctx *Module) (string, bool) {
	switch c.k {
	case kString:
		return fmt.Sprintf("quteCStr(%s)", v), true
	case kBytes:
		return fmt.Sprintf("quteCBytes(%s)", v), true
	case kStrList:
		return fmt.Sprintf("quteCStrList(%s)", v), true
	case kPrim, kEnum, kFlags, kChar, kStruct, kValue:
		_, a := g.goArg(c, v, method, ctx)
		return a[0], true
	case kObj:
		return fmt.Sprintf("rt.UseArg(unsafe.Pointer(%s), %q)", v, method), true
	}
	return "", false
}

// ---- C++ side ---------------------------------------------------------------

// cppArg builds the Qt argument from the C parameters named n.
func (g *Gen) cppArg(c *conv, n string) string {
	switch c.k {
	case kPrim:
		return fmt.Sprintf("static_cast<%s>(%s)", c.prim.cast, n)
	case kEnum:
		return fmt.Sprintf("static_cast<%s>(%s)", c.enum.cpp, n)
	case kFlags:
		return fmt.Sprintf("QFlags<%s>::fromInt(static_cast<QFlags<%s>::Int>(%s))", c.enum.cpp, c.enum.cpp, n)
	case kString:
		return fmt.Sprintf("QString::fromUtf8(%s, %s_len)", n, n)
	case kBytes:
		return fmt.Sprintf("QByteArray(%s, %s_len)", n, n)
	case kStrList:
		return fmt.Sprintf("qute_qstrlist(%s, %s_lens, %s_n)", n, n, n)
	case kChar:
		return fmt.Sprintf("QChar(static_cast<char32_t>(%s))", n)
	case kStruct:
		return fmt.Sprintf("qute_to_%s(%s)", c.st.name, n)
	case kValue:
		if !c.cls.DefCtor {
			return fmt.Sprintf("*static_cast<%s *>(%s)", c.cls.Name, n)
		}
		return fmt.Sprintf("(%s ? *static_cast<%s *>(%s) : %s())", n, c.cls.Name, n, c.cls.Name)
	case kObj:
		if c.ref {
			return fmt.Sprintf("*static_cast<%s *>(%s)", c.cls.Name, n)
		}
		return fmt.Sprintf("static_cast<%s *>(%s)", c.cls.Name, n)
	case kCStr:
		return n
	case kList:
		var dt, el string
		switch c.elem.k {
		case kObj, kValue:
			dt, el = "void *", g.cppArg(c.elem, "d[i]")
		case kString:
			dt, el = "qute_str", "QString::fromUtf8(d[i].data, d[i].len)"
		default:
			dt, el = cType(c.elem), g.cppArg(c.elem, "d[i]")
		}
		ex := fmt.Sprintf("[&] { QList<%s> l; auto d = static_cast<%s const *>(%s); l.reserve(%s_n); for (ptrdiff_t i = 0; i < %s_n; i++) l.append(%s); return l; }()",
			c.elem.cppBase(), dt, n, n, n, el)
		if c.wrap != "" {
			ex = c.wrap + "(" + ex + ")"
		}
		return ex
	}
	panic("cppArg")
}

func (c *conv) cppBase() string {
	switch c.k {
	case kObj:
		return c.cls.Name + " *"
	case kEnum:
		return c.enum.cpp
	case kFlags:
		return "QFlags<" + c.enum.cpp + ">"
	case kString:
		return "QString"
	}
	return c.base
}

// cppRet converts a Qt result expression e to its C representation.
func (g *Gen) cppRet(c *conv, e string) string {
	switch c.k {
	case kPrim:
		return fmt.Sprintf("static_cast<%s>(%s)", c.prim.c, e)
	case kEnum:
		return fmt.Sprintf("static_cast<int64_t>(%s)", e)
	case kFlags:
		return fmt.Sprintf("static_cast<int64_t>((%s).toInt())", e)
	case kString:
		return fmt.Sprintf("qute_ret_str(%s)", e)
	case kBytes:
		return fmt.Sprintf("qute_ret_bytes(%s)", e)
	case kStrList:
		return fmt.Sprintf("qute_ret_strlist(%s)", e)
	case kChar:
		return fmt.Sprintf("static_cast<unsigned int>((%s).unicode())", e)
	case kStruct:
		return fmt.Sprintf("qute_from_%s(%s)", c.st.name, e)
	case kValue:
		return fmt.Sprintf("static_cast<void *>(new %s(%s))", c.cls.Name, e)
	case kObj:
		if c.ref {
			return fmt.Sprintf("(void *)(&(%s))", e)
		}
		return fmt.Sprintf("(void *)(%s)", e)
	case kCStr:
		return e
	case kList:
		ec := cType(c.elem)
		return fmt.Sprintf("qute_ret_list<%s>(%s, [](const auto &v) { return %s; })", ec, e, g.cppRet(c.elem, "v"))
	}
	panic("cppRet")
}

// cppVirtRet converts the C value the Go override wrote into the Qt return.
func (g *Gen) cppVirtRet(c *conv, n string) string {
	switch c.k {
	case kString:
		return fmt.Sprintf("qute_take_qstr(%s)", n)
	case kBytes:
		return fmt.Sprintf("qute_take_qbytes(%s)", n)
	case kStrList:
		return fmt.Sprintf("qute_take_qstrlist(%s)", n)
	case kObj:
		return fmt.Sprintf("static_cast<%s *>(%s)", c.cls.Name, n)
	}
	return g.cppArg(c, n)
}

// virtRetSupported reports whether a type can be returned from a Go override.
func virtRetSupported(c *conv) bool {
	switch c.k {
	case kVoid, kPrim, kEnum, kFlags, kChar, kStruct, kValue, kObj, kString, kBytes, kStrList:
		return !(c.k == kObj && c.ref)
	}
	return false
}

// cppDefault is the value a pure virtual returns when Go does not override it.
func cppDefault(c *conv) string {
	return "{}"
}
