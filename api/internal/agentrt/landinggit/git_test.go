package landinggit

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const testTask = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Tester", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Tester", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git fixture: %v: %s", e, b)
	}
	return strings.TrimSpace(string(b))
}
