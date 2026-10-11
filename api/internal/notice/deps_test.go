package notice

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoticeDoesNotDependOnActionsOrApprovals fails if this package imports
// actions or approvals, directly or through another package. go list -deps
// is the same list the acceptance grep checks.
func TestNoticeDoesNotDependOnActionsOrApprovals(t *testing.T) {
	root := moduleRoot(t)
	cmd := exec.Command("go", "list", "-deps", "./internal/notice/...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "internal/notice") {
		t.Fatalf("go list did not mention notice:\n%s", out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, "internal/actions") || strings.HasSuffix(line, "internal/approvals") {
			t.Errorf("notice depends on %s", line)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
