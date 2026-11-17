package domainscore

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// GoSurfaceMax is the part of the contracts item version 4 measures: the
// five points the review took off for a Go surface other domains cannot
// read. The other fifteen are not measured (the ruler's "parts").
const GoSurfaceMax = 5

// ContractUse is how the production code of other domains uses one domain's
// Go packages: the review's three counts (section 3), taken the way its tool
// took them, from the uses the type checker records.
type ContractUse struct {
	// InterfaceCalls are uses of a method of an interface type the domain
	// declares; ConcreteCalls of a method of one of its other types;
	// FunctionCalls of one of its package-level functions; FieldAccesses of
	// a field of one of its structs (selectors and keys of struct literals).
	InterfaceCalls, ConcreteCalls, FunctionCalls, FieldAccesses int
	// ExportedTotal are the domain's exported package-level names and
	// exported methods of exported types; ExportedUsedByOthers those among
	// them another domain's production code names at least once.
	ExportedTotal, ExportedUsedByOthers int
	// Held are the concrete types other domains hold on to, heaviest first.
	Held []Held
}

// Held is one of a domain's types and one package of another domain that
// calls its methods or reaches into its fields.
type Held struct {
	Type, User    string
	Calls, Fields int
}

// Surface counts, package by package, what the domains use of each other.
// It is given type-checked packages; it runs no tool and reads no file.
type Surface struct {
	graph  *Graph
	module string
	use    map[string]*ContractUse
	held   map[[3]string]*Held
	// exported is every exported name by its key, with its domain; used
	// are the keys another domain names.
	exported map[string]string
	used     map[string]bool
	owners   map[*types.Package]map[*types.Var]string
}

// NewSurface counts over the packages of g. module is the import path of
// the repository root with a trailing slash, so that module+"api/internal/x"
// is the import path of the package the ownership table calls
// "api/internal/x".
func NewSurface(g *Graph, module string) *Surface {
	return &Surface{graph: g, module: module, use: map[string]*ContractUse{}, held: map[[3]string]*Held{},
		exported: map[string]string{}, used: map[string]bool{}, owners: map[*types.Package]map[*types.Var]string{}}
}

func (s *Surface) of(domain string) *ContractUse {
	if s.use[domain] == nil {
		s.use[domain] = &ContractUse{}
	}
	return s.use[domain]
}

// namedOf is the defined type behind a receiver, through a pointer.
func namedOf(t types.Type) *types.Named {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := types.Unalias(t).(*types.Named)
	return n
}

// key names a package-level object or a method: "path.Name" or
// "path.Type.Method". Fields and local objects have none.
func key(o types.Object) string {
	if o.Pkg() == nil {
		return ""
	}
	if f, ok := o.(*types.Func); ok {
		if recv := f.Type().(*types.Signature).Recv(); recv != nil {
			if n := namedOf(recv.Type()); n != nil {
				return o.Pkg().Path() + "." + n.Obj().Name() + "." + f.Name()
			}
			return ""
		}
	}
	if v, ok := o.(*types.Var); ok && v.IsField() {
		return ""
	}
	if o.Parent() != o.Pkg().Scope() {
		return ""
	}
	return o.Pkg().Path() + "." + o.Name()
}

// ownerOf is the name of the package-level type a field is declared in,
// through structs nested in it; "" for a field of a type with no name.
func (s *Surface) ownerOf(field *types.Var) string {
	pkg := field.Pkg()
	if s.owners[pkg] == nil {
		m := map[*types.Var]string{}
		var walk func(name string, t types.Type, depth int)
		walk = func(name string, t types.Type, depth int) {
			if depth > 6 {
				return
			}
			switch t := t.(type) {
			case *types.Struct:
				for i := 0; i < t.NumFields(); i++ {
					if _, seen := m[t.Field(i)]; !seen {
						m[t.Field(i)] = name
						walk(name, t.Field(i).Type(), depth+1)
					}
				}
			case *types.Pointer:
				walk(name, t.Elem(), depth+1)
			case *types.Slice:
				walk(name, t.Elem(), depth+1)
			case *types.Array:
				walk(name, t.Elem(), depth+1)
			case *types.Map:
				walk(name, t.Elem(), depth+1)
			}
		}
		scope := pkg.Scope()
		for _, name := range scope.Names() {
			if tn, ok := scope.Lookup(name).(*types.TypeName); ok && !tn.IsAlias() {
				walk(name, tn.Type().Underlying(), 0)
			}
		}
		s.owners[pkg] = m
	}
	return s.owners[pkg][field.Origin()]
}

