package landingci

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Origin: R0E (CI line 2026-11-25). A new ui commit is checked with platform's
// main before the stable pin moves: a full run (no base) with uiRevision set
// to that commit, one run per pair, and its status read back by name.
func TestUICheckIsAFullRunWithTheUICommitAndItsStatusIsRead(t *testing.T) {
	t.Parallel()
	c, _ := drillService(t)
	ctx := context.Background()
	name, err := c.EnsureUICheck(ctx, factSHA1, factSHA2)
	if err != nil || name != "ui-check-"+factSHA2[:12]+"-"+factSHA1[:8] {
		t.Fatal(name, err)
	}
	if again, err := c.EnsureUICheck(ctx, factSHA1, factSHA2); err != nil || again != name {
		t.Fatal("the same pair is the same run", again, err)
	}
	params, labels := fullParams(t, c, name)
	want := map[string]string{"revision": factSHA1, "uiRevision": factSHA2, "repo-url": platformRepoURL}
	if !reflect.DeepEqual(params, want) {
		t.Fatal(params)
	}
	if labels["bifrost.io/trigger"] != UICheckTrigger || labels["bifrost.io/ui-revision"] != factSHA2 || labels["bifrost.io/revision"] != factSHA1 {
		t.Fatal(labels)
	}
	if status, err := c.RunStatus(ctx, name); err != nil || status != "Unknown" {
		t.Fatal("a run with no condition yet", status, err)
	}
	if _, err := c.RunStatus(ctx, "ui-check-gone"); !errors.Is(err, ErrRunGone) {
		t.Fatal("a missing run", err)
	}
	if _, err := c.EnsureUICheck(ctx, "main", factSHA2); err == nil {
		t.Fatal("a branch name was accepted")
	}
}
