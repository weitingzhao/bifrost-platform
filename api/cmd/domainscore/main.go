// domainscore prints every domain's score against config/quality-ruler.json
// for the checkout it is run in:
//
//	go run ./cmd/domainscore -repo ..        (from api/)
//
// It reads the code with `go list`, the ownership table and the ruler, and
// writes JSON. It changes nothing in the checkout. For the contracts item it
// type-checks every package of the module (go/types over the export data
// `go list -export` names, which compiles the module into the build cache);
// the deployed platform never runs it.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
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

	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
)

const module = "github.com/weitingzhao/bifrost-platform/"

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
func surface(graph *domainscore.Graph, own []listed, exports map[string]string) (map[string]domainscore.ContractUse, error) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, fmt.Errorf("go list gave no export data for %s", path)
		}
		return os.Open(exports[path])
	})
	s := domainscore.NewSurface(graph, module)
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
		s.Add(strings.TrimPrefix(p.ImportPath, module), fset, info)
	}
	return s.Uses(), nil
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
		if out[m[1]], err = domainscore.CountRoutes(src); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func run() error {
	repo := flag.String("repo", "..", "the repository root")
	flag.Parse()
	root, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	rawRuler, err := os.ReadFile(filepath.Join(root, "config", "quality-ruler.json"))
	if err != nil {
		return err
	}
	ruler, err := domainscore.ParseRuler(rawRuler)
	if err != nil {
		return err
	}
	rawOwn, err := os.ReadFile(filepath.Join(root, "config", "domain-ownership.json"))
	if err != nil {
		return err
	}
	in := domainscore.Input{Ruler: ruler, WallHolds: map[string]bool{}}
	if err = json.Unmarshal(rawOwn, &in.Ownership); err != nil {
		return fmt.Errorf("the ownership table does not parse: %w", err)
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
		return fmt.Errorf("go list: %w", err)
	}
	var own []listed
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p listed
		if err = dec.Decode(&p); err != nil {
			return fmt.Errorf("go list: %w", err)
		}
		exports[p.ImportPath] = p.Export
		if !strings.HasPrefix(p.ImportPath, module) {
			continue
		}
		own = append(own, p)
		pkg := domainscore.Package{Path: strings.TrimPrefix(p.ImportPath, module)}
		for _, dep := range p.Imports {
			if strings.HasPrefix(dep, module) {
				pkg.Imports = append(pkg.Imports, strings.TrimPrefix(dep, module))
			}
		}
		for _, f := range append(p.GoFiles, p.CgoFiles...) {
			pkg.Lines += lines(filepath.Join(p.Dir, f))
		}
		in.Packages = append(in.Packages, pkg)
	}
	if len(in.Packages) < 50 {
		return fmt.Errorf("go list named %d packages", len(in.Packages))
	}
	if in.Contracts, err = surface(domainscore.NewGraph(in.Ownership, in.Packages), own, exports); err != nil {
		return err
	}
	if in.Routes, err = routes(root); err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(domainscore.Score(in))
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "domainscore:", err)
		os.Exit(1)
	}
}
