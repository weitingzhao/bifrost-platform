package devsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionsCatalog_RepoFile(t *testing.T) {
	// Resolve repo config/ relative to this test file's package → api/internal/devsession → ../../..
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	configDir := filepath.Join(root, "config")
	if _, err := os.Stat(filepath.Join(configDir, sessionsCatalogFile)); err != nil {
		t.Skip("repo sessions-catalog.yaml not present")
	}
	cat, err := LoadSessionsCatalog(configDir)
	if err != nil {
		t.Fatal(err)
	}
	stg := cat.EntriesForEnv("stg")
	prod := cat.EntriesForEnv("prod")
	if len(stg) < 5 || len(prod) < 5 {
		t.Fatalf("expected focused catalogs, got stg=%d prod=%d", len(stg), len(prod))
	}
	platform := cat.Lookup("stg", "platform")
	if platform == nil || platform.Deployment != "platform-api" {
		t.Fatalf("platform mapping: %+v", platform)
	}
	if platform.Namespace != "bifrost-platform-stg" {
		t.Fatalf("platform ns=%q", platform.Namespace)
	}
	prodPlatform := cat.Lookup("prod", "platform")
	if prodPlatform == nil || prodPlatform.Namespace != "bifrost-platform-prod" {
		t.Fatalf("prod platform: %+v", prodPlatform)
	}
}
