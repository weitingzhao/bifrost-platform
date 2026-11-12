// Package platformcheck runs the platform's rule and changed-package tests.
// A rule test is a top-level TestXxx other than TestMain in a module test
// file which, directly or through functions or package variables reached by
// name in that package's test files, uses go/parser, go/build or
// golang.org/x/tools/go/packages; calls filepath.Walk, filepath.WalkDir or
// fs.WalkDir; calls os.ReadFile, ReadDir, Open, DirFS or filepath.Glob with
// an argument containing a literal "..", starting "../" or ending ".go";
// or runs exec.Command[Context] with program "go". Tests in files whose
// names contain "vocab", and testscope.ExplicitGuards, are also rule tests.
package platformcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/testscope"
)

const NoDatabase = "platformcheck: the database tests did not run: this check has no test database (AGENTRT_TEST_PG is empty; TD-403)"

type pkg struct {
	Dir, ImportPath           string
	TestGoFiles, XTestGoFiles []string
}

func command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	c.Env = env
	return c
}
func load(ctx context.Context, dir string, env []string) ([]pkg, error) {
	c := command(ctx, dir, env, "go", "list", "-json", "./...")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("go list: %w: %s", e, stderr.String())
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var ps []pkg
	for {
		var p pkg
		e = d.Decode(&p)
		if e == io.EOF {
			return ps, nil
		}
		if e != nil {
			return nil, e
		}
		if p.Dir, e = resolve(p.Dir); e != nil {
			return nil, e
		}
		ps = append(ps, p)
	}
}

// resolve returns the absolute path with symbolic links evaluated. go list
// reports directories from the kernel's working directory, which has every
// link resolved (on macOS /var is a link to /private/var), so paths are
// compared only after both sides have been resolved.
func resolve(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(abs)
}

type node struct {
	body    ast.Node
	aliases map[string]string
	vocab   bool
}

func ruleNames(files map[string]*ast.File) []string {
	groups := map[string]map[string]*ast.File{}
	for path, f := range files {
		if groups[f.Name.Name] == nil {
			groups[f.Name.Name] = map[string]*ast.File{}
		}
		groups[f.Name.Name][path] = f
	}
	set := map[string]bool{}
	for _, g := range groups {
		for _, n := range packageRuleNames(g) {
			set[n] = true
		}
	}
	return sorted(set)
}
func packageRuleNames(files map[string]*ast.File) []string {
	nodes := map[string][]node{}
	tests := map[string]bool{}
	for path, f := range files {
		aliases := map[string]string{}
		for _, i := range f.Imports {
			p, _ := strconv.Unquote(i.Path.Value)
			name := filepath.Base(p)
			if i.Name != nil {
				name = i.Name.Name
			}
			aliases[name] = p
		}
		for _, d := range f.Decls {
			n := node{aliases: aliases, vocab: strings.Contains(filepath.Base(path), "vocab")}
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					continue
				}
				n.body = d.Body
				nodes[d.Name.Name] = append(nodes[d.Name.Name], n)
				name := d.Name.Name
				if strings.HasPrefix(name, "Test") && name != "TestMain" && len(name) > 4 {
					r, _ := utf8.DecodeRuneInString(name[4:])
					if !unicode.IsLower(r) {
						tests[name] = true
					}
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if v, ok := s.(*ast.ValueSpec); ok {
						n.body = v
						for _, id := range v.Names {
							nodes[id.Name] = append(nodes[id.Name], n)
						}
					}
				}
			}
		}
	}
	var out []string
	for name := range tests {
		seen := map[string]bool{}
		var reaches func(string) bool
		reaches = func(name string) bool {
			if seen[name] {
				return false
			}
			seen[name] = true
			for _, n := range nodes[name] {
				if n.vocab && tests[name] {
					return true
				}
				found := false
				values := map[string][]ast.Expr{}
				ast.Inspect(n.body, func(a ast.Node) bool {
					if a, ok := a.(*ast.AssignStmt); ok {
						for i, l := range a.Lhs {
							if id, ok := l.(*ast.Ident); ok && i < len(a.Rhs) {
								values[id.Name] = append(values[id.Name], a.Rhs[i])
							}
						}
					}
					if v, ok := a.(*ast.ValueSpec); ok {
						for i, id := range v.Names {
							if i < len(v.Values) {
								values[id.Name] = append(values[id.Name], v.Values[i])
							}
						}
					}
					return true
				})
				ast.Inspect(n.body, func(a ast.Node) bool {
					if id, ok := a.(*ast.Ident); ok && reaches(id.Name) {
						found = true
					}
					if s, ok := a.(*ast.SelectorExpr); ok {
						if id, ok := s.X.(*ast.Ident); ok {
							switch n.aliases[id.Name] {
							case "go/parser", "go/build", "golang.org/x/tools/go/packages":
								found = true
							}
						}
					}
					c, ok := a.(*ast.CallExpr)
					if !ok {
						return true
					}
					var p, fn string
					if s, ok := c.Fun.(*ast.SelectorExpr); ok {
						if id, ok := s.X.(*ast.Ident); ok {
							p = n.aliases[id.Name]
							fn = s.Sel.Name
						}
					}
					if id, ok := c.Fun.(*ast.Ident); ok {
						p = n.aliases["."]
						fn = id.Name
					}
					if (p == "path/filepath" && (fn == "Walk" || fn == "WalkDir")) || (p == "io/fs" && fn == "WalkDir") {
						found = true
					}
					if p == "os/exec" && (fn == "Command" || fn == "CommandContext") {
						i := 0
						if fn == "CommandContext" {
							i = 1
						}
						if len(c.Args) > i {
							if l, ok := c.Args[i].(*ast.BasicLit); ok {
								v, _ := strconv.Unquote(l.Value)
								if v == "go" {
									found = true
								}
							}
						}
					}
					if (p == "os" && (fn == "ReadFile" || fn == "ReadDir" || fn == "Open" || fn == "DirFS")) || (p == "path/filepath" && fn == "Glob") {
						for _, arg := range c.Args {
							seenArgs := map[string]bool{}
							var inspectArg func(ast.Node)
							inspectArg = func(arg ast.Node) {
								ast.Inspect(arg, func(a ast.Node) bool {
									if id, ok := a.(*ast.Ident); ok && !seenArgs[id.Name] {
										seenArgs[id.Name] = true
										for _, v := range values[id.Name] {
											inspectArg(v)
										}
										for _, v := range nodes[id.Name] {
											if value, ok := v.body.(*ast.ValueSpec); ok {
												inspectArg(value)
											}
										}
									}
									if l, ok := a.(*ast.BasicLit); ok && l.Kind == token.STRING {
										v, _ := strconv.Unquote(l.Value)
										if v == ".." || strings.HasPrefix(v, "../") || strings.HasSuffix(v, ".go") {
											found = true
										}
									}
									return true
								})
							}
							inspectArg(arg)
						}
					}
					return true
				})
				if found {
					return true
				}
			}
			return false
		}
		if reaches(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
func rules(ps []pkg, dir string) (map[string][]string, error) {
	result := map[string][]string{}
	for _, p := range ps {
		files := map[string]*ast.File{}
		for _, name := range append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...) {
			f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, name), nil, 0)
			if e != nil {
				return nil, e
			}
			files[name] = f
		}
		names := ruleNames(files)
		rel, e := filepath.Rel(dir, p.Dir)
		if e != nil {
			return nil, e
		}
		set := map[string]bool{}
		for _, n := range append(names, testscope.ExplicitGuards[filepath.ToSlash(rel)]...) {
			set[n] = true
		}
		names = nil
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > 0 {
			result[p.ImportPath] = names
		}
	}
	return result, nil
}