// Add counts one type-checked package: pkg is its path from the repository
// root, info what the type checker filled for it (Defs and Uses) and fset
// where its files are. A package outside the graph (the assembly roots under api/cmd/) is
// neither a user nor a provider, and a _test.go file is not read.
func (s *Surface) Add(pkg string, fset *token.FileSet, info *types.Info) {
	from, ok := s.graph.DomainOf(pkg)
	if !ok {
		return
	}
	production := func(pos token.Pos) bool { return !strings.HasSuffix(fset.Position(pos).Filename, "_test.go") }
	for id, o := range info.Defs {
		if o == nil || !id.IsExported() || !production(id.Pos()) {
			continue
		}
		k := key(o)
		if k == "" {
			continue
		}
		if f, ok := o.(*types.Func); ok {
			// A method of a type other packages cannot name is not surface.
			if recv := f.Type().(*types.Signature).Recv(); recv != nil && !namedOf(recv.Type()).Obj().Exported() {
				continue
			}
		}
		s.exported[k] = from
	}
	for id, o := range info.Uses {
		if o == nil || o.Pkg() == nil || !production(id.Pos()) || !strings.HasPrefix(o.Pkg().Path(), s.module) {
			continue
		}
		provider := strings.TrimPrefix(o.Pkg().Path(), s.module)
		to, ok := s.graph.DomainOf(provider)
		if !ok || to == from {
			continue
		}
		u := s.of(to)
		hold := func(typ string, calls, fields int) {
			if typ == "" {
				typ = "(a type with no name)"
			}
			k := [3]string{to, provider + "." + typ, pkg}
			if s.held[k] == nil {
				s.held[k] = &Held{Type: k[1], User: pkg}
			}
			s.held[k].Calls += calls
			s.held[k].Fields += fields
		}
		switch o := o.(type) {
		case *types.Var:
			if o.IsField() {
				u.FieldAccesses++
				hold(s.ownerOf(o), 0, 1)
			}
		case *types.Func:
			recv := o.Type().(*types.Signature).Recv()
			switch {
			case recv == nil:
				if o.Parent() == o.Pkg().Scope() {
					u.FunctionCalls++
				}
			case types.IsInterface(recv.Type()):
				u.InterfaceCalls++
			default:
				u.ConcreteCalls++
				name := ""
				if n := namedOf(recv.Type()); n != nil {
					name = n.Obj().Name()
				}
				hold(name, 1, 0)
			}
		}
		if k := key(originOf(o)); k != "" {
			s.used[to+"\x00"+k] = true
		}
	}
}

// originOf is the declared object behind a method or field of an
// instantiated generic type.
func originOf(o types.Object) types.Object {
	switch o := o.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	}
	return o
}

// Uses is the count for every domain something was counted for, after every
// package has been added.
func (s *Surface) Uses() map[string]ContractUse {
	out := map[string]ContractUse{}
	for k, d := range s.exported {
		u := s.of(d)
		u.ExportedTotal++
		if s.used[d+"\x00"+k] {
			u.ExportedUsedByOthers++
		}
	}
	for k, h := range s.held {
		s.of(k[0]).Held = append(s.of(k[0]).Held, *h)
	}
	for d, u := range s.use {
		sort.Slice(u.Held, func(i, j int) bool {
			a, b := u.Held[i], u.Held[j]
			if a.Calls+a.Fields != b.Calls+b.Fields {
				return a.Calls+a.Fields > b.Calls+b.Fields
			}
			return a.Type+a.User < b.Type+b.User
		})
		out[d] = *u
	}
	// A second call starts from the same counts.
	for _, u := range s.use {
		u.ExportedTotal, u.ExportedUsedByOthers, u.Held = 0, 0, nil
	}
	return out
}

var routeMethods = map[string]bool{"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true, "Head": true, "Options": true,
	"Handle": true, "HandleFunc": true, "Method": true, "MethodFunc": true}

// CountRoutes counts the routes one api/internal/server/mount_<domain>.go
// registers: calls of a router's Get, Post, Put, Patch, Delete, Head,
// Options, Handle, HandleFunc, Method or MethodFunc whose path argument is a
// string that starts with "/". It is a count of lines of a list, not a
// check that the list is complete.
func CountRoutes(src []byte) (int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mount.go", src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !routeMethods[sel.Sel.Name] {
			return true
		}
		at := 0
		if strings.HasPrefix(sel.Sel.Name, "Method") {
			at = 1
		}
		if len(call.Args) <= at {
			return true
		}
		if lit, ok := call.Args[at].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if path, err := strconv.Unquote(lit.Value); err == nil && strings.HasPrefix(path, "/") {
				n++
			}
		}
		return true
	})
	return n, nil
}

// contracts fills the contracts item for one domain: the Go surface part,
// GoSurfaceMax points, by the ruler's formula. users are the packages of
// other domains that import one of the domain's.
func contracts(item *Item, u ContractUse, users int, routes map[string]int, domain string) {
	item.Measured = true
	item.MeasuredMax = GoSurfaceMax
	methods := u.InterfaceCalls + u.ConcreteCalls
	share := 1.0
	if methods > 0 {
		share = float64(u.InterfaceCalls) / float64(methods)
	}
	item.Score = round1(GoSurfaceMax * share)
	item.Measures = map[string]float64{
		"interface_calls": float64(u.InterfaceCalls), "concrete_calls": float64(u.ConcreteCalls),
		"function_calls": float64(u.FunctionCalls), "field_accesses": float64(u.FieldAccesses),
		"interface_share": float64(int(share*1000+0.5)) / 1000, "users": float64(users),
		"exported_total": float64(u.ExportedTotal), "exported_used_by_others": float64(u.ExportedUsedByOthers),
	}
	if n, ok := routes[domain]; ok {
		item.Measures["http_routes"] = float64(n)
	}
	if methods == 0 {
		item.Evidence = append(item.Evidence, "no other domain calls a method of this domain's types: none is held to a concrete type, so nothing is taken off")
	}
	for i, h := range u.Held {
		if i == 10 {
			item.Evidence = append(item.Evidence, fmt.Sprintf("and %d more pairs of a type and a package that uses it", len(u.Held)-10))
			break
		}
		item.Evidence = append(item.Evidence, fmt.Sprintf("%s ← %s: %d (method calls %d, field accesses %d)", h.Type, h.User, h.Calls+h.Fields, h.Calls, h.Fields))
	}
	item.Evidence = append(item.Evidence, fmt.Sprintf("%d of the item's %v points are measured; the other %v (registered events, the HTTP route list, the ledger read contract) are not measured in this version",
		GoSurfaceMax, item.Max, item.Max-GoSurfaceMax))
}
