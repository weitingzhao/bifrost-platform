// Package testscope conservatively selects tests for a repository landing.
package testscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Result struct{ Scope, Packages, StoreRun, Why string }

func Full(why string) Result {
	return Result{Scope: "full", Why: strings.Join(strings.Fields(why), " ")}
}
func (r Result) Write(w io.Writer) {
	fmt.Fprintf(w, "scope=%s\npackages=%s\nstore_run=%s\nwhy=%s\n", r.Scope, r.Packages, r.StoreRun, r.Why)
}

type Change struct {
	Status string
	Paths  []string
}

// ParseChanges accepts git diff --name-status output, including both rename paths.
func ParseChanges(r io.Reader) ([]Change, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	var out []Change
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			return nil, fmt.Errorf("invalid name-status line")
		}
		if f[0] == "" || !strings.ContainsRune("ACDMRTUXB", rune(f[0][0])) {
			return nil, fmt.Errorf("invalid change status")
		}
		for _, ch := range f[0][1:] {
			if ch < '0' || ch > '9' {
				return nil, fmt.Errorf("invalid change status")
			}
		}
		for _, p := range f[1:] {
			if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
				return nil, fmt.Errorf("invalid change path")
			}
		}
		n := 1
		if strings.HasPrefix(f[0], "R") || strings.HasPrefix(f[0], "C") {
			n = 2
		}
		if len(f) != n+1 {
			return nil, fmt.Errorf("invalid name-status paths")
		}
		for _, p := range f[1:] {
			if strings.HasPrefix(p, "\"") {
				return nil, fmt.Errorf("quoted path")
			}
		}
		out = append(out, Change{f[0], f[1:]})
	}
	return out, nil
}
func command(dir string, args ...string) ([]byte, error) {
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	b, e := c.Output()
	if e != nil {
		if ee, ok := e.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s: %w: %s", args[0], e, ee.Stderr)
		}
		return nil, fmt.Errorf("%s: %w", args[0], e)
	}
	return b, nil
}

// Select verifies the history before asking git for changes. All failures are full scope.
func Select(module, base, head string) Result {
	if _, e := command(module, "git", "cat-file", "-e", base+"^{commit}"); e != nil {
		return Full("unreadable base")
	}
	if _, e := command(module, "git", "merge-base", "--is-ancestor", base, head); e != nil {
		return Full("base is not an ancestor of head")
	}
	b, e := command(module, "git", "diff", "--name-status", "-M", base, head, "--")
	if e != nil {
		return Full("git diff error")
	}
	return SelectChanges(module, bytes.NewReader(b))
}

type pkg struct {
	ImportPath, Dir, Name, ForTest string
	Imports, Deps                  []string
	Error                          *struct{ Err string }
	DepsErrors                     []struct{ Err string }
}
type graph struct {
	module, prefix string
	packages       map[string]*pkg
	dirs           map[string]string
	deps           map[string]map[string]bool
	// codeDeps is each package's dependencies without its tests' (deps holds
	// both): what a store file reaches through the packages it imports.
	codeDeps map[string]map[string]bool
}

