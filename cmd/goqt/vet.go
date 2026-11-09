package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modPath = "github.com/nomad-software/qute/"

var qtPkgs = map[string]bool{
	modPath + "qtcore": true, modPath + "qtgui": true, modPath + "qtwidgets": true,
}

type finding struct {
	pos  token.Position
	rule string
	msg  string
}

// vet runs the goqt static checks:
//
//	owned-leak      an Owned object with signal connections is never deleted,
//	                parented or shown (its connections pin it forever)
//	delete-handler  Delete() inside a signal or event handler (use DeleteLater)
//	gui-goroutine   Qt calls inside a goroutine (use goqt.Post)
//	scoped-escape   an event or painter argument stored past its callback
func vet(args []string) int {
	all, err := vetFindings(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goqt vet:", err)
		return 2
	}
	for _, f := range all {
		fmt.Printf("%s: %s: %s\n", f.pos, f.rule, f.msg)
	}
	if len(all) > 0 {
		return 1
	}
	return 0
}

func vetFindings(args []string) ([]finding, error) {
	if len(args) == 0 {
		args = []string{"./..."}
	}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
		packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps, Tests: true}
	pkgs, err := packages.Load(cfg, args...)
	if err != nil {
		return nil, err
	}
	var all []finding
	seen := map[string]bool{}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			fmt.Fprintln(os.Stderr, e)
		}
		if qtPkgs[p.PkgPath] || strings.HasPrefix(p.PkgPath, modPath+"internal") {
			continue
		}
		v := &vetter{pkg: p}
		for _, f := range p.Syntax {
			v.file(f)
		}
		for _, f := range v.out {
			k := f.pos.String() + f.rule
			if !seen[k] {
				seen[k] = true
				all = append(all, f)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].pos.Filename != all[j].pos.Filename {
			return all[i].pos.Filename < all[j].pos.Filename
		}
		return all[i].pos.Line < all[j].pos.Line
	})
	return all, nil
}

type vetter struct {
	pkg *packages.Package
	out []finding
}

func (v *vetter) report(n ast.Node, rule, format string, a ...any) {
	v.out = append(v.out, finding{v.pkg.Fset.Position(n.Pos()), rule, fmt.Sprintf(format, a...)})
}

// qtMethod returns the selected method if it belongs to a generated Qt type.
func (v *vetter) qtMethod(call *ast.CallExpr) (*types.Func, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, ""
	}
	obj := v.pkg.TypesInfo.Uses[sel.Sel]
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || !qtPkgs[fn.Pkg().Path()] {
		return nil, ""
	}
	return fn, sel.Sel.Name
}

func isQtFunc(info *types.Info, call *ast.CallExpr) (*types.Func, bool) {
	var id *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return nil, false
	}
	fn, ok := info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return nil, false
	}
	return fn, qtPkgs[fn.Pkg().Path()]
}

func (v *vetter) file(f *ast.File) {
	v.checkGoroutines(f)
	v.checkHandlers(f)
	v.checkScoped(f)
	v.checkLeaks(f)
}

// gui-goroutine: Qt calls in `go` statements, except inside goqt.Post/PostSync.
func (v *vetter) checkGoroutines(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		g, ok := n.(*ast.GoStmt)
		if !ok {
			return true
		}
		var body ast.Node = g.Call
		if lit, ok := g.Call.Fun.(*ast.FuncLit); ok {
			body = lit.Body
		}
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if fn, _ := isQtFunc(v.pkg.TypesInfo, call); fn != nil && fn.Pkg().Path() == modPath+"goqt" {
				switch fn.Name() {
				case "Post", "PostSync", "Run", "Quit":
					return false // runs on the GUI thread
				}
			}
			if fn, ok := isQtFunc(v.pkg.TypesInfo, call); ok {
				v.report(call, "gui-goroutine", "%s called from a goroutine; Qt objects live on the GUI thread, wrap it in goqt.Post(func() { ... })", fn.Name())
				return false
			}
			return true
		})
		return false
	})
}

