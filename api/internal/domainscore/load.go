package domainscore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Module is the import path of the repository's Go module, with a trailing
// slash: Module+"api/internal/x" is the package the ownership table calls
// "api/internal/x".
const Module = "github.com/weitingzhao/bifrost-platform/"

// Load reads the checkout at root the way cmd/domainscore scores it: the
// ruler, the ownership table, the walls, every package of the module with
// its production imports and lines (`go list -export -deps`), the routes of
// the mount files, and the contracts item's counts from type-checking every
// package's production files. It returns the Input Score takes and the
// Surface it counted with (UseList). It changes nothing in the checkout; it
// runs `go list` and `git rev-parse`, and the deployed platform never calls
// it (2026-11-17: moved here from cmd/domainscore so that the decoupling
// line's contract check reads the same counts as the ruler).
func Load(root string) (Input, *Surface, error) {
	rawRuler, err := os.ReadFile(filepath.Join(root, "config", "quality-ruler.json"))
	if err != nil {
		return Input{}, nil, err
	}
	ruler, err := ParseRuler(rawRuler)
	if err != nil {
		return Input{}, nil, err
	}
	rawOwn, err := os.ReadFile(filepath.Join(root, "config", "domain-ownership.json"))
	if err != nil {
		return Input{}, nil, err
	}
	in := Input{Ruler: ruler, WallHolds: map[string]bool{}}
	if err = json.Unmarshal(rawOwn, &in.Ownership); err != nil {
		return Input{}, nil, fmt.Errorf("the ownership table does not parse: %w", err)
	}
	if out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		in.Commit = strings.TrimSpace(string(out))
	}
	for domain, wall := range ruler.Walls {
		src, err := os.ReadFile(filepath.Join(root, wall.File))
		in.WallHolds[domain] = err == nil && bytes.Contains(src, []byte("func "+wall.Test+"(t *testing.T)"))
	}
	// -deps names the standard library and the modules too: their export
	// data is what the module's own packages are type-checked against.
	cmd := exec.Command("go", "list", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,CgoFiles,Imports,ImportMap,Export", "./...")
	cmd.Dir = filepath.Join(root, "api")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return Input{}, nil, fmt.Errorf("go list: %w", err)
	}
	var own []listed
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p listed
		if err = dec.Decode(&p); err != nil {
			return Input{}, nil, fmt.Errorf("go list: %w", err)
		}
		exports[p.ImportPath] = p.Export
		if !strings.HasPrefix(p.ImportPath, Module) {
			continue
		}
		own = append(own, p)
		pkg := Package{Path: strings.TrimPrefix(p.ImportPath, Module)}
		for _, dep := range p.Imports {
			if strings.HasPrefix(dep, Module) {
				pkg.Imports = append(pkg.Imports, strings.TrimPrefix(dep, Module))
			}
		}
		for _, f := range append(p.GoFiles, p.CgoFiles...) {
			pkg.Lines += lines(filepath.Join(p.Dir, f))
		}
		in.Packages = append(in.Packages, pkg)
	}
	if len(in.Packages) < 50 {
		return Input{}, nil, fmt.Errorf("go list named %d packages", len(in.Packages))
	}
	s, err := surface(NewGraph(in.Ownership, in.Packages), own, exports)
	if err != nil {
		return Input{}, nil, err
	}
	in.Contracts = s.Uses()
	if in.Routes, err = routes(root); err != nil {
		return Input{}, nil, err
	}
	return in, s, nil
}

func lines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n, sc := 0, bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		n++
	}
	return n
}

type listed struct {
	ImportPath, Dir, Export    string
	GoFiles, CgoFiles, Imports []string
	ImportMap                  map[string]string
}

// mapped resolves an import the way the importing package's ImportMap says
// (the standard library's vendored packages) before the export data is read.
type mapped struct {
	to map[string]string
	types.Importer
}

func (m mapped) Import(path string) (*types.Package, error) {
	if to, ok := m.to[path]; ok {
		path = to
	}
	return m.Importer.Import(path)
}

// surface type-checks the production files of every package of the module
// and counts what the domains use of each other. A package that does not
// type-check stops the run: a count from half a tree would be a guess.
func surface(graph *Graph, own []listed, exports map[string]string) (*Surface, error) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, fmt.Errorf("go list gave no export data for %s", path)
		}
		return os.Open(exports[path])
	})
	s := NewSurface(graph, Module)
	for _, p := range own {
		if len(p.CgoFiles) > 0 {
			return nil, fmt.Errorf("%s has cgo files; this tool does not type-check them", p.ImportPath)
		}
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: mapped{p.ImportMap, imp}}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			return nil, fmt.Errorf("type-checking %s: %w", p.ImportPath, err)
		}
		s.Add(strings.TrimPrefix(p.ImportPath, Module), fset, info)
	}
	return s, nil
}

var mountFile = regexp.MustCompile(`^mount_([a-z]+)\.go$`)

// routes counts the routes of every api/internal/server/mount_<name>.go, by
// <name>. Score reads the entries whose name is a domain.
func routes(root string) (map[string]int, error) {
	dir := filepath.Join(root, "api", "internal", "server")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, e := range entries {
		m := mountFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if out[m[1]], err = CountRoutes(src); err != nil {
			return nil, err
		}
	}
	return out, nil
}
