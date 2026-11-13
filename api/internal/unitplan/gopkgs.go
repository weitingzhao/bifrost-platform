package unitplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// GoPackage is one package of a Go module as a commit's tree has it: its
// files by name with their git blob ids, and the packages of the same module
// it imports.
type GoPackage struct {
	// Dir is the package directory from the repository root; ImportPath its
	// import path.
	Dir        string `json:"dir"`
	ImportPath string `json:"import_path"`
	// LibFiles are the files that make the package itself: Go files (those a
	// build constraint excludes here as well), other sources, and the files
	// it embeds. TestFiles are its *_test.go files and the files they embed.
	// Names are relative to Dir.
	LibFiles  []GoFile `json:"lib_files"`
	TestFiles []GoFile `json:"test_files"`
	// Testdata is the git tree id of Dir/testdata, "" without one.
	Testdata string `json:"testdata,omitempty"`
	// OtherFiles are the files directly in Dir that are neither: a lock file,
	// a golden file, a manifest a test reads beside itself. go list does not
	// report them; a test can read them, so they are in the test key.
	OtherFiles []GoFile `json:"other_files,omitempty"`
	// Imports and TestImports are the packages of this module the package
	// and its tests import, by import path, sorted.
	Imports     []string `json:"imports"`
	TestImports []string `json:"test_imports"`
	HasTests    bool     `json:"has_tests"`
	// Lib is L(P): the package's own files and the Lib of each import. Test
	// is T(P) without what the manifest adds (GoModule.TestKeyParts).
	Lib string `json:"lib"`
}

// GoFile is a file by its name relative to its package and its blob id.
type GoFile struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// GoModule is the packages of one Go module at one commit.
type GoModule struct {
	// Root is the module directory from the repository root; Path the module
	// path of its go.mod.
	Root, Path string
	// GoMod and GoSum are the blob ids of the two files ("" without one).
	GoMod, GoSum string
	Packages     map[string]*GoPackage // by import path
}

// The platform CI tests for: build constraints are read for it whatever the
// machine this runs on.
const goOS, goArch = "linux", "amd64"

// ReadGoModule reads the Go module at root of the tree: every directory under
// it with Go files is a package (vendor, testdata and directories starting
// with "." or "_" are not looked into, nor nested modules).
func ReadGoModule(t *Tree, root string) (*GoModule, error) {
	root = strings.Trim(root, "/")
	mod := &GoModule{Root: root, Packages: map[string]*GoPackage{}}
	gomod, err := t.ReadFile(join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("unitplan: %s has no go.mod: %w", root, err)
	}
	for _, line := range strings.Split(string(gomod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
			mod.Path = strings.Trim(f[1], `"`)
			break
		}
	}
	if mod.Path == "" {
		return nil, fmt.Errorf("unitplan: %s/go.mod names no module", root)
	}
	if e, ok := t.Lookup(join(root, "go.mod")); ok {
		mod.GoMod = e.ID
	}
	if e, ok := t.Lookup(join(root, "go.sum")); ok {
		mod.GoSum = e.ID
	}
	ctx := buildContext(t)
	var dirs []string
	var walk func(dir string)
	walk = func(dir string) {
		dirs = append(dirs, dir)
		for _, name := range t.Names(dir) {
			p := join(dir, name)
			if e, _ := t.Lookup(p); !e.Dir || name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			if _, nested := t.Lookup(join(p, "go.mod")); nested {
				continue
			}
			walk(p)
		}
	}
	walk(root)
	for _, dir := range dirs {
		pkg, err := readGoPackage(t, &ctx, mod, dir)
		if err != nil {
			return nil, err
		}
		if pkg != nil {
			mod.Packages[pkg.ImportPath] = pkg
		}
	}
	for _, ip := range mod.sorted() {
		if _, err := mod.lib(ip, nil); err != nil {
			return nil, err
		}
	}
	return mod, nil
}

