package cluster

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRemovedPrimarySQLHelperStaysGone guards TD-268: the removed helper's psql
// exec into the CNPG primary invited quick reads that entrenched the privilege.
// The HTTP path is the only one now for those reads.
func TestRemovedPrimarySQLHelperStaysGone(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate package directory")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	needle := []byte("ExecSQL" + "OnPrimary")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "dist", "build", "bin", ".vite", ".next", ".docker-build":
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for i, line := range bytes.Split(contents, []byte{'\n'}) {
			if bytes.Contains(line, needle) {
				t.Errorf("%s:%d: removed primary SQL helper reference", filepath.ToSlash(rel), i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
