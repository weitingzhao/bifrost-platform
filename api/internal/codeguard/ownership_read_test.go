package codeguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/ownershiptable"
)

// ownershipRaw is the ownership table as one document: the main file with the
// per-domain files of config/domains merged in (ownershiptable.Merge). Every
// check here reads it this way, so moving rows into a domain's file changes
// nothing they see. Origin: 2026-11-22, the map line split the table to stop
// lines colliding in one file (190 commits in seven days).
func ownershipRaw(t *testing.T) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	main, err := os.ReadFile(filepath.Join(root, "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "config", "domains", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, f)
		parts[filepath.ToSlash(rel)] = b
	}
	merged, err := ownershiptable.Merge(main, parts)
	if err != nil {
		t.Fatal(err)
	}
	return merged
}
