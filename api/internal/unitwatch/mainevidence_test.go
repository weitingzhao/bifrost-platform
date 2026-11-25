package unitwatch

import (
	"context"
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// Origin: Foundation's reading of unit evidence, 2026-11-25: a main head had
// 73 of 165 tasks with evidence, because only the daily run and agents asking
// by hand started units, and main moved a dozen times that day. A head is now
// advanced when main moves; one whose units could not start yet is tried again.
func TestMainEvidenceAdvancesEachHeadOnceAndRetriesAllDeferred(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var advanced []string
	answer := unitexec.Advanced{Created: []string{"u-1"}, Deferred: 2}
	m := &MainEvidence{
		Head: func(context.Context, string) (string, error) { return head, nil },
		Advance: func(_ context.Context, repo, commit string) (unitexec.Advanced, error) {
			if repo != "bifrost-platform" {
				t.Fatal(repo)
			}
			advanced = append(advanced, commit)
			return answer, nil
		},
	}
	ctx := context.Background()
	if err := m.Pass(ctx); err != nil || len(advanced) != 1 {
		t.Fatal("a new head is advanced", err, advanced)
	}
	if err := m.Pass(ctx); err != nil || len(advanced) != 1 {
		t.Fatal("the same head is not advanced again (the receiving pass continues it)", err, advanced)
	}
	head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	answer = unitexec.Advanced{Deferred: 5}
	if err := m.Pass(ctx); !errors.Is(err, ErrAllDeferred) {
		t.Fatal("a head none of whose units started answers an error, so the watch retries", err)
	}
	answer = unitexec.Advanced{Created: []string{"u-2"}}
	if err := m.Pass(ctx); err != nil || len(advanced) != 3 || advanced[2] != head {
		t.Fatal("the retry advances the head again", err, advanced)
	}
	// Positive control: a head with nothing to run is done, not retried.
	head = "cccccccccccccccccccccccccccccccccccccccc"
	answer = unitexec.Advanced{}
	if err := m.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Pass(ctx); err != nil || len(advanced) != 4 {
		t.Fatal("a head with nothing to run is advanced once", err, advanced)
	}
}

func TestMainEvidenceKeepsAHeadWhoseAdvanceFailed(t *testing.T) {
	calls := 0
	m := &MainEvidence{
		Head: func(context.Context, string) (string, error) { return "dddddddddddddddddddddddddddddddddddddddd", nil },
		Advance: func(context.Context, string, string) (unitexec.Advanced, error) {
			calls++
			if calls == 1 {
				return unitexec.Advanced{}, unitexec.ErrNoNode
			}
			return unitexec.Advanced{}, nil
		},
	}
	if err := m.Pass(context.Background()); !errors.Is(err, unitexec.ErrNoNode) {
		t.Fatal(err)
	}
	if err := m.Pass(context.Background()); err != nil || calls != 2 {
		t.Fatal("a failed advance is tried again", err, calls)
	}
}
