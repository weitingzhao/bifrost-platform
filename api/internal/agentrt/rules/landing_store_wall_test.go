package rules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The wall between the store's open and permission files (landingStoreOpen,
// landingStorePermission). An open file lands without the Owner, so it may
// not run code at start, write the permission files' package state, add
// methods to their types, or name a permission record. These checks are
// syntactic: they raise the bar for an open change that reaches into
// permission code, and prove no isolation. An open file is in the same
// package and process; it can still call permission functions, use
// reflection or unsafe, or build a name from pieces.

// storeWallFile is one parsed Go file of the store package.
type storeWallFile struct {
	name string
	open bool
	file *ast.File
}

func parseStoreWall(t *testing.T, dir string) (*token.FileSet, []storeWallFile) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no store files in %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var out []storeWallFile
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		// An external test package shares no state with the store.
		if f.Name.Name != "store" {
			continue
		}
		base := filepath.Base(name)
		_, open := landingStoreEntry(landingStore + base)
		out = append(out, storeWallFile{name: base, open: open, file: f})
	}
	return fset, out
}

// permissionRecordNames are the names of the permission records, read from
// the permission files that define them, so a record added there is guarded
// without editing this list:
//   - landing_grant.go: the note keys badGrantRecord checks (landing_grant,
//     landing_grant_revoked, landing_grant_use, landing_rule_clear, ...) and
//     the Action*Landing approval actions (approve_core_landing, ...);
//   - vocab.go: the rule_* event types of eventTypes, envelopes included
//     (a standing envelope is a rule);
//   - rules.go: the tables it inserts rule versions into (agentrt.rule);
//   - propose.go: the mission columns that bind a mission to its envelope.
func permissionRecordNames(t *testing.T, dir string) []string {
	t.Helper()
	parse := func(name string) *ast.File {
		t.Helper()
		if _, open := landingStoreEntry(landingStore + name); open {
			t.Fatalf("%s is open; the record names must come from permission files", name)
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	str := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	set := map[string]bool{}
	grant := parse("landing_grant.go")
	for _, decl := range grant.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != "badGrantRecord" || d.Body == nil {
				continue
			}
			ast.Inspect(d.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CaseClause); ok {
					for _, e := range c.List {
						if s, ok := str(e); ok {
							set[s] = true
						}
					}
				}
				return true
			})
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if strings.HasPrefix(n.Name, "Action") && strings.HasSuffix(n.Name, "Landing") && i < len(vs.Values) {
						if s, ok := str(vs.Values[i]); ok {
							set[s] = true
						}
					}
				}
			}
		}
	}
	vocab := parse("vocab.go")
	for _, decl := range vocab.Decls {
		d, ok := decl.(*ast.GenDecl)
		if !ok || d.Tok != token.VAR {
			continue
		}
		for _, spec := range d.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if n.Name != "eventTypes" || i >= len(vs.Values) {
					continue
				}
				for _, elt := range vs.Values[i].(*ast.CompositeLit).Elts {
					if s, ok := str(elt.(*ast.KeyValueExpr).Key); ok && strings.HasPrefix(s, "rule_") {
						set[s] = true
					}
				}
			}
		}
	}
	scan := func(f *ast.File, re *regexp.Regexp, prefix string) {
		ast.Inspect(f, func(n ast.Node) bool {
			if s, ok := n.(ast.Expr); ok {
				if v, ok := str(s); ok {
					for _, m := range re.FindAllStringSubmatch(v, -1) {
						set[prefix+m[1]] = true
					}
				}
			}
			return true
		})
	}
	scan(parse("rules.go"), regexp.MustCompile(`INSERT INTO agentrt\.(\w+)`), "agentrt.")
	scan(parse("propose.go"), regexp.MustCompile(`\b(envelope_rule_\w+)\s*=`), "")
	names := slices.Sorted(func(yield func(string) bool) {
		for s := range set {
			if !yield(s) {
				return
			}
		}
	})
	// The extraction must still find the records the Owner named; if a
	// permission file is restructured, fix the extraction, not this list.
	for _, want := range []string{"landing_grant", "landing_grant_revoked", "landing_rule_clear", "approve_core_landing",
		"approve_guardrail_landing", "rule_signed", "agentrt.rule", "envelope_rule_id"} {
		if !slices.Contains(names, want) {
			t.Fatalf("permission record %s is no longer found in the permission files: %v", want, names)
		}
	}
	return names
}

