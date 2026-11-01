package landinggit

import (
	"context"
	"errors"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// changeFixture is a remote served over smart HTTP by git-http-backend and a
// source repository; commit makes a commit on parent ("" for a root commit)
// with files written as given, pushes it to a branch of its own and returns
// its SHA.
func changeFixture(t *testing.T) (*Git, func(parent string, files map[string]string, msg string) string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", repo)
	src := filepath.Join(root, "source")
	gitTest(t, root, "init", "-b", "main", src)
	n := 0
	commit := func(parent string, files map[string]string, msg string) string {
		t.Helper()
		if parent == "" {
			gitTest(t, src, "checkout", "--orphan", "root-"+msg)
			gitTest(t, src, "rm", "-rf", "--ignore-unmatch", ".")
		} else {
			gitTest(t, src, "checkout", "--detach", parent)
		}
		for name, content := range files {
			if e := os.WriteFile(filepath.Join(src, name), []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			gitTest(t, src, "add", name)
		}
		gitTest(t, src, "commit", "-m", msg)
		sha := gitTest(t, src, "rev-parse", "HEAD")
		n++
		gitTest(t, src, "push", repo, sha+":refs/heads/agent/fixture/"+strings.Repeat("x", n))
		return sha
	}
	execPath := gitTest(t, src, "--exec-path")
	server := httptest.NewServer(&cgi.Handler{Path: filepath.Join(execPath, "git-http-backend"), Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	t.Cleanup(server.Close)
	u, p := filepath.Join(root, "username"), filepath.Join(root, "password")
	if e := os.WriteFile(u, []byte("change-fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("fixture-only-password"), 0600); e != nil {
		t.Fatal(e)
	}
	return &Git{remote: server.URL + "/remote.git", usernameFile: u, passwordFile: p}, commit
}

// DESIGN-step-landed-superseded-2026-11-01 §6 item 2: two accepted results
// carry the same change only when the verbatim patch-id and the raw diff's
// sha256 both agree, each read from the two commits alone.
func TestChangesIdentity(t *testing.T) {
	g, commit := changeFixture(t)
	ctx := context.Background()
	config := "A: 0\nB: 0\nC: 0\nD: 0\n"
	base := commit("", map[string]string{"config.yaml": config, "other.txt": "one\n"}, "base")
	compare := func(a, b string) (ChangeIdentity, ChangeIdentity) {
		t.Helper()
		x, y, err := g.Changes(ctx, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if x.PatchID == "" || len(x.DiffSHA256) != 64 || y.PatchID == "" || len(y.DiffSHA256) != 64 {
			t.Fatalf("identity not read: %+v %+v", x, y)
		}
		return x, y
	}

	// The same change, handed in twice (other message, so another commit).
	old := commit(base, map[string]string{"config.yaml": config + "E: 1\n"}, "old hand-in")
	same := commit(base, map[string]string{"config.yaml": config + "E: 1\n"}, "new hand-in")
	if old == same {
		t.Fatal("fixture: the two hand-ins are one commit")
	}
	if x, y := compare(old, same); !SameChange(x, y) {
		t.Fatalf("the same change: %+v %+v", x, y)
	}

	// YAML indentation only: --stable would call these the same patch.
	indented := commit(base, map[string]string{"config.yaml": config + "  E: 1\n"}, "indented")
	if x, y := compare(old, indented); SameChange(x, y) || x.PatchID == y.PatchID {
		t.Fatalf("an indentation change is another change: %+v %+v", x, y)
	}

	// Whitespace inside a line.
	spaced := commit(base, map[string]string{"config.yaml": config + "E:  1\n"}, "spaced")
	if x, y := compare(old, spaced); SameChange(x, y) || x.PatchID == y.PatchID {
		t.Fatalf("whitespace inside a line is another change: %+v %+v", x, y)
	}

	// The same change on another base whose touched file is the same: the
	// diff is byte for byte the same.
	moved := commit(base, map[string]string{"other.txt": "two\n"}, "main moved elsewhere")
	rebased := commit(moved, map[string]string{"config.yaml": config + "E: 1\n"}, "rebased hand-in")
	if x, y := compare(old, rebased); !SameChange(x, y) {
		t.Fatalf("the same change on another base: %+v %+v", x, y)
	}

	// The same lines added with the same context at another line (lines
	// inserted above it, out of the hunk): the patch-id ignores position,
	// but the raw diff (hunk header, blob ids) differs, so §6 item 2's
	// digest keeps them apart.
	above := "X: 1\nX: 2\nX: 3\nX: 4\nX: 5\n"
	shifted := commit(base, map[string]string{"config.yaml": above + config}, "context shifted")
	onShifted := commit(shifted, map[string]string{"config.yaml": above + config + "E: 1\n"}, "hand-in on shifted context")
	x, y := compare(old, onShifted)
	if x.PatchID != y.PatchID {
		t.Fatalf("patch-id should ignore where the lines sit: %+v %+v", x, y)
	}
	if SameChange(x, y) {
		t.Fatalf("a diff with other bytes is not the same change: %+v %+v", x, y)
	}

	// A different change.
	other := commit(base, map[string]string{"config.yaml": config + "E: 2\n"}, "other")
	if x, y := compare(old, other); SameChange(x, y) {
		t.Fatalf("a different change: %+v %+v", x, y)
	}
}

// A result Git cannot read is an error, never an identity. One that reading
// again cannot change is ErrNotComparable (a name for a SHA, a root commit,
// a diff over the output bound); a commit the answering remote does not
// serve is ErrNotServed, inferred and so a not-comparable worth reading
// again later; a remote that does not answer is a plain error.
// Serial: it lowers outputMax.
func TestChangesFailures(t *testing.T) {
	g, commit := changeFixture(t)
	ctx := context.Background()
	base := commit("", map[string]string{"f.txt": "a\n"}, "base")
	change := commit(base, map[string]string{"f.txt": "b\n"}, "change")
	big := commit(base, map[string]string{"f.txt": strings.Repeat("a line of the change\n", 200)}, "big")
	if _, _, err := g.Changes(ctx, change, strings.Repeat("e", 40)); !errors.Is(err, ErrNotServed) || !errors.Is(err, ErrNotComparable) {
		t.Errorf("a commit the remote does not have: %v, want ErrNotServed", err)
	}
	for name, b := range map[string]string{
		"a name for a SHA": "main",
		"an empty SHA":     "",
		"a root commit":    base,
	} {
		if _, _, err := g.Changes(ctx, change, b); !errors.Is(err, ErrNotComparable) || errors.Is(err, ErrNotServed) {
			t.Errorf("%s: %v, want ErrNotComparable, not ErrNotServed", name, err)
		}
	}
	old := outputMax
	outputMax = 1024
	_, _, err := g.Changes(ctx, change, big)
	outputMax = old
	if !errors.Is(err, ErrNotComparable) || errors.Is(err, ErrNotServed) {
		t.Errorf("a diff over the output bound: %v, want ErrNotComparable, not ErrNotServed", err)
	}
	if _, _, err := g.Changes(ctx, change, big); err != nil {
		t.Fatalf("the same diff under the bound: %v", err)
	}
	down := &Git{remote: "http://127.0.0.1:1/remote.git", usernameFile: g.usernameFile, passwordFile: g.passwordFile}
	if _, _, err := down.Changes(ctx, change, big); err == nil || errors.Is(err, ErrNotComparable) {
		t.Errorf("a remote that does not answer: %v, want a plain error", err)
	}
	if SameChange(ChangeIdentity{DiffSHA256: "x"}, ChangeIdentity{DiffSHA256: "x"}) {
		t.Fatal("a result with no change is the same as another")
	}
}