// handlerBodies yields closures passed to On* methods and bodies of methods
// that override a virtual on a type embedding a generated *Base.
func (v *vetter) handlerBodies(f *ast.File, visit func(body *ast.BlockStmt, params *ast.FieldList, what string)) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if fn, name := v.qtMethod(x); fn != nil && strings.HasPrefix(name, "On") {
				for _, a := range x.Args {
					if lit, ok := a.(*ast.FuncLit); ok {
						visit(lit.Body, lit.Type.Params, name+" handler")
					}
				}
			}
		case *ast.FuncDecl:
			if x.Recv != nil && x.Body != nil && v.embedsBase(x) && strings.HasSuffix(x.Name.Name, "Event") {
				visit(x.Body, x.Type.Params, x.Name.Name)
			}
		}
		return true
	})
}

func (v *vetter) embedsBase(fd *ast.FuncDecl) bool {
	if len(fd.Recv.List) == 0 {
		return false
	}
	t := v.pkg.TypesInfo.TypeOf(fd.Recv.List[0].Type)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Embedded() && strings.HasSuffix(f.Type().String(), "Base") && f.Pkg() != nil {
			if n, ok := f.Type().(*types.Named); ok && n.Obj().Pkg() != nil && qtPkgs[n.Obj().Pkg().Path()] {
				return true
			}
		}
	}
	return false
}

// delete-handler: Delete() inside a handler.
func (v *vetter) checkHandlers(f *ast.File) {
	v.handlerBodies(f, func(body *ast.BlockStmt, _ *ast.FieldList, what string) {
		ast.Inspect(body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok && lit.Body != body {
				return false // a nested closure may run later
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, name := v.qtMethod(call)
			if fn == nil || name != "Delete" {
				return true
			}
			recv := v.pkg.TypesInfo.TypeOf(call.Fun.(*ast.SelectorExpr).X)
			if recv != nil && strings.Contains(recv.String(), "QPainter") {
				return true // painters are created and ended inside paint handlers
			}
			v.report(call, "delete-handler", "Delete() inside %s; Qt may still be using the object, call DeleteLater() instead (rule 5)", what)
			return true
		})
	})
}

func isScopedType(t types.Type) bool {
	s := t.String()
	if !strings.HasPrefix(s, "*"+modPath) {
		return false
	}
	return strings.HasSuffix(s, "Event") || strings.HasSuffix(s, ".QPainter") || strings.Contains(s, ".QStyleOption")
}

// scoped-escape: an event/painter parameter assigned to anything but a local,
// appended, sent on a channel, or captured by a goroutine or closure.
func (v *vetter) checkScoped(f *ast.File) {
	v.handlerBodies(f, func(body *ast.BlockStmt, params *ast.FieldList, what string) {
		scoped := map[types.Object]bool{}
		if params == nil {
			return
		}
		for _, fld := range params.List {
			for _, nm := range fld.Names {
				if obj := v.pkg.TypesInfo.Defs[nm]; obj != nil && isScopedType(obj.Type()) {
					scoped[obj] = true
				}
			}
		}
		if len(scoped) == 0 {
			return
		}
		uses := func(e ast.Node) *ast.Ident {
			var hit *ast.Ident
			ast.Inspect(e, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && scoped[v.pkg.TypesInfo.Uses[id]] {
					hit = id
				}
				return hit == nil
			})
			return hit
		}
		bare := func(e ast.Expr) *ast.Ident {
			if id, ok := e.(*ast.Ident); ok && scoped[v.pkg.TypesInfo.Uses[id]] {
				return id
			}
			return nil
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, r := range x.Rhs {
					id := bare(r)
					if id == nil || i >= len(x.Lhs) {
						continue
					}
					if lid, ok := x.Lhs[i].(*ast.Ident); ok {
						if obj := v.pkg.TypesInfo.ObjectOf(lid); obj != nil && obj.Parent() != nil && body.Pos() <= obj.Pos() && obj.Pos() <= body.End() {
							continue // a local variable
						}
					}
					v.report(x, "scoped-escape", "%s argument %s is stored past its callback; it is Dead once %s returns, copy the values you need (rule 6)", what, id.Name, what)
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "append" {
					for _, a := range x.Args[1:] {
						if hit := bare(a); hit != nil {
							v.report(x, "scoped-escape", "%s argument %s is appended to a slice; it is Dead once %s returns (rule 6)", what, hit.Name, what)
						}
					}
				}
			case *ast.SendStmt:
				if hit := bare(x.Value); hit != nil {
					v.report(x, "scoped-escape", "%s argument %s is sent on a channel; it is Dead once %s returns (rule 6)", what, hit.Name, what)
				}
			case *ast.GoStmt:
				if hit := uses(x.Call); hit != nil {
					v.report(x, "scoped-escape", "%s argument %s is used by a goroutine; it is Dead once %s returns (rule 6)", what, hit.Name, what)
				}
				return false
			case *ast.FuncLit:
				if hit := uses(x.Body); hit != nil {
					v.report(x, "scoped-escape", "%s argument %s is captured by a closure that may run later (rule 6)", what, hit.Name)
				}
				return false
			}
			return true
		})
	})
}

