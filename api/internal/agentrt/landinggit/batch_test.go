package landinggit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

const testTask2 = "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"

// R5:5.7 goal 2 (landing/batch.go): a batch replays its members in one
// workspace, each on the one before, and its grant material reads R as the
// batch's stack on M0: the patch is the whole change, the match the lead's
// own. A member that rewrites a line the stack already changed conflicts.
func TestBatchStackReplayAndPatch(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	put := func(name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(src, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	// A second result on the same base, another file, another task.
	gitTest(t, src, "checkout", "-q", in.Base)
	put("second.txt", "second\n")
	gitTest(t, src, "add", "second.txt")
	gitTest(t, src, "commit", "-m", "second\n\nAgentrt-Task: "+testTask2+"\nChange-Id: I"+strings.Repeat("b", 40))
	second := gitTest(t, src, "rev-parse", "HEAD")
	// A third that changes the lead's file again.
	gitTest(t, src, "checkout", "-q", in.Base)
	put("display.txt", "third\n")
	gitTest(t, src, "add", "display.txt")
	gitTest(t, src, "commit", "-m", "third\n\nAgentrt-Task: "+testTask2+"\nChange-Id: I"+strings.Repeat("c", 40))
	third := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", filepath.Join(filepath.Dir(src), "remote.git"), second+":refs/heads/second", third+":refs/heads/third")

	replay := func() (*Workspace, string, string) {
		t.Helper()
		w, e := g.Open(ctx, in.A)
		if e != nil {
			t.Fatal(e)
		}
		r1, e := w.Replay(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		if e = w.Fetch(ctx, second); e != nil {
			t.Fatal(e)
		}
		next := in
		next.A, next.M0, next.Task, next.Operation = second, r1, testTask2, "land:"+testTask2+":1"
		r2, e := w.Replay(ctx, next)
		if e != nil {
			t.Fatal(e)
		}
		return w, r1, r2
	}
	w, r1, r2 := replay()
	if parent := gitTest(t, w.dir, "rev-parse", r2+"^"); parent != r1 {
		t.Fatal("the second member is not on the first", parent, r1)
	}
	if e := w.Fetch(ctx, third); e != nil {
		t.Fatal(e)
	}
	over := in
	over.A, over.M0, over.Task, over.Operation = third, r2, testTask2, "land:"+testTask2+":2"
	if _, e := w.Replay(ctx, over); !errors.Is(e, ErrConflict) {
		t.Fatal("a member that rewrites the line the stack changed does not conflict", e)
	}
	ref := "refs/heads/land/stg/" + testTask2 + "/1"
	if e := w.Push(ctx, ref, r2); e != nil {
		t.Fatal(e)
	}
	if e := w.Fetch(ctx, "0000000000000000000000000000000000000000x"); e == nil {
		t.Fatal("fetched what is not a commit")
	}
	_ = w.Close()
	// Recovery replays the same stack to the same R.
	again, _, r2again := replay()
	_ = again.Close()
	if r2again != r2 {
		t.Fatal("the stack's R changed on recovery", r2, r2again)
	}
	p, e := g.CandidatePatch(ctx, in.A, in.M0, r2)
	if e != nil {
		t.Fatal(e)
	}
	if !p.Match || !strings.Contains(p.Shown, "second.txt") || !strings.Contains(p.Shown, "display.txt") {
		t.Fatalf("batch patch: match %v\n%s", p.Match, p.Shown)
	}
	// R not on M0 at all is refused.
	if _, e := g.CandidatePatch(ctx, in.A, r1, in.A); e == nil {
		t.Fatal("a commit not on M0 read as a batch")
	}
}

// A batch lands its members as a stack whose top is every member's R. A
// member other than the last reads Effective by its own commit below R; one
// whose commit is above the R asked about does not (2026-11-10: M-374 w2 and
// M-377 v1 read unknown, the reconciler carded them, B5 waited on them).
func TestMainEvidenceOfABatchMember(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	repo := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "checkout", "-q", in.Base)
	if e := os.WriteFile(filepath.Join(src, "second.txt"), []byte("second\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitTest(t, src, "add", "second.txt")
	gitTest(t, src, "commit", "-m", "second\n\nAgentrt-Task: "+testTask2+"\nChange-Id: I"+strings.Repeat("b", 40))
	second := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, second+":refs/heads/second")
	gitTest(t, src, "checkout", "-q", "main")
	gitTest(t, src, "cherry-pick", "-x", in.A)
	lead := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "cherry-pick", "-x", second)
	top := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, "main")
	if _, has, hist, err := g.MainEvidence(ctx, top, testTask, in.A); err != nil || !has || hist.State != landing.Effective || hist.Commit != lead {
		t.Fatalf("the lead below R: has=%v %+v %v", has, hist, err)
	}
	if _, has, hist, err := g.MainEvidence(ctx, top, testTask2, second); err != nil || !has || hist.State != landing.Effective || hist.Commit != top {
		t.Fatalf("the last member at R: has=%v %+v %v", has, hist, err)
	}
	if _, has, hist, err := g.MainEvidence(ctx, lead, testTask2, second); err != nil || !has || hist.State != landing.Unknown {
		t.Fatalf("a member above the R asked about: has=%v %+v %v", has, hist, err)
	}
}
