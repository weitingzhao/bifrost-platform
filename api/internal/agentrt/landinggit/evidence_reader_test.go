package landinggit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// R5:5.7-5: one reader answers many pairs exactly as a fresh read does, in
// any order, including R in P, R not in P, P = R, and with task and A.
func TestEvidenceReaderReadsPairsLikeAFreshRead(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	main := gitTest(t, src, "rev-parse", "main")
	repo := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "cherry-pick", "-x", in.A)
	landed := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, "main")
	er, err := g.OpenEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer er.Close()
	gone := strings.Repeat("e", 40)
	// Each pair's answer is fixed by the fixture's graph: main and the
	// accepted branch share only base, and landed is main's cherry-pick of A.
	for _, c := range []struct {
		p, r, task, a string
		has           bool
		state         landing.History
	}{
		{main, in.Base, "", "", true, landing.Unknown}, {main, in.A, "", "", false, landing.Unknown},
		{in.A, in.Base, "", "", true, landing.Unknown}, {in.A, main, "", "", false, landing.Unknown},
		{in.A, in.A, "", "", true, landing.Unknown}, {main, main, "", "", true, landing.Unknown},
		{landed, landed, testTask, in.A, true, landing.Effective}, {landed, landed, testTask, gone, true, landing.Unknown},
		{landed, landed, testTask, in.A, true, landing.Effective}, {landed, in.A, testTask, in.A, false, landing.Unknown},
	} {
		h1, has1, hist1, err1 := er.ReleaseEvidence(ctx, c.p, c.r, c.task, c.a)
		if err1 != nil || h1 != c.p || has1 != c.has || hist1.State != c.state {
			t.Fatalf("P %s R %s A %s: head %s has %v %+v %v; want head P, has %v, %s", c.p[:7], c.r[:7], c.a, h1, has1, hist1, err1, c.has, c.state)
		}
		// A fresh read gives the same answer (it reads through its own reader).
		h2, has2, hist2, err2 := g.ReleaseEvidence(ctx, c.p, c.r, c.task, c.a)
		if h1 != h2 || has1 != has2 || !reflect.DeepEqual(hist1, hist2) || (err1 == nil) != (err2 == nil) {
			t.Fatalf("P %s R %s A %s: reader %s %v %+v %v; fresh %s %v %+v %v", c.p[:7], c.r[:7], c.a, h1, has1, hist1, err1, h2, has2, hist2, err2)
		}
	}
	// An A that does not fetch leaves validity Unknown for that pair only.
	if _, has, hist, err := er.ReleaseEvidence(ctx, landed, landed, testTask, gone); err != nil || !has || hist.State != landing.Unknown {
		t.Fatalf("A gone: has=%v %+v %v", has, hist, err)
	}
	if _, _, hist, err := er.ReleaseEvidence(ctx, landed, landed, testTask, in.A); err != nil || hist.State != landing.Effective {
		t.Fatalf("A back: %+v %v", hist, err)
	}
	if _, _, _, err := er.ReleaseEvidence(ctx, "main", in.Base, "", ""); err == nil {
		t.Fatal("a P that is not a SHA was read")
	}
	if _, _, _, err := er.ReleaseEvidence(ctx, main, "HEAD", "", ""); err == nil {
		t.Fatal("an R that is not a SHA was read")
	}
}