// recordNamePatterns match a name as a whole token inside a string literal:
// "agentrt.rule" matches "SELECT ... FROM agentrt.rule WHERE", not
// "agentrt.rule_x"; "landing_grant" does not match "landing_grant_use".
func recordNamePatterns(names []string) map[string]*regexp.Regexp {
	out := map[string]*regexp.Regexp{}
	for _, n := range names {
		out[n] = regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(n) + `($|[^A-Za-z0-9_])`)
	}
	return out
}

// recordLiterals lists the string literals of f that hold a record name.
func recordLiterals(fset *token.FileSet, f *ast.File, patterns map[string]*regexp.Regexp) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			v = lit.Value
		}
		for name, re := range patterns {
			if re.MatchString(v) {
				out = append(out, fmt.Sprintf("%s: string literal names permission record %s", fset.Position(lit.Pos()), name))
			}
		}
		return true
	})
	slices.Sort(out)
	return out
}

// receiverType is the type name a method is declared on.
func receiverType(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// assignedRoot is the variable an assignment target writes: x for x, x.f,
// x[i], *x and (x).
func assignedRoot(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return nil
		}
	}
}

// localNames are the names f's functions declare (parameters, results,
// receivers, var and := declarations, range keys): an assignment to one of
// them is to a local, not to package state. This is coarse per file: a name
// declared locally anywhere is skipped everywhere in the file, which errs
// toward letting a write through, never toward a false alarm.
func localNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	fields := func(l *ast.FieldList) {
		if l == nil {
			return
		}
		for _, fld := range l.List {
			for _, n := range fld.Names {
				out[n.Name] = true
			}
		}
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fields(fn.Recv)
		ast.Inspect(fn, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncType:
				fields(x.Params)
				fields(x.Results)
			case *ast.ValueSpec:
				for _, id := range x.Names {
					out[id.Name] = true
				}
			case *ast.AssignStmt:
				if x.Tok == token.DEFINE {
					for _, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							out[id.Name] = true
						}
					}
				}
			case *ast.RangeStmt:
				if x.Tok == token.DEFINE {
					for _, l := range []ast.Expr{x.Key, x.Value} {
						if id, ok := l.(*ast.Ident); ok {
							out[id.Name] = true
						}
					}
				}
			}
			return true
		})
	}
	return out
}

