package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecSQLOnPrimaryCallSitesOnlyDataClone is the TD-256 ratchet.
// Production call sites of ExecSQLOnPrimary outside cluster/data_clone*.go
// must stay at 0 (baseline was 2: marketdata and flexquery freshness probes).
// The count may only fall. The data clone calls execOnPrimary, not this helper;
// a future caller inside data_clone*.go is the only place still allowed.
func TestExecSQLOnPrimaryCallSitesOnlyDataClone(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const needle = ".ExecSQLOnPrimary("
	var outside []string
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		base := filepath.Base(path)
		allowed := strings.HasPrefix(rel, "internal/cluster/") && strings.HasPrefix(base, "data_clone")
		if allowed {
			return nil
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, needle) {
				outside = append(outside, rel+":"+itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outside) != 0 {
		t.Fatalf("ExecSQLOnPrimary call sites outside cluster/data_clone*.go = %d, want 0: %s", len(outside), strings.Join(outside, ", "))
	}
}

func itoa(n int) string {
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
