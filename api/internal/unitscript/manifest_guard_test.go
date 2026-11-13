package unitscript

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// outsideReader is what a test file looks like when its tests may read
// something its package's key does not hold: a path that walks up, the
// working directory or the caller's file, a directory walk, a command, Go's
// own parsers, or a file opened by a path. It errs on the side of naming a
// package that reads nothing outside: the cost of that is a test run, the
// cost of the other mistake is a green that tested nothing new.
var outsideReader = regexp.MustCompile(`"\.\.[/"]|\.\./|os\.Getwd\(|runtime\.Caller\(|go/packages|packages\.Load|parser\.ParseDir|go/build|` +
	`filepath\.Walk|WalkDir\(|os\.ReadDir\(|exec\.Command|os\.DirFS\(|go/parser|parser\.ParseFile|os\.ReadFile\(|os\.Open\(|os\.Stat\(`)

// Every package whose test files look like that is declared in
// bifrost.units.yaml with the inputs its tests read ("." until they are
// named). A new package with such a test fails here until it is declared;
// an entry may be narrowed to what the package really reads, never dropped
// while the test still reads outside.
func TestManifestDeclaresTheInputsOfOutsideReaders(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	doc, err := os.ReadFile(filepath.Join(root, unitplan.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := unitplan.ParseManifest(doc)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, mod := range manifest.Modules {
		for dir, inputs := range mod.Inputs {
			declared[dir] = len(inputs) > 0
		}
	}
	flagged := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(root, "api"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if outsideReader.Match(body) {
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			flagged[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for dir := range flagged {
		if !declared[dir] {
			missing = append(missing, dir)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these packages have a test that may read outside the package and no inputs in %s (add each under modules[api].inputs, with \".\" or what it reads):\n  %s",
			unitplan.ManifestPath, strings.Join(missing, "\n  "))
	}
	if len(flagged) < 30 {
		t.Fatalf("only %d packages look like outside readers: the search is not seeing the tests", len(flagged))
	}
}