// storeWallViolations is every breach of the wall by an open file in dir.
func storeWallViolations(t *testing.T, dir string, records []string) []string {
	t.Helper()
	fset, files := parseStoreWall(t, dir)
	vars, types := map[string]string{}, map[string]string{}
	for _, f := range files {
		if f.open {
			continue
		}
		for _, decl := range f.file.Decls {
			d, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if d.Tok == token.VAR {
						for _, n := range s.Names {
							vars[n.Name] = f.name
						}
					}
				case *ast.TypeSpec:
					types[s.Name.Name] = f.name
				}
			}
		}
	}
	patterns := recordNamePatterns(records)
	var out []string
	for _, f := range files {
		if !f.open {
			continue
		}
		own := map[string]bool{}
		for _, decl := range f.file.Decls {
			if d, ok := decl.(*ast.GenDecl); ok && d.Tok == token.VAR {
				for _, spec := range d.Specs {
					for _, n := range spec.(*ast.ValueSpec).Names {
						own[n.Name] = true
					}
				}
			}
		}
		locals := localNames(f.file)
		permissionVar := func(e ast.Expr) {
			id := assignedRoot(e)
			if id == nil || locals[id.Name] || own[id.Name] {
				return
			}
			if from, ok := vars[id.Name]; ok {
				out = append(out, fmt.Sprintf("%s: assigns %s, a package variable of permission file %s", fset.Position(id.Pos()), id.Name, from))
			}
		}
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil && fn.Name.Name == "init" {
				out = append(out, fmt.Sprintf("%s: declares func init", fset.Position(fn.Pos())))
			}
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				if name := receiverType(fn.Recv.List[0].Type); types[name] != "" {
					out = append(out, fmt.Sprintf("%s: declares method %s on %s, a type of permission file %s", fset.Position(fn.Pos()), fn.Name.Name, name, types[name]))
				}
			}
		}
		// Package-level var initializers can assign too, through function
		// literals, so walk the whole file.
		ast.Inspect(f.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if x.Tok != token.DEFINE {
					for _, l := range x.Lhs {
						permissionVar(l)
					}
				}
			case *ast.IncDecStmt:
				permissionVar(x.X)
			case *ast.RangeStmt:
				if x.Tok == token.ASSIGN {
					for _, l := range []ast.Expr{x.Key, x.Value} {
						if l != nil {
							permissionVar(l)
						}
					}
				}
			}
			return true
		})
		out = append(out, recordLiterals(fset, f.file, patterns)...)
	}
	return out
}

