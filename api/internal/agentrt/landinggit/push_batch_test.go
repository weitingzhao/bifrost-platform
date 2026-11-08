package landinggit

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchPushRequiresExactLinearReplayChain(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	remote := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "checkout", "-q", "-b", "batch-candidate", in.M0)
	gitTest(t, src, "commit", "--allow-empty", "-m", "member one")
	gitTest(t, src, "commit", "--allow-empty", "-m", "member two")
	r := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", remote, r+":refs/heads/land/prod/"+testTask+"/1")
	lander := &Git{remote: g.remote, usernameFile: g.usernameFile, passwordFile: g.passwordFile, scope: liveRef, lander: true}
	w, err := lander.Open(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	deadline := func() time.Time { return time.Now().Add(PushTimeout) }
	if got := w.PushMain(ctx, r, in.M0, deadline()); got != PushNotSent {
		t.Fatalf("single path accepted batch: %s", got)
	}
	for _, count := range []int{0, 1, 3} {
		if got := w.PushBatchMain(ctx, r, in.M0, deadline(), count); got != PushNotSent {
			t.Fatalf("wrong count %d accepted: %s", count, got)
		}
	}
	if got := w.PushBatchMain(ctx, r, in.Base, deadline(), 2); got != PushNotSent {
		t.Fatalf("wrong base accepted: %s", got)
	}
	if got := w.PushBatchMain(ctx, r, in.M0, time.Now().Add(-time.Second), 2); got != PushNotSent {
		t.Fatalf("expired push: %s", got)
	}
	if got := w.PushBatchMain(ctx, r, in.M0, deadline(), 2); got != PushOK {
		t.Fatalf("valid two-member batch refused: %s", got)
	}
	if got := gitTest(t, remote, "rev-parse", "refs/heads/main"); got != r {
		t.Fatalf("main %s want %s", got, r)
	}
}