// Every read fetches its P again: a P the remote did not have for one pair
// reads for the next pair once the remote has it (review of R5:5.7-5 slice
// 4, round 1: a P kept from an earlier read must not stop a later fetch).
func TestEvidenceReaderFetchesEachPAgain(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	er, err := g.OpenEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer er.Close()
	if err := os.WriteFile(filepath.Join(src, "later.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, src, "add", "later.txt")
	gitTest(t, src, "commit", "-m", "later")
	later := gitTest(t, src, "rev-parse", "HEAD")
	if _, _, _, err := er.ReleaseEvidence(ctx, later, in.Base, "", ""); err == nil {
		t.Fatal("a P the remote does not have was read")
	}
	gitTest(t, src, "push", filepath.Join(filepath.Dir(src), "remote.git"), "HEAD:refs/heads/later")
	if _, has, _, err := er.ReleaseEvidence(ctx, later, in.Base, "", ""); err != nil || !has {
		t.Fatalf("the same P once the remote has it: has=%v %v", has, err)
	}
}

// A reader that fails to open leaves no workspace; one that opens leaves it
// until Close.
func TestOpenEvidenceOwnsItsWorkspace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	g, _, _ := fixture(t)
	ctx := context.Background()
	left := func() int {
		m, _ := filepath.Glob(filepath.Join(tmp, "agentrt-landing-main-*"))
		return len(m)
	}
	broken := &Git{remote: "http://127.0.0.1:1/remote.git", usernameFile: g.usernameFile, passwordFile: g.passwordFile}
	if _, err := broken.OpenEvidence(ctx); err == nil || left() != 0 {
		t.Fatalf("failed open: err=%v, %d workspaces left", err, left())
	}
	er, err := g.OpenEvidence(ctx)
	if err != nil || left() != 1 {
		t.Fatalf("open: %v, %d workspaces", err, left())
	}
	if err := er.Close(); err != nil || left() != 0 {
		t.Fatalf("close: %v, %d workspaces left", err, left())
	}
}

// Review of R5:5.7-5 slice 4, rounds 1 and 2: an earlier read can leave P in
// the workspace without its history (the remote had only a shallow P). A
// later read of the same P, once the remote has its history, must fetch it
// again and read; a reader that skipped the fetch because P is present would
// keep failing.
func TestEvidenceReaderRefetchesAPWhoseHistoryWasMissing(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	root := filepath.Dir(src)
	remote := filepath.Join(root, "remote.git")
	// side: R, then P on top of it, both only in src so far.
	gitTest(t, src, "checkout", "-q", "-b", "side", in.Base)
	for _, name := range []string{"r", "p"} {
		if err := os.WriteFile(filepath.Join(src, name+".txt"), []byte(name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, src, "add", name+".txt")
		gitTest(t, src, "commit", "-m", name)
	}
	p := gitTest(t, src, "rev-parse", "HEAD")
	r := gitTest(t, src, "rev-parse", "HEAD~1")
	// The remote gets P alone: pushed from a depth-1 clone, so its history
	// stops at P (the remote becomes shallow there).
	shallow := filepath.Join(root, "shallow")
	gitTest(t, root, "clone", "-q", "--depth=1", "--branch=side", "file://"+src, shallow)
	gitTest(t, remote, "config", "receive.shallowUpdate", "true")
	gitTest(t, shallow, "push", "-q", remote, "side:refs/heads/side")
	er, err := g.OpenEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer er.Close()
	if _, _, _, err := er.ReleaseEvidence(ctx, p, r, "", ""); err == nil {
		t.Fatal("a P without its history was read")
	}
	if out := gitTest(t, er.dir, "cat-file", "-t", p); out != "commit" {
		t.Fatalf("the first read did not leave P in the workspace: %q", out)
	}
	// The remote gets P's history. Its shallow branch goes first: while the
	// remote names P, a push assumes it has P's ancestors and sends none.
	gitTest(t, remote, "update-ref", "-d", "refs/heads/side")
	gitTest(t, src, "push", "-q", remote, "side:refs/heads/side")
	if err := os.Remove(filepath.Join(remote, "shallow")); err != nil {
		t.Fatal(err)
	}
	head, has, _, err := er.ReleaseEvidence(ctx, p, r, "", "")
	if err != nil || head != p || !has {
		t.Fatalf("the same P once the remote has its history: head=%s has=%v %v", head, has, err)
	}
	if h2, has2, _, err2 := g.ReleaseEvidence(ctx, p, r, "", ""); err2 != nil || h2 != head || has2 != has {
		t.Fatalf("a fresh read: %s %v %v", h2, has2, err2)
	}
}
