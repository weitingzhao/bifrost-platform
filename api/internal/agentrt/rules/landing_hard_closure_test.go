package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/weitingzhao/bifrost-platform/api/"

type listedPackage struct {
	ImportPath string
	Imports    []string
	DepOnly    bool
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}
func listDependencies(t *testing.T, dir string, args ...string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list", "-deps", "-json"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v: %s", err, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out []listedPackage
	for {
		var p listedPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// §4.2 excludes mounted business features, not injected trust providers.
// Each retained edge names its consumer so this list cannot silently stand
// for every import. The closure roots import no business feature now, so no
// edge is exempt. These exclusions do not prove isolation from arbitrary
// same-process code.
var assemblyEdges = map[string]map[string]string{}

func assemblyEdge(from, to string) bool {
	return assemblyEdges[strings.TrimPrefix(from, modulePath)][strings.TrimPrefix(to, modulePath+"internal/")] != ""
}
func uncoveredDependencies(pkgs []listedPackage, prefix string, protected func(string) bool, exempt func(string, string) bool) []string {
	graph := map[string]listedPackage{}
	var queue []string
	for _, p := range pkgs {
		graph[p.ImportPath] = p
		if !p.DepOnly {
			queue = append(queue, p.ImportPath)
		}
	}
	seen := map[string]bool{}
	var failures []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !protected(name) {
			failures = append(failures, "root: "+name)
		}
		for _, dep := range graph[name].Imports {
			if !strings.HasPrefix(dep, prefix) {
				continue
			}
			if exempt(name, dep) {
				continue
			}
			if !protected(dep) {
				failures = append(failures, name+" -> "+dep)
				continue
			}
			queue = append(queue, dep)
		}
	}
	return failures
}

// T19 uses a real temporary Go module. Adding a non-test dependency to the
// protected root makes the same go-list closure checker reject it.
func TestLandingHardClosureDetectsNewDependency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(p, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/closure\n\ngo 1.25.0\n")
	write("root/root.go", "package root\n")
	check := func() []string {
		return uncoveredDependencies(listDependencies(t, dir, "./root"), "example.test/closure/", func(p string) bool { return p == "example.test/closure/root" }, func(string, string) bool { return false })
	}
	if got := check(); len(got) != 0 {
		t.Fatal("control", got)
	}
	write("dep/dep.go", "package dep\n")
	write("root/root.go", "package root\nimport _ \"example.test/closure/dep\"\n")
	if got := check(); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "/dep") {
		t.Fatal("new dependency not caught", got)
	}
}
