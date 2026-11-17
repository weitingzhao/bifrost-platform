package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
)

const externalDeliveryRepo = "bifrost-platform"

// ExternalDelivery is everything FoldExternalDelivery reads.
type ExternalDelivery struct {
	// Work is the external work, Registrant who registered it, Repo the
	// repository its description names.
	Work, Registrant, Repo, Commit string
	// NamedAt is when the work's earliest event naming Commit was recorded:
	// a heartbeat, or the submitted event.
	NamedAt time.Time
	// Runs are the recorded pipeline runs: nil when they were not read, an
	// empty non-nil slice when they were read and hold none.
	Runs       []PipelineRun
	Production *deliveryview.DeliveryProduction
}

// FoldExternalDelivery adapts recorded observations to the pure delivery view.
func FoldExternalDelivery(in ExternalDelivery) []deliveryview.DeliveryStage {
	var runs []deliveryview.Run
	// nil means unread; a non-nil empty slice means read and no runs found.
	if in.Runs != nil {
		runs = make([]deliveryview.Run, len(in.Runs))
		for i, r := range in.Runs {
			runs[i] = deliveryview.Run{Run: r.Run, Pipeline: r.Pipeline, Kind: r.Kind, Result: r.Result, Revision: r.Revision,
				CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
		}
	}
	return deliveryview.FoldExternalDelivery(deliveryview.ExternalDelivery{
		Work: in.Work, Registrant: in.Registrant, Repo: in.Repo, Commit: in.Commit, NamedAt: in.NamedAt,
		Runs: runs, Production: in.Production,
	})
}

// externalDeliveryMatch is one external work naming a commit: the note of
// its earliest event that carries the commit, and when that was recorded.
type externalDeliveryMatch struct {
	note   externalwork.ExternalNote
	commit string
	at     time.Time
}

// externalDeliveryCommit finds the external work naming a commit starting
// with prefix: the earliest event of any work that carries it, a heartbeat
// or the submitted event (a work's note keeps every commit named so far, so
// each work counts once per commit, at its first event carrying it). Two
// different commits under one prefix are ErrInvalid naming both; none is
// pgx.ErrNoRows.
func externalDeliveryCommit(ctx context.Context, tx pgx.Tx, prefix string) (externalDeliveryMatch, error) {
	rows, err := tx.Query(ctx, `SELECT body, sha, at FROM (SELECT DISTINCT ON (e.event_stream_id, s.sha)
	  e.event_id, e.body_json->'external_work' AS body, s.sha, e.at FROM agentrt.event e
	  CROSS JOIN LATERAL jsonb_array_elements_text(e.body_json->'external_work'->'commit_shas') AS s(sha)
	  WHERE e.event_stream_id LIKE 'external-work:%' AND e.event_type = 'note'
	  AND jsonb_typeof(e.body_json->'external_work'->'commit_shas') = 'array'
	  AND left(s.sha, length($1)) = $1 ORDER BY e.event_stream_id, s.sha, e.seq) x
	  ORDER BY event_id, sha LIMIT 50`, prefix)
	if err != nil {
		return externalDeliveryMatch{}, err
	}
	defer rows.Close()
	var best externalDeliveryMatch
	found := false
	for rows.Next() {
		var raw []byte
		var m externalDeliveryMatch
		if err = rows.Scan(&raw, &m.commit, &m.at); err != nil {
			return externalDeliveryMatch{}, err
		}
		if found {
			if m.commit != best.commit {
				return externalDeliveryMatch{}, fmt.Errorf("%w: commit %s is ambiguous: external work %s (%s) and (%s)", ErrInvalid, prefix, best.note.ID, best.commit, m.commit)
			}
			continue
		}
		if err = json.Unmarshal(raw, &m.note); err != nil {
			return externalDeliveryMatch{}, err
		}
		best, found = m, true
	}
	if err = rows.Err(); err != nil {
		return externalDeliveryMatch{}, err
	}
	if !found {
		return externalDeliveryMatch{}, pgx.ErrNoRows
	}
	return best, nil
}

// externalDeliveryRuns are the recorded pipeline runs naming commit as their
// revision, each as last recorded, oldest first.
func externalDeliveryRuns(ctx context.Context, tx pgx.Tx, commit string) ([]PipelineRun, error) {
	runs, err := deliveryreadings.Runs(ctx, tx, commit)
	var out []PipelineRun
	if runs != nil {
		out = make([]PipelineRun, len(runs))
		for i, r := range runs {
			out[i] = PipelineRun{Run: r.Run, Pipeline: r.Pipeline, Kind: r.Kind, Result: r.Result, Repo: r.Repo, Revision: r.Revision, CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
		}
	}
	return out, err
}

// readExternalDelivery is ReadDelivery's answer for a commit only an external
// work names: the same response, with the work in external_work_id and no
// attempt or operation. The step and task are the work's as it is bound now
// (its latest note): a heartbeat may name the commit before the work is
// linked. pgx.ErrNoRows: no external work names it either.
func readExternalDelivery(ctx context.Context, tx pgx.Tx, q DeliveryQuery, now time.Time) (map[string]any, error) {
	m, err := externalDeliveryCommit(ctx, tx, q.Commit)
	if err != nil {
		return nil, err
	}
	in := ExternalDelivery{Work: m.note.ID, Registrant: m.note.Actor, Commit: m.commit, NamedAt: m.at}
	if m.note, _, _, err = externalwork.ReadExternal(ctx, tx, m.note.ID); err != nil {
		return nil, err
	}
	warnings := []string{"stages are matched by this exact commit; a later revision that carries it is not recognised"}
	if d, err := externalwork.ExternalDescriptionOf(ctx, tx, m.note.Artifact); err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		warnings = append(warnings, "the work's description could not be read: its repository is unknown")
	} else {
		in.Repo = d.Repo
	}
	if in.Runs, err = externalDeliveryRuns(ctx, tx, m.commit); err != nil {
		return nil, err
	}
	// A recorded run names the repository by its label; the description's is
	// free text.
	for _, r := range in.Runs {
		if r.Repo != "" {
			in.Repo = r.Repo
			break
		}
	}
	if in.Repo == externalDeliveryRepo {
		in.Production = q.Production
	}
	return map[string]any{"schema": "agentrt.delivery_response/v1", "commit": m.commit, "task_id": m.note.Task, "attempt_id": "",
		"plan_node_id": m.note.Node, "repo": in.Repo, "operation": "", "external_work_id": m.note.ID,
		"stages": FoldExternalDelivery(in), "warnings": warnings, "as_of": timeText(now)}, nil
}
