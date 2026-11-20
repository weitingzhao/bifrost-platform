package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// Origin: 0.2 map line, 2026-11-20. aeeff266 shipped config/catalog-review.json
// with "repositories" one level short (repository → review instead of
// repository → path → review); the unit tests used the right shape, the real
// file did not parse on STG, and every verdict of the pool was dropped. The
// files the scan reads from the platform's own repository must parse here.
func TestTheRepositorysOwnMapFilesParse(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ReviewPath))
	if err != nil {
		t.Fatal(err)
	}
	own, others, err := ParseReviewFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(own) == 0 || len(others) == 0 {
		t.Fatalf("%s: %d reviews of its own and %d repositories reviewed; both are written", ReviewPath, len(own), len(others))
	}
	if raw, err = os.ReadFile(filepath.Join(root, IndexPath)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseIndex(raw, "", nil, Table{}); err != nil {
		t.Fatal(err)
	}
}
