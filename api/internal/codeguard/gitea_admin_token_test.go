// Package codeguard holds the guards that read every Go file of the module:
// a name or a pattern that must not appear anywhere in the platform's code.
// They live apart from the packages they protect so that those packages'
// tests are rerun for their own changes, not for every Go change (moved from
// delivery on 2026-11-14, when its tests stopped declaring the whole module).
package codeguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoGiteaAdminTokenInPlatformCode keeps the administrator's Secret and
// key out of every non-test Go file of the module (TD-432 ratchet): the
// platform has its own read-only account and must not learn the other.
func TestNoGiteaAdminTokenInPlatformCode(t *testing.T) {
	root, absErr := filepath.Abs(filepath.Join("..", ".."))
	if absErr != nil {
		t.Fatal(absErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("module root not found at %s: %v", root, statErr)
	}
	banned := []string{"gitea-" + "bootstrap", "gitea_admin" + "_token"}
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, b := range banned {
			if strings.Contains(string(raw), b) {
				t.Errorf("%s names %q: the platform reads Gitea with its own reader only (TD-432)", path, b)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if scanned < 50 {
		t.Fatalf("scanned %d Go files under %s; the walk is not seeing the module", scanned, root)
	}
}
