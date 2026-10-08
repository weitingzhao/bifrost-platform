package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecSQLOnPrimarySymbolRemoved is the TD-268 ratchet.
// ExecSQLOnPrimary was removed; the symbol must not reappear anywhere in api/ except this file.
func TestExecSQLOnPrimarySymbolRemoved(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const needle = "ExecSQLOnPrimary"
	const self = "internal/cluster/execsql_callers_test.go"
	var hits []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == self {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, needle) {
				hits = append(hits, rel+":"+strconvItoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("ExecSQLOnPrimary references = %d, want 0: %s", len(hits), strings.Join(hits, ", "))
	}
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
