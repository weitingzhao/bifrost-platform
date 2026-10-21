package agentd

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type tmpWriteKey struct{ file, literal string }

// tmpWriteExemptions may only shrink. Each entry identifies the file and exact
// Go literal text, with the reason this test needs a literal /tmp write.
// Entries that stop matching must be removed.
var tmpWriteExemptions = map[tmpWriteKey]string{
	{"api/internal/agentd/confine_test.go", `"/tmp"`}:                                                                         "TestCursorDataDirIsShortAndWalled tries a short root, falling back to t.TempDir when the wall refuses it",
	{"api/internal/agentd/rootwall_linux_test.go", strings.ReplaceAll(`"echo x > <tmp>/claude-planted-$$"`, "<tmp>", "/tmp")}: "TestClaudeImplementerWritesOnlyItsOwn asserts that its own wall refuses this write",
	{"api/internal/agentd/setup_test.go", strings.ReplaceAll(`"--upload-pack=touch <tmp>/x"`, "<tmp>", "/tmp")}:               "TestPrepareSibling must refuse this argument before any git runs",
}

type tmpWrite struct {
	line    int
	literal string
}

var tmpRedirect = regexp.MustCompile(`>{1,2}[ \t]*["']?/tmp(?:/|[\s"';|&<>]|$)`)
var tmpCommand = regexp.MustCompile(`(?:^|[\s;|&=("'])(?:ln|touch|mkdir|cp|mv|tee|mktemp)[ \t]+[^;|&\n]*`)
var tmpWord = regexp.MustCompile(`(?:^|[\s"'])/tmp(?:/|[\s"';|&<>]|$)`)

// classifyTmpWrites parses source independently of build constraints. Only
// write call path arguments and shell write literals count; reads, ordinary
// values, and comments do not. The literal is kept exactly as written in Go.
func classifyTmpWrites(filename string, source []byte) ([]tmpWrite, error) {
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, err
	}
	found := map[gotoken.Pos]bool{}
	var writes []tmpWrite
	add := func(lit *ast.BasicLit) {
		if !found[lit.Pos()] {
			found[lit.Pos()] = true
			writes = append(writes, tmpWrite{fset.Position(lit.Pos()).Line, lit.Value})
		}
	}
	literal := func(expr ast.Expr) *ast.BasicLit {
		lit, _ := expr.(*ast.BasicLit)
		if lit != nil && lit.Kind == gotoken.STRING {
			return lit
		}
		return nil
	}
	tmpPath := func(lit *ast.BasicLit) bool {
		if lit == nil {
			return false
		}
		s, err := strconv.Unquote(lit.Value)
		return err == nil && (s == "/tmp" || strings.HasPrefix(s, "/tmp/"))
	}
	pathLiteral := func(expr ast.Expr) *ast.BasicLit {
		if lit := literal(expr); lit != nil {
			return lit
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return nil
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Join" {
			return nil
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || (pkg.Name != "filepath" && pkg.Name != "path") {
			return nil
		}
		return literal(call.Args[0])
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" {
					count := 0
					switch sel.Sel.Name {
					case "WriteFile", "Create", "OpenFile", "Mkdir", "MkdirAll", "MkdirTemp", "CreateTemp":
						count = 1
					case "Symlink", "Link", "Rename":
						count = 2
					}
					for i := 0; i < count && i < len(call.Args); i++ {
						if lit := pathLiteral(call.Args[i]); tmpPath(lit) {
							add(lit)
						}
					}
				}
			}
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == gotoken.STRING {
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if tmpRedirect.MatchString(s) {
				add(lit)
			} else {
				for _, command := range tmpCommand.FindAllString(s, -1) {
					if tmpWord.MatchString(command) {
						add(lit)
						break
					}
				}
			}
		}
		return true
	})
	return writes, nil
}

func TestNoNewTmpWrites(t *testing.T) {
	matched := map[tmpWriteKey]bool{}
	for _, root := range []string{".", "../../cmd/agentd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			writes, err := classifyTmpWrites(path, src)
			if err != nil {
				return err
			}
			base, err := filepath.Abs("../..")
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, abs)
			if err != nil {
				return err
			}
			file := "api/" + filepath.ToSlash(rel)
			for _, write := range writes {
				key := tmpWriteKey{file, write.literal}
				if _, ok := tmpWriteExemptions[key]; ok {
					matched[key] = true
				} else {
					t.Errorf("%s:%d: literal /tmp write %s; use t.TempDir() or $TMPDIR", file, write.line, write.literal)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for key, reason := range tmpWriteExemptions {
		if !matched[key] {
			t.Errorf("%s: exemption %s no longer matches; remove it (%s)", key.file, key.literal, reason)
		}
	}
}

func TestTmpWriteClassifier(t *testing.T) {
	// Substitute the path so the guard does not mistake its fixtures for writes.
	cases := []struct {
		name, code string
		want       int
	}{
		{"old bench", "const script = `*/Q10/work) printf new > \"<tmp>/bench-new-$$\" && rm calc.txt && ln -s \"<tmp>/bench-new-$$\" calc.txt ;;`", 1},
		{"current bench", "const script = `*/Q10/work) printf new > \"${TMPDIR:-<tmp>}/bench-new-$$\" && rm calc.txt && ln -s \"${TMPDIR:-<tmp>}/bench-new-$$\" calc.txt ;;`", 0},
		{"mkdirtemp", `func f() { os.MkdirTemp("<tmp>", "cw") }`, 1},
		{"write", `func f() { os.WriteFile("<tmp>/x", nil, 0600) }`, 1},
		{"join", `func f() { os.MkdirAll(filepath.Join("<tmp>", "d"), 0o700) }`, 1},
		{"values", `func f() { _ = got == "<tmp>"; strings.Contains(out, "<tmp>/"); withEnv(env, "X", "<tmp>") }; // <tmp>`, 0},
		{"reads and embedded paths", "var x = []string{`python3 <tmp>/x.py`, `touch .out<tmp>/x`, `mkdir /p/tmp`, `echo ${TMPDIR:-<tmp>}/x`, `CLAUDE_CODE_TMPDIR=<tmp>`, `\"cwd\": \"<tmp>\"`}", 0},
		{"shell commands", "var x = []string{`ln -s <tmp>/x x`, `touch '<tmp>/x'`, `mkdir <tmp>`, `cp x <tmp>/x`, `mv <tmp>/x x`, `tee <tmp>/x`, `mktemp <tmp>/x`, `echo x >>'<tmp>/x'`}", 8},
		{"both paths", `func f() { os.Symlink("x", "<tmp>/x"); os.Link("<tmp>/x", "x"); os.Rename("x", "<tmp>/x"); os.Create("<tmp>/x"); os.OpenFile("<tmp>/x", 0, 0); os.Mkdir("<tmp>/x", 0); os.CreateTemp("<tmp>", "x"); os.MkdirAll(path.Join("<tmp>", "x"), 0) }`, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "package fixture\n" + strings.ReplaceAll(tc.code, "<tmp>", "/tmp")
			got, err := classifyTmpWrites("fixture_test.go", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %v; want %d violations", got, tc.want)
			}
			for _, write := range got {
				if write.line != 2 || write.literal == "" {
					t.Errorf("invalid diagnostic: %+v", write)
				}
			}
		})
	}
}
