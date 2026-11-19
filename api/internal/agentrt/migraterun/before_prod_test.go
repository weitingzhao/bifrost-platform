package migraterun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; the step before a PROD delivery runs the migrations only for a real
// commit, with something pending, that the signed policy covers.
func TestBeforeProdRunsOnlyWhatThePolicyCovers(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for name, tc := range map[string]struct {
		commit     string
		pending    []string
		pendingErr error
		auto       bool
		evaluated  bool
		ran        bool
	}{
		"not a commit":   {commit: "main", pending: []string{"m"}, auto: true},
		"unreadable":     {commit: commit, pendingErr: errors.New("gitea down"), auto: true},
		"nothing to run": {commit: commit, auto: true},
		"not covered":    {commit: commit, pending: []string{"m"}, evaluated: true},
		"covered":        {commit: commit, pending: []string{"m"}, auto: true, evaluated: true, ran: true},
	} {
		var evaluated, ran bool
		BeforeProd{
			Pending: func(context.Context, string) (releasepolicy.MigrationState, error) {
				return releasepolicy.MigrationState{Pending: tc.pending}, tc.pendingErr
			},
			Evaluate: func(_ context.Context, action string, params map[string]any) releasepolicy.Decision {
				evaluated = true
				if action != ActionID || params["commit"] != tc.commit {
					t.Errorf("%s: evaluated %s %v", name, action, params)
				}
				return releasepolicy.Decision{Auto: tc.auto}
			},
			Run: func(_ context.Context, params map[string]any) (any, error) {
				ran = true
				if params["commit"] != tc.commit {
					t.Errorf("%s: ran %v", name, params)
				}
				return nil, nil
			},
		}.Prepare(context.Background(), tc.commit)
		if evaluated != tc.evaluated || ran != tc.ran {
			t.Errorf("%s: evaluated %v, ran %v", name, evaluated, ran)
		}
	}
}
