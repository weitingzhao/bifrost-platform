package agentd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T11 against what agentd hands in: a check in agentd's layout whose failing
// command prints its own checkout, its task's directory (a sibling) and its
// output's temporary directory, as tests do, hands in a verify.json the
// platform takes, and that a review quoting it word for word is taken too.
// The platform refuses what names another task's workspace; agentd writes
// these as ".", ".." and "../a<n>.out", which name none.
func TestHandedInVerifyNamesNoWorkspace(t *testing.T) {
	root := t.TempDir()
	checkout := WorkspaceDir(root, "t_01M57YSQN6B43ZFNESD9VQ2HA9", 1)
	for _, d := range []string{checkout, checkout + ".out/tmp/TestParse1/001", filepath.Join(filepath.Dir(checkout), "bifrost-ui")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(checkout)
	print := strings.Join([]string{
		`echo "--- FAIL: TestParse"`,
		`echo "open ` + checkout + `.out/tmp/TestParse1/001/in.json: no such file or directory"`,
		`echo "` + checkout + `/api/parse_test.go:12"`,
		`echo "` + filepath.Dir(checkout) + `/bifrost-ui/src/b.ts"`,
		`exit 1`,
	}, "; ")
	body, err := json.Marshal(map[string]any{"text": "t", "verify": [][]string{{"sh", "-c", print}}})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	if err = os.WriteFile(specPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "verify.json")
	if rc := RunVerify([]string{"--spec", specPath, "--out", out}); rc != 1 {
		t.Fatalf("RunVerify exited %d, want 1 (a failed check)", rc)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "../a1.out") || !strings.Contains(string(got), "../bifrost-ui") {
		t.Fatalf("the output's temporary directory is not in what is handed in, so this proves nothing:\n%s", got)
	}
	for _, kind := range []string{"verify", "review", "report"} {
		if p, found := store.LocalPath(kind, got); found {
			t.Errorf("as a %s it holds a local path: %q", kind, p)
		}
		if ref, found := store.WorkspaceRef(kind, got); found {
			t.Errorf("as a %s it names a workspace: %q\n%s", kind, ref, got)
		}
	}
}