// Run accepts a module directory, a mode (rules or changed), and the complete
// subprocess environment. A nil environment inherits the process environment.
func Run(ctx context.Context, dir, mode string, env []string, out io.Writer) int {
	if env == nil {
		env = os.Environ()
	}
	pg := ""
	for _, v := range env {
		if strings.HasPrefix(v, "AGENTRT_TEST_PG=") {
			pg = strings.TrimPrefix(v, "AGENTRT_TEST_PG=")
		}
	}
	if pg == "" {
		defer fmt.Fprintln(out, NoDatabase)
	}
	fail := func(e error) int { fmt.Fprintln(out, "platformcheck:", e); return 1 }
	abs, e := resolve(dir)
	if e != nil {
		return fail(e)
	}
	ps, e := load(ctx, abs, env)
	if e != nil {
		return fail(e)
	}
	selected := map[string]bool{}
	var names []string
	switch mode {
	case "rules":
		rs, e := rules(ps, abs)
		if e != nil {
			return fail(e)
		}
		for p, ns := range rs {
			selected[p] = true
			names = append(names, ns...)
		}
		keys := sorted(selected)
		for _, p := range keys {
			fmt.Fprintf(out, "platformcheck: %s: %s\n", p, strings.Join(rs[p], " "))
		}
	case "changed":
		c := command(ctx, abs, env, "git", "diff", "--name-only", "--no-renames", "HEAD^", "HEAD")
		var stderr bytes.Buffer
		c.Stderr = &stderr
		b, e := c.Output()
		if e != nil {
			return fail(fmt.Errorf("git diff HEAD^ HEAD: %w: %s", e, stderr.String()))
		}
		for _, path := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if !strings.HasPrefix(path, "api/") {
				continue
			}
			rel := strings.TrimPrefix(path, "api/")
			parts := strings.Split(rel, "/")
			owner := filepath.Dir(rel)
			for i, s := range parts[:len(parts)-1] {
				if s == "testdata" {
					owner = filepath.Join(parts[:i]...)
					break
				}
			}
			for _, p := range ps {
				if filepath.Clean(p.Dir) == filepath.Join(abs, owner) {
					selected[p.ImportPath] = true
				}
			}
		}
		for _, p := range sorted(selected) {
			fmt.Fprintln(out, "platformcheck: changed:", p)
		}
	default:
		return fail(fmt.Errorf("unknown mode %q (want rules or changed)", mode))
	}
	packages := sorted(selected)
	if len(packages) == 0 {
		fmt.Fprintln(out, "platformcheck: no package changed or selected")
		return 0
	}
	args := append([]string{"go", "test", "-count=1"}, packages...)
	if mode == "rules" {
		sort.Strings(names)
		for i := range names {
			names[i] = regexp.QuoteMeta(names[i])
		}
		args = append(args, "-run", "^("+strings.Join(names, "|")+")$")
	}
	fmt.Fprintln(out, "platformcheck:", strings.Join(args, " "))
	c := command(ctx, abs, env, args...)
	c.Stdout = out
	c.Stderr = out
	if e := c.Run(); e != nil {
		if e, ok := e.(*exec.ExitError); ok {
			return e.ExitCode()
		}
		return fail(e)
	}
	return 0
}
func sorted(m map[string]bool) []string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
