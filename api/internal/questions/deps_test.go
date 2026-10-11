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

func TestThosePackagesDoNotDependOnQuestions(t *testing.T) {
	for _, pkg := range []string{
		"./internal/actions/...",
		"./internal/approvals/...",
		"./internal/approvalnotify/...",
		"./internal/releasepolicy/...",
	} {
		out := goList(t, "-deps", pkg)
		for _, line := range strings.Split(out, "\n") {
			if strings.HasSuffix(line, "/internal/questions") {
				t.Fatalf("%s depends on questions", pkg)
			}
		}
	}
}

func TestServerNamesQuestionsOnlyInTheWireFile(t *testing.T) {
	dir := filepath.Join("..", "server")
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
	if len(hits) != 1 || hits[0] != "questions_wire.go" {
		t.Fatalf("files containing questions. = %v", hits)
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
