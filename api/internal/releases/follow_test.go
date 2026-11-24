package releases

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TD-588 (2026-11-23): a unit plan's pin with advance stg follows the commit
// the current STG release shipped. While the release cannot be read (a
// delivery in flight, the table down) the last commit holds, so one commit's
// plans do not flip back and forth during a release.
// Positive control: without the cache, the second read below answers "".
func TestFollowGivesTheReleasesCommitAndKeepsItWhileUnreadable(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC)
	s, _ := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	s.Tick(ctx)
	rows := &fakeRows{}
	s.SetTableReader(rows)
	s.Tick(ctx)
	now := t1.Add(time.Hour)
	s.now = func() time.Time { return now }

	follow := s.Follow("app", "stg")
	if got, from := follow("core"); got != shaA || from != "stg release deliver-stg-1" {
		t.Fatalf("follow(core) = %q %q", got, from)
	}
	if got, _ := follow("ui"); got != "" {
		t.Fatalf("a repository the release did not build = %q", got)
	}
	rows.err = errors.New("database down")
	now = now.Add(2 * followFresh)
	if got, _ := follow("core"); got != shaA {
		t.Fatalf("while unreadable = %q, want the last commit", got)
	}

	// Never read: nothing to give.
	if got, _ := s.Follow("app", "stg")("core"); got != "" {
		t.Fatalf("a follower that never read = %q", got)
	}
}
