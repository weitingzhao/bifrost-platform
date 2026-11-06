package testscope

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// storeRun selects store tests by name; "" runs the whole store package. code
// says the store was selected by a code change or as a guard; data holds the
// base names of changed testdata files the store reads.
func storeRun(g *graph, store string, changedFiles []string, changed map[string]bool, code bool, data []string) (string, error) {
	p := g.packages[store]
	if p == nil {
		return "", fmt.Errorf("store missing")
	}
	entries, e := os.ReadDir(p.Dir)
	if e != nil {
		return "", e
	}
	sources := map[string]*ast.File{}
	tests := map[string]*ast.File{}
	texts := map[string][]byte{}
	all := map[string]bool{}
	for _, v := range entries {
		if !strings.HasSuffix(v.Name(), ".go") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(p.Dir, v.Name()))
		if e != nil {
			return "", e
		}
		f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, v.Name()), b, 0)
		if e != nil {
			return "", e
		}
		sources[v.Name()] = f
		if strings.HasSuffix(v.Name(), "_test.go") {
			tests[v.Name()] = f
			texts[v.Name()] = b
			for _, n := range testNames(f) {
				all[n] = true
			}
		}
	}
	if code && len(changedFiles) == 0 {
		// A store import is the nearest package to store on its dependency path.
		// Keep every such path, including longer paths alongside a direct import:
		// choosing only the shortest path could miss another affected source file.
		nearest := map[string]bool{}
		for _, f := range sources {
			for _, imp := range f.Imports {
				i, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return "", err
				}
				// The store package itself is no path: every external store
				// test imports it, and through the tests' own imports it
				// depends on what they reach, so counting it picked every
				// test file and the whole package ran (CI 2026-11-06: one line
				// in internal/delivery ran all 1251 store tests).
				if i == store {
					continue
				}
				// Through i's code, not its tests: a package whose own
				// tests reach the change does not change for the store.
				for c := range changed {
					if i == c || g.codeDeps[i][c] {
						nearest[i] = true
					}
				}
			}
		}
		for name, f := range sources {
			for _, i := range f.Imports {
				v, _ := strconv.Unquote(i.Path.Value)
				if nearest[v] {
					changedFiles = append(changedFiles, name)
					break
				}
			}
		}
		if len(changedFiles) == 0 {
			return "", nil
		}
	}
	selected := map[string]bool{}
	for _, name := range changedFiles {
		f := sources[name]
		if f == nil {
			return "", nil // A resolved package with a removed file needs every store test.
		}
		if strings.HasSuffix(name, "_test.go") {
			// A test file selects its own tests. One with none holds helpers
			// (a fixture file): it selects the tests that use what it declares,
			// as a source file does below.
			if ns := testNames(f); len(ns) > 0 {
				for _, n := range ns {
					selected[n] = true
				}
				continue
			}
		}
		ids := map[string]bool{}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					ids[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						ids[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							ids[n.Name] = true
						}
					}
				}
			}
		}
		// The blank identifier and init name nothing another file can use;
		// "_" in a helper file (var _ I = (*T)(nil)) matched every test file
		// that has a blank identifier (CI 2026-11-06).
		delete(ids, "_")
		delete(ids, "init")
		stem := strings.TrimSuffix(name, ".go")
		for tn, tf := range tests {
			ts := strings.TrimSuffix(strings.TrimSuffix(tn, "_test.go"), "_internal")
			match := ts == stem || strings.HasPrefix(ts, stem+"_")
			external := tf.Name.Name != f.Name.Name
			aliases := map[string]bool{}
			for _, i := range tf.Imports {
				v, _ := strconv.Unquote(i.Path.Value)
				if v == store {
					alias := "store"
					if i.Name != nil {
						alias = i.Name.Name
					}
					aliases[alias] = true
				}
			}
			ast.Inspect(tf, func(n ast.Node) bool {
				if external {
					if s, ok := n.(*ast.SelectorExpr); ok {
						if id, ok := s.X.(*ast.Ident); ok && aliases[id.Name] && ids[s.Sel.Name] {
							match = true
						}
					}
					if id, ok := n.(*ast.Ident); ok && aliases["."] && ids[id.Name] {
						match = true
					}
				} else if id, ok := n.(*ast.Ident); ok && ids[id.Name] {
					match = true
				}
				return true
			})
			if match {
				for _, n := range testNames(tf) {
					selected[n] = true
				}
			}
		}
	}
	// A testdata file selects the tests whose files name it; a file no store
	// test names needs every store test.
	for _, d := range data {
		found := false
		for tn, tf := range tests {
			if bytes.Contains(texts[tn], []byte(d)) {
				ns := testNames(tf)
				if len(ns) == 0 {
					return "", nil
				}
				for _, n := range ns {
					selected[n] = true
				}
				found = true
			}
		}
		if !found {
			return "", nil
		}
	}
	if len(selected) == 0 || len(selected)*2 > len(all) {
		return "", nil
	}
	names := make([]string, 0, len(selected))
	for n := range selected {
		names = append(names, regexp.QuoteMeta(n))
	}
	sort.Strings(names)
	return "^(" + strings.Join(names, "|") + ")$", nil
}
