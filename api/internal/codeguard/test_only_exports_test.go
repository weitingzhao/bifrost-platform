package codeguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// An exported name that only other packages' tests use is a door left open
// for tests into a package's insides (2026-11-17).
//
// Origin: the same rule as tests_reach_test.go (OWNER_LAW #28; the CI line's
// page duty/from-ci-w1-2026-11-17-test-standard.md). The decoupling line
// moved the plan code out of agentrt/store into agentrt/planstore and found
// 21 names planstore exports for the store tests that still assert the plan
// code from inside store (TD-582, its notes biz-align/planstore-tests-
// 2026-11-17.md): no production code of another package uses them. Such a
// name is part of no contract and widens one. The list is what the tree has
// and only shrinks: a name leaves when the tests that use it test through
// the contract, or when production code of another package uses it (then it
// is surface, for the contract check to hold).
//
// It reads names, not types: a package-level exported name of a package's
// production files (a function, type, variable or constant; not a method or
// a field) and the selectors "<import>.<Name>" of other packages' files.

// exportUses is, per "<package>.<Name>", whether other packages' production
// code and other packages' tests name it.
type exportUses struct{ prod, test bool }

// testOnlyExports reads the module under root/api and returns, sorted, the
// "<package>.<Name>" that some other package's test names and no other
// package's production code does. module is the module path with a
// trailing slash.
func testOnlyExports(root, module string) ([]string, error) {
	fset := token.NewFileSet()
	exported := map[string]bool{}
	// signature is, by an exported function's key, the keys of its own
	// package's types its parameters and results name: a production use of
	// the function is a use of them (Load returns Input; nobody writes
	// domainscore.Input to call it).
	signature := map[string][]string{}
	uses := map[string]*exportUses{}
	type parsed struct {
		pkg  string
		test bool
		file *ast.File
	}
	var files []parsed
	err := filepath.WalkDir(filepath.Join(root, "api"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		files = append(files, parsed{filepath.ToSlash(rel), strings.HasSuffix(name, "_test.go"), f})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range files {
		if p.test || strings.HasSuffix(p.file.Name.Name, "_test") {
			continue
		}
		for _, decl := range p.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					k := p.pkg + "." + d.Name.Name
					exported[k] = true
					for _, fl := range []*ast.FieldList{d.Type.Params, d.Type.Results} {
						if fl == nil {
							continue
						}
						for _, field := range fl.List {
							ast.Inspect(field.Type, func(n ast.Node) bool {
								if id, ok := n.(*ast.Ident); ok && id.IsExported() {
									signature[k] = append(signature[k], p.pkg+"."+id.Name)
								}
								return true
							})
						}
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							exported[p.pkg+"."+s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								exported[p.pkg+"."+n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	for _, p := range files {
		aliases := map[string]string{}
		for _, imp := range p.file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(path, module) {
				continue
			}
			target := strings.TrimPrefix(path, module)
			if target == p.pkg {
				continue
			}
			name := filepath.Base(target)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "_" || name == "." {
				continue
			}
			aliases[name] = target
		}
		if len(aliases) == 0 {
			continue
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			target, ok := aliases[id.Name]
			if !ok {
				return true
			}
			k := target + "." + sel.Sel.Name
			if !exported[k] {
				return true
			}
			if uses[k] == nil {
				uses[k] = &exportUses{}
			}
			if p.test {
				uses[k].test = true
			} else {
				uses[k].prod = true
			}
			return true
		})
	}
	for k, u := range uses {
		if !u.prod {
			continue
		}
		for _, t := range signature[k] {
			if exported[t] {
				if uses[t] == nil {
					uses[t] = &exportUses{}
				}
				uses[t].prod = true
			}
		}
	}
	var out []string
	for k, u := range uses {
		if u.test && !u.prod {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

func TestNoNewNameIsExportedForAnotherPackagesTests(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	got, err := testOnlyExports(root, module)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 10 {
		t.Fatalf("the scan found %d names: it did not read the tree", len(got))
	}
	if os.Getenv("CODEGUARD_PRINT_REACH") != "" {
		t.Log("\nvar exportedForOtherTests = []string{\n\t\"" + strings.Join(got, "\",\n\t\"") + "\",\n}")
	}
	t.Logf("names only other packages' tests use: %d", len(got))
	held := map[string]bool{}
	for _, k := range exportedForOtherTests {
		held[k] = true
	}
	now := map[string]bool{}
	for _, k := range got {
		now[k] = true
		if !held[k] {
			t.Errorf("%s is exported and only other packages' tests use it: a test reaches a package through its contract (OWNER_LAW #28). Test through what the package offers, move the test into the package, or unexport the name. The list exportedForOtherTests only shrinks.", k)
		}
	}
	for k := range held {
		if !now[k] {
			t.Errorf("%s is no longer used by other packages' tests only: take it off exportedForOtherTests (the list only shrinks)", k)
		}
	}
}

// The positive control of the check above, on a made-up module: a name
// another package's test uses and no other package's production code does is
// listed; one production code elsewhere uses is not, nor one only its own
// package's tests use, nor a method; an alias import is followed; a type
// that another package's production code takes through a function's
// signature (Load returns In) is not listed though it never names it.
func TestTestOnlyExportsReadsTheNamesAndTheirUsers(t *testing.T) {
	root := t.TempDir()
	write := func(path, src string) {
		t.Helper()
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("api/internal/a/a.go", "package a\nfunc ForTests() {}\nfunc Used() {}\nfunc Own() {}\ntype T struct{}\nfunc (T) M() {}\nconst Limit = 3\ntype In struct{}\nfunc Load() (In, error) { return In{}, nil }\n")
	write("api/internal/a/a_test.go", "package a\nfunc x() { Own() }\n")
	write("api/internal/b/b.go", "package b\nimport \"m.test/api/internal/a\"\nfunc f() { a.Used(); a.Load() }\n")
	write("api/internal/b/b_test.go", "package b\nimport aa \"m.test/api/internal/a\"\nfunc g() { aa.ForTests(); aa.Used(); var t aa.T; t.M(); _ = aa.Limit; var i aa.In; _ = i }\n")
	got, err := testOnlyExports(root, "m.test/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "api/internal/a.ForTests,api/internal/a.Limit,api/internal/a.T" {
		t.Fatalf("got %v", got)
	}
}