func (m *GoModule) sorted() []string {
	out := make([]string, 0, len(m.Packages))
	for ip := range m.Packages {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

// buildContext reads the tree as go/build's file system, for goOS/goArch
// without cgo (as CI builds).
func buildContext(t *Tree) build.Context {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled, ctx.Compiler = goOS, goArch, false, "gc"
	ctx.GOROOT, ctx.GOPATH, ctx.BuildTags, ctx.ToolTags, ctx.InstallSuffix = "/nonexistent-goroot", "", nil, nil, ""
	ctx.JoinPath = func(elem ...string) string { return path.Join(elem...) }
	ctx.SplitPathList = func(list string) []string { return strings.Split(list, ":") }
	ctx.IsAbsPath = func(p string) bool { return strings.HasPrefix(p, "/") }
	ctx.IsDir = func(p string) bool { e, ok := t.Lookup(p); return ok && e.Dir }
	ctx.HasSubdir = func(root, dir string) (string, bool) {
		root, dir = strings.Trim(root, "/"), strings.Trim(dir, "/")
		if root == "" {
			return dir, true
		}
		return strings.CutPrefix(dir, root+"/")
	}
	ctx.ReadDir = func(dir string) ([]fs.FileInfo, error) {
		e, ok := t.Lookup(dir)
		if !ok || !e.Dir {
			return nil, fs.ErrNotExist
		}
		var out []fs.FileInfo
		for _, name := range t.Names(dir) {
			child, _ := t.Lookup(join(strings.Trim(dir, "/"), name))
			out = append(out, fileInfo{name, child.Dir})
		}
		return out, nil
	}
	ctx.OpenFile = func(p string) (io.ReadCloser, error) {
		b, err := t.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return ctx
}

func readGoPackage(t *Tree, ctx *build.Context, mod *GoModule, dir string) (*GoPackage, error) {
	hasGo := false
	for _, name := range t.Names(dir) {
		if e, _ := t.Lookup(join(dir, name)); !e.Dir && strings.HasSuffix(name, ".go") {
			hasGo = true
			break
		}
	}
	if !hasGo {
		return nil, nil
	}
	// A leading slash: go/build wants a rooted directory; the tree strips it.
	bp, err := ctx.ImportDir("/"+dir, 0)
	var noGo *build.NoGoError
	if err != nil && !errors.As(err, &noGo) {
		return nil, fmt.Errorf("unitplan: package %s: %w", dir, err)
	}
	if noGo != nil && len(bp.TestGoFiles)+len(bp.XTestGoFiles)+len(bp.IgnoredGoFiles) == 0 {
		return nil, nil
	}
	pkg := &GoPackage{Dir: dir, ImportPath: mod.Path, HasTests: len(bp.TestGoFiles)+len(bp.XTestGoFiles) > 0}
	if rel := strings.TrimPrefix(strings.TrimPrefix(dir, mod.Root), "/"); rel != "" {
		pkg.ImportPath = mod.Path + "/" + rel
	}
	var lib, test []string
	for _, list := range [][]string{bp.GoFiles, bp.CgoFiles, bp.CFiles, bp.CXXFiles, bp.MFiles, bp.HFiles, bp.FFiles, bp.SFiles,
		bp.SwigFiles, bp.SwigCXXFiles, bp.SysoFiles, bp.IgnoredOtherFiles} {
		lib = append(lib, list...)
	}
	test = append(append(test, bp.TestGoFiles...), bp.XTestGoFiles...)
	for _, name := range append(append([]string{}, bp.IgnoredGoFiles...), bp.InvalidGoFiles...) {
		if strings.HasSuffix(name, "_test.go") {
			test = append(test, name)
		} else {
			lib = append(lib, name)
		}
	}
	embedded, err := embedFiles(t, dir, bp.EmbedPatterns)
	if err != nil {
		return nil, err
	}
	lib = append(lib, embedded...)
	if embedded, err = embedFiles(t, dir, append(append([]string{}, bp.TestEmbedPatterns...), bp.XTestEmbedPatterns...)); err != nil {
		return nil, err
	}
	test = append(test, embedded...)
	if pkg.LibFiles, err = fileIDs(t, dir, lib); err != nil {
		return nil, err
	}
	if pkg.TestFiles, err = fileIDs(t, dir, test); err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	for _, f := range append(append([]GoFile{}, pkg.LibFiles...), pkg.TestFiles...) {
		listed[f.Name] = true
	}
	for _, name := range t.Names(dir) {
		if e, _ := t.Lookup(join(dir, name)); !e.Dir && !listed[name] {
			pkg.OtherFiles = append(pkg.OtherFiles, GoFile{name, e.ID})
		}
	}
	if e, ok := t.Lookup(join(dir, "testdata")); ok && e.Dir {
		pkg.Testdata = e.ID
	}
	pkg.Imports = inModule(mod.Path, bp.Imports)
	pkg.TestImports = inModule(mod.Path, append(append([]string{}, bp.TestImports...), bp.XTestImports...))
	return pkg, nil
}

func inModule(mod string, imports []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ip := range imports {
		if (ip == mod || strings.HasPrefix(ip, mod+"/")) && !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	sort.Strings(out)
	return out
}

func fileIDs(t *Tree, dir string, names []string) ([]GoFile, error) {
	seen := map[string]bool{}
	out := []GoFile{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		e, ok := t.Lookup(join(dir, name))
		if !ok || e.Dir {
			return nil, fmt.Errorf("unitplan: %s/%s is not a file of this tree", dir, name)
		}
		out = append(out, GoFile{name, e.ID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// embedFiles expands //go:embed patterns as the go command does: a pattern is
// a path.Match glob from the package directory; a directory it names embeds
// every file under it except those in or named with a leading "." or "_",
// unless the pattern starts with "all:". Names are relative to dir.
func embedFiles(t *Tree, dir string, patterns []string) ([]string, error) {
	var out []string
	for _, pattern := range patterns {
		all := false
		glob := pattern
		if rest, ok := strings.CutPrefix(pattern, "all:"); ok {
			all, glob = true, rest
		}
		matched := false
		for _, m := range globTree(t, dir, strings.Split(glob, "/")) {
			e, _ := t.Lookup(join(dir, m))
			if !e.Dir {
				matched = true
				out = append(out, m)
				continue
			}
			for _, f := range t.Walk(join(dir, m)) {
				rel := strings.TrimPrefix(f.Path, join(dir, m)+"/")
				hidden := false
				for _, elem := range strings.Split(rel, "/") {
					hidden = hidden || strings.HasPrefix(elem, ".") || strings.HasPrefix(elem, "_")
				}
				if hidden && !all {
					continue
				}
				matched = true
				out = append(out, m+"/"+rel)
			}
		}
		if !matched {
			return nil, fmt.Errorf("unitplan: %s: //go:embed %s matches no file", dir, pattern)
		}
	}
	return out, nil
}

// globTree matches the pattern's elements against the tree under dir, one
// directory level an element, and returns the matching paths relative to dir.
func globTree(t *Tree, dir string, elems []string) []string {
	if len(elems) == 0 {
		return []string{""}
	}
	var out []string
	for _, name := range t.Names(dir) {
		if ok, _ := path.Match(elems[0], name); !ok {
			continue
		}
		if len(elems) == 1 {
			out = append(out, name)
			continue
		}
		if e, _ := t.Lookup(join(dir, name)); e.Dir {
			for _, rest := range globTree(t, join(dir, name), elems[1:]) {
				out = append(out, name+"/"+rest)
			}
		}
	}
	return out
}

// lib computes L(P) for the package and, first, for every package it imports.
func (m *GoModule) lib(ip string, stack []string) (string, error) {
	pkg, ok := m.Packages[ip]
	if !ok {
		// An import of this module that the tree has no package for: the
		// package does not build, and its key says so rather than hide it.
		return "unresolved:" + ip, nil
	}
	if pkg.Lib != "" {
		return pkg.Lib, nil
	}
	for _, s := range stack {
		if s == ip {
			return "", fmt.Errorf("unitplan: import cycle at %s", ip)
		}
	}
	deps := make([]string, 0, len(pkg.Imports))
	for _, q := range pkg.Imports {
		l, err := m.lib(q, append(stack, ip))
		if err != nil {
			return "", err
		}
		deps = append(deps, q+"="+l)
	}
	pkg.Lib = Hash(map[string]any{"kind": "go-lib", "package": ip, "files": pkg.LibFiles, "imports": deps})
	return pkg.Lib, nil
}

// TestKeyParts is what T(P) is made of before the manifest adds the inputs
// it declares, the images and the task: the package itself, its test files
// and testdata, the other files of its directory, the packages only its
// tests import, go.mod and go.sum.
func (m *GoModule) TestKeyParts(ip string) (map[string]any, error) {
	pkg, ok := m.Packages[ip]
	if !ok {
		return nil, fmt.Errorf("unitplan: no package %s", ip)
	}
	deps := make([]string, 0, len(pkg.TestImports))
	for _, q := range pkg.TestImports {
		l, err := m.lib(q, nil)
		if err != nil {
			return nil, err
		}
		deps = append(deps, q+"="+l)
	}
	return map[string]any{"lib": pkg.Lib, "test_files": pkg.TestFiles, "testdata": pkg.Testdata, "other_files": pkg.OtherFiles, "test_imports": deps,
		"go_mod": m.GoMod, "go_sum": m.GoSum}, nil
}

// Hash is the sha256, in hex, of a document's JSON with sorted keys: the form
// every key of a plan is computed in, so anyone can recompute it from git.
func Hash(doc any) string {
	b, err := json.Marshal(doc) // maps marshal with sorted keys
	if err != nil {
		panic("unitplan: a key document does not marshal: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}