func plain(s string) string { return strings.Split(s, " [")[0] }
func load(module string) (*graph, error) {
	abs, e := filepath.Abs(module)
	if e != nil {
		return nil, e
	}
	b, e := command(abs, "go", "list", "-m", "-f", "{{.Path}}")
	if e != nil {
		return nil, e
	}
	g := &graph{module: abs, prefix: strings.TrimSpace(string(b)), packages: map[string]*pkg{}, dirs: map[string]string{}, deps: map[string]map[string]bool{}, codeDeps: map[string]map[string]bool{}}
	b, e = command(abs, "go", "list", "-buildvcs=false", "-json", "-deps", "-test", "./...")
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	for {
		var p pkg
		e = d.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if p.Error != nil || len(p.DepsErrors) > 0 {
			return nil, fmt.Errorf("package load error")
		}
		name := plain(p.ImportPath)
		if p.ForTest != "" {
			name = p.ForTest
		}
		if p.Name == "main" && strings.HasSuffix(p.ImportPath, ".test") {
			continue
		}
		if p.Dir == abs || strings.HasPrefix(p.Dir, abs+string(filepath.Separator)) {
			if p.ForTest == "" {
				q := p
				g.packages[name] = &q
				g.dirs[p.Dir] = name
			}
			if g.deps[name] == nil {
				g.deps[name] = map[string]bool{}
			}
			for _, v := range p.Deps {
				g.deps[name][plain(v)] = true
			}
			if p.ForTest == "" && !strings.Contains(p.ImportPath, " [") {
				code := map[string]bool{}
				for _, v := range p.Deps {
					code[plain(v)] = true
				}
				g.codeDeps[name] = code
			}
		}
	}
	return g, nil
}
func trigger(p string) string {
	if p == "api/go.mod" || p == "api/go.sum" {
		return "module dependencies"
	}
	for _, v := range []string{"api/internal/agentrt/migrations/", "api/internal/testscope/", "api/cmd/testscope/"} {
		if strings.HasPrefix(p, v) {
			return "protected path " + v
		}
	}
	if p == "scripts/ci/test_api_scoped.sh" {
		return "selector script"
	}
	return ""
}

// testdataOwner returns the directory, relative to the module, that owns the
// first testdata element of rel.
func testdataOwner(rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	for i, v := range parts[:len(parts)-1] {
		if v == "testdata" {
			return strings.Join(parts[:i], "/"), true
		}
	}
	return "", false
}

// testdataReaders maps each package to the base names of the changed testdata
// files it reads: the owner of each file, and every package whose test files
// name the file's base name or its path relative to the module.
func testdataReaders(g *graph, data map[string][]string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	add := func(p, base string) {
		if out[p] == nil {
			out[p] = map[string]bool{}
		}
		out[p][base] = true
	}
	for owner, rels := range data {
		for _, rel := range rels {
			add(owner, filepath.Base(rel))
		}
	}
	for name, p := range g.packages {
		entries, e := os.ReadDir(p.Dir)
		if e != nil {
			return nil, e
		}
		for _, v := range entries {
			if !strings.HasSuffix(v.Name(), "_test.go") {
				continue
			}
			b, e := os.ReadFile(filepath.Join(p.Dir, v.Name()))
			if e != nil {
				return nil, e
			}
			for _, rels := range data {
				for _, rel := range rels {
					if bytes.Contains(b, []byte(filepath.Base(rel))) || bytes.Contains(b, []byte(rel)) {
						add(name, filepath.Base(rel))
					}
				}
			}
		}
	}
	return out, nil
}

// ExplicitGuards supplements guards discovered by scanning test source.
var ExplicitGuards = map[string][]string{"internal/cluster": {"TestRemovedPrimarySQLHelperStaysGone"}, "internal/agentrt/agentrtapi": {"TestPools"}, "internal/safego": {"TestNoUnguardedGoroutines"}}

func testNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && len(fn.Name.Name) >= 4 && fn.Name.Name != "TestMain" {
			r, _ := utf8.DecodeRuneInString(fn.Name.Name[4:])
			if len(fn.Name.Name) > 4 && unicode.IsLower(r) {
				continue
			}
			out = append(out, fn.Name.Name)
		}
	}
	return out
}
func walks(f *ast.File) bool {
	aliases := map[string]bool{}
	for _, i := range f.Imports {
		p, _ := strconv.Unquote(i.Path.Value)
		if p == "path/filepath" {
			n := "filepath"
			if i.Name != nil {
				n = i.Name.Name
			}
			aliases[n] = true
		}
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if ok {
			id, ok := s.X.(*ast.Ident)
			if ok && aliases[id.Name] && (s.Sel.Name == "Walk" || s.Sel.Name == "WalkDir") {
				found = true
			}
		} else if id, ok := c.Fun.(*ast.Ident); ok && aliases["."] && (id.Name == "Walk" || id.Name == "WalkDir") {
			found = true
		}
		return true
	})
	return found
}
func guards(g *graph) (map[string]bool, error) {
	out := map[string]bool{}
	for p := range ExplicitGuards {
		if g.packages[g.prefix+"/"+p] != nil {
			out[g.prefix+"/"+p] = true
		}
	}
	for name, p := range g.packages {
		entries, e := os.ReadDir(p.Dir)
		if e != nil {
			return nil, e
		}
		for _, v := range entries {
			if strings.HasSuffix(v.Name(), "_test.go") {
				f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, v.Name()), nil, 0)
				if e != nil {
					return nil, e
				}
				if walks(f) {
					out[name] = true
				}
			}
		}
	}
	return out, nil
}
func SelectChanges(module string, r io.Reader) (result Result) {
	defer func() {
		if recover() != nil {
			result = Full("selector panic")
		}
	}()
	changes, e := ParseChanges(r)
	if e != nil {
		return Full("change list error: " + e.Error())
	}
	for _, c := range changes {
		for _, p := range c.Paths {
			if why := trigger(p); why != "" {
				return Full(why)
			}
		}
	}
	g, e := load(module)
	if e != nil {
		return Full("package graph error: " + e.Error())
	}
	changed := map[string]bool{}
	files := map[string][]string{}
	data := map[string][]string{}
	for _, c := range changes {
		for _, p := range c.Paths {
			if !strings.HasPrefix(p, "api/") {
				continue
			}
			rel := strings.TrimPrefix(p, "api/")
			if dir, ok := testdataOwner(rel); ok {
				name := g.dirs[filepath.Join(g.module, dir)]
				if name == "" {
					return Full("unresolved testdata package")
				}
				q := g.packages[name]
				if strings.HasSuffix(q.Name, "test") || strings.HasSuffix(q.Name, "testutil") {
					return Full("shared test helper package")
				}
				data[name] = append(data[name], rel)
				continue
			}
			name := g.dirs[filepath.Join(g.module, filepath.Dir(rel))]
			if name == "" {
				if strings.HasSuffix(p, ".go") {
					return Full("unresolved Go package")
				}
				continue
			}
			q := g.packages[name]
			if strings.HasSuffix(q.Name, "test") || strings.HasSuffix(q.Name, "testutil") {
				return Full("shared test helper package")
			}
			if !strings.HasSuffix(p, ".go") {
				continue
			}
			changed[name] = true
			files[name] = append(files[name], filepath.Base(p))
		}
	}
	selected, e := guards(g)
	if e != nil {
		return Full("guard scan error: " + e.Error())
	}
	for p := range g.packages {
		if changed[p] {
			selected[p] = true
		}
		for c := range changed {
			if g.deps[p][c] {
				selected[p] = true
			}
		}
	}
	store := g.prefix + "/internal/agentrt/store"
	code := selected[store]
	why := "changed packages, test dependents, and guards"
	readers := map[string]map[string]bool{}
	if len(data) > 0 {
		readers, e = testdataReaders(g, data)
		if e != nil {
			return Full("testdata scan error: " + e.Error())
		}
		var names []string
		for p := range readers {
			selected[p] = true
			names = append(names, p)
		}
		sort.Strings(names)
		why += "; testdata: " + strings.Join(names, " ")
	}
	run := ""
	if selected[store] {
		var bases []string
		for b := range readers[store] {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		run, e = storeRun(g, store, files[store], changed, code, bases)
		if e != nil {
			return Full("store selection error: " + e.Error())
		}
	}
	var names []string
	for p := range selected {
		if g.packages[p] != nil {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	return Result{"incremental", strings.Join(names, " "), run, why}
}
