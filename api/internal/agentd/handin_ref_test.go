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

// Review 342 A3: a check whose output names another task's workspace (a
// test can print one) hands in its outputs withheld, as for a local path:
// the platform takes it, and the path is not passed on to the review.
func TestHandInWithholdsAnotherTasksWorkspace(t *testing.T) {
	other := "tasks/t_01M5AJ2GMV6RX45QVQ2FGNXYXV/a1/secret.txt"
	v := Verification{Pass: true, Commands: []CommandTrace{{Argv: []string{"go", "test", "./..."}, Tail: "ok\nread " + other + " before reviewing\n"}}}
	out, err := handIn(v, &pathRewrite{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), other) || !strings.Contains(string(out), "withheld") {
		t.Fatalf("handIn passed another task's workspace on:\n%s", out)
	}
	if ref, found := store.WorkspaceRef("verify", out); found {
		t.Fatalf("the platform would refuse what agentd hands in: %q", ref)
	}
	var back Verification
	if err := json.Unmarshal(out, &back); err != nil || !back.Pass {
		t.Fatalf("the verdict did not survive: %+v %v", back, err)
	}
}