// openRecordLiterals is every record-name literal in a non-test Go file
// under root/api on an open path (LandingOpen).
func openRecordLiterals(t *testing.T, root string, records []string) []string {
	t.Helper()
	patterns := recordNamePatterns(records)
	fset := token.NewFileSet()
	var out []string
	err := filepath.WalkDir(filepath.Join(root, "api"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if !LandingOpen("bifrost-platform", filepath.ToSlash(rel)) {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		out = append(out, recordLiterals(fset, f, patterns)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLandingStoreOpenFilesStayOutOfPermission(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repositoryRoot(t), landingStore)
	if got := storeWallViolations(t, dir, permissionRecordNames(t, dir)); len(got) != 0 {
		t.Fatalf("open store files reach into permission code:\n%s", strings.Join(got, "\n"))
	}
}

func TestLandingOpenPathsNameNoPermissionRecord(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	records := permissionRecordNames(t, filepath.Join(root, landingStore))
	if got := openRecordLiterals(t, root, records); len(got) != 0 {
		t.Fatalf("open paths name permission records:\n%s", strings.Join(got, "\n"))
	}
}

// copyGoFiles copies the Go files of src into dst.
func copyGoFiles(t *testing.T, src, dst string) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(src, "*.go"))
	if err != nil || len(names) == 0 {
		t.Fatalf("nothing to copy from %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(name)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func appendFile(t *testing.T, p, data string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

// The control: a temporary copy of the store passes, and each violation
// planted in one of its open files fails the same checker.
func TestLandingStoreWallDetectsPlantedViolation(t *testing.T) {
	t.Parallel()
	src := filepath.Join(repositoryRoot(t), landingStore)
	records := permissionRecordNames(t, src)
	const open = "planpace.go"
	if _, ok := landingStoreOpen[open]; !ok {
		t.Fatalf("%s is no longer open; plant into another open file", open)
	}
	// A package variable and a type of permission files, read from the copy.
	_, files := parseStoreWall(t, src)
	var permVar, permType string
	for _, f := range files {
		if f.open || strings.HasSuffix(f.name, "_test.go") {
			continue
		}
		for _, decl := range f.file.Decls {
			d, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if d.Tok == token.VAR && permVar == "" && s.Names[0].Name != "_" {
						permVar = s.Names[0].Name
					}
				case *ast.TypeSpec:
					if permType == "" && s.TypeParams == nil {
						permType = s.Name.Name
					}
				}
			}
		}
	}
	if permVar == "" || permType == "" {
		t.Fatal("no permission variable or type to plant against")
	}
	for _, c := range []struct{ name, plant, want string }{
		{"init", "\nfunc init() {}\n", "declares func init"},
		{"assign", "\nfunc plantedAssign() { " + permVar + " = " + permVar + " }\n", "assigns " + permVar},
		{"assign in var initializer", "\nvar _ = func() int { " + permVar + " = " + permVar + "; return 0 }()\n", "assigns " + permVar},
		{"method", "\nfunc (" + permType + ") plantedMethod() {}\n", "on " + permType},
		{"pointer method", "\nfunc (*" + permType + ") plantedMethod() {}\n", "on " + permType},
	} {
		dir := filepath.Join(t.TempDir(), "store")
		copyGoFiles(t, src, dir)
		if got := storeWallViolations(t, dir, records); len(got) != 0 {
			t.Fatalf("%s: control copy fails before planting: %v", c.name, got)
		}
		appendFile(t, filepath.Join(dir, open), c.plant)
		if got := storeWallViolations(t, dir, records); len(got) != 1 || !strings.Contains(got[0], c.want) {
			t.Errorf("%s: planted violation not caught: %v", c.name, got)
		}
	}
	for _, name := range records {
		dir := filepath.Join(t.TempDir(), "store")
		copyGoFiles(t, src, dir)
		appendFile(t, filepath.Join(dir, open), "\nconst plantedRecord = "+strconv.Quote("x "+name+" y")+"\n")
		if got := storeWallViolations(t, dir, records); len(got) != 1 || !strings.Contains(got[0], "record "+name) {
			t.Errorf("literal %s: planted violation not caught: %v", name, got)
		}
	}
	// A local of the same name, or a token that only starts with a record
	// name, is no violation.
	dir := filepath.Join(t.TempDir(), "store")
	copyGoFiles(t, src, dir)
	appendFile(t, filepath.Join(dir, open), "\nfunc plantedLocal() { "+permVar+" := 0; "+permVar+" = 1; _ = "+permVar+" }\n"+
		"const plantedNear = \"landing_grant_x agentrt.rule_x\"\n")
	if got := storeWallViolations(t, dir, records); len(got) != 0 {
		t.Errorf("false alarm on a local or a longer token: %v", got)
	}
}

// The control for the api scan: a copy of an open file passes, the same
// copy with a planted record name fails, and a permission path may name it.
func TestLandingOpenPathsDetectsPlantedRecord(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	records := permissionRecordNames(t, filepath.Join(root, landingStore))
	const openDir = "api/internal/agentrt/controller"
	const permDir = "api/internal/agentrt/landing"
	if !LandingOpen("bifrost-platform", openDir+"/x.go") || LandingOpen("bifrost-platform", permDir+"/x.go") {
		t.Fatal("control directories changed class")
	}
	tmp := t.TempDir()
	copyGoFiles(t, filepath.Join(root, openDir), filepath.Join(tmp, openDir))
	if got := openRecordLiterals(t, tmp, records); len(got) != 0 {
		t.Fatalf("control copy fails before planting: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(tmp, permDir), 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(tmp, permDir, "planted.go"), filepath.Join(tmp, openDir, "planted_test.go")} {
		if err := os.WriteFile(p, []byte("package x\n\nconst planted = \"approve_core_landing\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := openRecordLiterals(t, tmp, records); len(got) != 0 {
		t.Fatalf("a permission path or a test file was flagged: %v", got)
	}
	if err := os.WriteFile(filepath.Join(tmp, openDir, "planted.go"), []byte("package controller\n\nconst planted = `UPDATE agentrt.rule SET`\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := openRecordLiterals(t, tmp, records); len(got) != 1 || !strings.Contains(got[0], "agentrt.rule") {
		t.Fatalf("planted record not caught: %v", got)
	}
}
