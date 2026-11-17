// domainscore prints every domain's score against config/quality-ruler.json
// for the checkout it is run in:
//
//	go run ./cmd/domainscore -repo ..        (from api/)
//
// It reads the code with `go list`, the ownership table and the ruler, and
// writes JSON. It changes nothing.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("go", "list", "-json=ImportPath,Dir,GoFiles,CgoFiles,Imports", "./...")
	cmd.Dir = filepath.Join(root, "api")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath, Dir            string
			GoFiles, CgoFiles, Imports []string
		}
		if err = dec.Decode(&p); err != nil {
			return fmt.Errorf("go list: %w", err)
		}
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