// owned-leak: x := qtX.NewY(..., nil) with x.OnZ(...) and no Delete,
// SetParent, Show, or use of x as an argument/return/stored value.
func (v *vetter) checkLeaks(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			return true
		}
		type cand struct {
			id       *ast.Ident
			conns    int
			released bool
		}
		cands := map[types.Object]*cand{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Tok != token.DEFINE {
				return true
			}
			for i, r := range as.Rhs {
				call, ok := r.(*ast.CallExpr)
				if !ok || i >= len(as.Lhs) {
					continue
				}
				fn, isQt := isQtFunc(v.pkg.TypesInfo, call)
				if !isQt || !strings.HasPrefix(fn.Name(), "New") || strings.HasSuffix(fn.Name(), "With") {
					continue
				}
				owned := true
				for _, a := range call.Args {
					if id, ok := a.(*ast.Ident); !ok || id.Name != "nil" {
						if tv := v.pkg.TypesInfo.Types[a]; tv.Type != nil && types.IsInterface(tv.Type) {
							owned = false // a parent was passed
						}
					}
				}
				if id, ok := as.Lhs[i].(*ast.Ident); ok && owned {
					if obj := v.pkg.TypesInfo.Defs[id]; obj != nil {
						cands[obj] = &cand{id: id}
					}
				}
			}
			return true
		})
		if len(cands) == 0 {
			return true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						if c := cands[v.pkg.TypesInfo.Uses[id]]; c != nil {
							switch name := sel.Sel.Name; {
							case strings.HasPrefix(name, "On"):
								c.conns++
							case name == "Delete" || name == "DeleteLater" || name == "SetParent" || name == "Show" ||
								name == "Exec" || name == "Open" || name == "Keep" || strings.HasPrefix(name, "Show"):
								c.released = true
							}
						}
					}
				}
				for _, a := range x.Args {
					ast.Inspect(a, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok {
							if c := cands[v.pkg.TypesInfo.Uses[id]]; c != nil {
								c.released = true // handed to something that may own it
							}
						}
						return true
					})
				}
			case *ast.ReturnStmt:
				for _, r := range x.Results {
					if id, ok := r.(*ast.Ident); ok {
						if c := cands[v.pkg.TypesInfo.Uses[id]]; c != nil {
							c.released = true
						}
					}
				}
			case *ast.AssignStmt:
				for _, r := range x.Rhs {
					if id, ok := r.(*ast.Ident); ok {
						if c := cands[v.pkg.TypesInfo.Uses[id]]; c != nil {
							c.released = true
						}
					}
				}
			case *ast.CompositeLit:
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						e = kv.Value
					}
					if id, ok := e.(*ast.Ident); ok {
						if c := cands[v.pkg.TypesInfo.Uses[id]]; c != nil {
							c.released = true
						}
					}
				}
			}
			return true
		})
		for _, c := range cands {
			if c.conns > 0 && !c.released {
				v.report(c.id, "owned-leak", "%s is Owned and has signal connections, which pin it, but it is never deleted, parented or shown; call %s.Delete() when done or give it a parent", c.id.Name, c.id.Name)
			}
		}
		return true
	})
}
