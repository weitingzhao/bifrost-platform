package questions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoesNotDependOnActionsOrApprovals(t *testing.T) {
	out := goList(t, "-deps", "./internal/questions/...")
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, "/internal/actions") ||
			strings.HasSuffix(line, "/internal/approvals") ||
			strings.HasSuffix(line, "/internal/approvalnotify") ||
			strings.HasSuffix(line, "/internal/releasepolicy") {
			t.Fatalf("questions depends on %s", line)
		}
	}
}

// The questions are mounted by the owner's assembly (owner/assembly) since
// 2026-11-19, which took the line from server's questions_wire.go: server
// names the package nowhere, and the assembly in its one file.
func TestServerNamesQuestionsOnlyInTheWireFile(t *testing.T) {
	names := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var hits []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "questions.") {
				hits = append(hits, e.Name())
			}
		}
		return hits
	}
	if hits := names(filepath.Join("..", "server")); len(hits) != 0 {
		t.Fatalf("server files containing questions. = %v", hits)
	}
	if hits := names(filepath.Join("..", "owner", "assembly")); len(hits) != 1 || hits[0] != "assembly.go" {
		t.Fatalf("owner assembly files containing questions. = %v", hits)
	}
}

func goList(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
