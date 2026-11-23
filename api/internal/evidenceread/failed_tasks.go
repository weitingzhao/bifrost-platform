package evidenceread

import (
	"context"
	"encoding/json"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
)

// FailedTasks are the pipeline tasks that failed in a CI run of repo at
// commit, as the run's failed row in the evidence ledger says
// (statement.predicate.failed_tasks). No row of that run that failed, or no
// ledger (db nil), answers none and no error. Until 2026-11-22 the server
// read the ledger for this itself (server/evalreadings_wire.go); it is the
// evidence ledger's read, so it lives here, the domain's public read.
func FailedTasks(ctx context.Context, db eventlog.DB, repo, commit, run string) ([]string, error) {
	if db == nil || repo == "" || commit == "" || run == "" {
		return nil, nil
	}
	rows, err := evidence.EvidenceByCommit(ctx, db, repo, commit, 50)
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		if e.RunName != run || e.Result != evidence.EvidenceFailed {
			continue
		}
		var st struct {
			Predicate struct {
				FailedTasks []string `json:"failed_tasks"`
			} `json:"predicate"`
		}
		if err := json.Unmarshal(e.Statement, &st); err != nil {
			return nil, err
		}
		return st.Predicate.FailedTasks, nil
	}
	return nil, nil
}
