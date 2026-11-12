package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// External delivery: the delivery stages of a commit that no landing
// operation or attempt names and an external work handed in (its submitted
// event's commit_shas). The stages are the same seven. What the runtime
// records of such a commit is the pipeline runs naming it as their revision
// (pipeline_seen) and the current PROD release; the merge into main happens
// in Gitea and is not recorded, so owner_wait and on_main are unknown, with
// the reason, never inferred from the stages around them.

// The delivery pipelines whose revision is the commit of the repository they
// deliver, and the stage a run of each is.
var externalDeliveryPipelines = map[string]string{
	"bifrost-deliver-platform":      StageSTG,
	"bifrost-deliver-platform-prod": StageProdRelease,
}

const externalDeliveryRepo = "bifrost-platform"

// ExternalDelivery is everything FoldExternalDelivery reads.
type ExternalDelivery struct {
	// Work is the external work, Registrant who registered it, Repo the
	// repository its description names.
	Work, Registrant, Repo, Commit string
	// SubmittedAt is when the submitted event naming Commit was recorded.
	SubmittedAt time.Time
	// Runs are the recorded pipeline runs: nil when they were not read, an
	// empty non-nil slice when they were read and hold none.
	Runs       []PipelineRun
	Production *DeliveryProduction
}

// externalRuns is what the recorded runs of one stage say.
type externalRuns struct {
	started, green time.Time
	failed         string // the latest run that failed, "" when none did
}

func (f *externalRuns) add(r PipelineRun) {
	if !r.StartedAt.IsZero() && (f.started.IsZero() || r.StartedAt.Before(f.started)) {
		f.started = r.StartedAt
	}
	switch r.Result {
	case "Succeeded":
		if !r.CompletedAt.IsZero() && (f.green.IsZero() || r.CompletedAt.Before(f.green)) {
			f.green = r.CompletedAt
		}
	case "Failed":
		f.failed = r.Run
	}
}

// FoldExternalDelivery gives the seven stages of in's commit, in order. It is
// pure. A run counts only when its revision is exactly the commit: a later
// revision that carries the change is not recognised.
func FoldExternalDelivery(in ExternalDelivery) []DeliveryStage {
	st := make([]DeliveryStage, len(DeliveryStageOrder))
	for i, name := range DeliveryStageOrder {
		st[i] = DeliveryStage{Stage: name}
	}
	reached := make([]bool, len(st))
	unknown := make([]string, len(st))

	first, firstSource := in.SubmittedAt, "external_work"
	var ci, stg, prod externalRuns
	for _, r := range in.Runs {
		if r.Revision != in.Commit {
			continue
		}
		if !r.CreatedAt.IsZero() && (first.IsZero() || r.CreatedAt.Before(first)) {
			first, firstSource = r.CreatedAt, "pipeline_seen"
		}
		switch {
		case r.Kind == "ci":
			ci.add(r)
		case r.Kind == "delivery" && externalDeliveryPipelines[r.Pipeline] == StageSTG:
			stg.add(r)
		case r.Kind == "delivery" && externalDeliveryPipelines[r.Pipeline] == StageProdRelease:
			prod.add(r)
		}
	}

	// handed_in: the commit first reached the platform, as a run of it or as
	// the work's submitted event, whichever is earlier.
	h := &st[0]
	h.EnteredAt, h.Actor, h.ActorRole, h.Source = first, in.Registrant, "executor", firstSource
	reached[0] = true

	run := func(i int, f externalRuns, waits, failed string) {
		s := &st[i]
		s.EnteredAt, s.LeftAt, s.WaitsOn, s.Source = f.started, f.green, waits, "pipeline_seen"
		reached[i] = !f.green.IsZero()
		if !reached[i] && f.failed != "" {
			s.Reason = failed + ": " + f.failed
		}
	}
	run(1, ci, "CI", "CI failed")
	run(4, stg, "STG release", "STG delivery failed")
	run(5, prod, "PROD release", "PROD delivery failed")
	q := &st[6]
	q.EnteredAt, q.Source = prod.green, "pipeline_seen"
	reached[6] = !prod.green.IsZero()

	switch {
	case in.Runs == nil:
		for _, i := range []int{1, 4, 5, 6} {
			unknown[i] = "the recorded pipeline runs were not read"
		}
	case ci.started.IsZero() && ci.green.IsZero() && ci.failed == "":
		unknown[1] = "no completed CI run of this commit is recorded"
	}
	if in.Runs != nil && in.Repo != externalDeliveryRepo {
		why := "delivery runs of " + in.Repo + " are not matched to a commit"
		if in.Repo == "" {
			why = "the repository of this commit is unknown"
		}
		for _, i := range []int{4, 5, 6} {
			unknown[i] = why
		}
	}
	unknown[2] = "the merge into main happens in Gitea; the platform does not record who waited for it"
	unknown[3] = "the platform does not record when a commit reaches main"

	// STG delivered the commit and no PROD run of it is recorded (a run is
	// recorded minutes after it ends): what PROD runs now tells waiting from
	// unknown. Waiting is said only when that was read: the current release
	// is another commit, or the workloads do not run this commit's build.
	if p := in.Production; in.Runs != nil && in.Repo == externalDeliveryRepo && reached[4] && prod == (externalRuns{}) {
		switch {
		case p == nil:
			unknown[5] = "the current PROD release was not read"
		case p.Reason == "", p.NotCommit == in.Commit:
		default:
			unknown[5] = "what PROD runs now is unknown: " + p.Reason
		}
		unknown[6] = unknown[5]
	}

	// The current PROD release, validated against live workloads, names this
	// commit: on PROD even when its run is not recorded yet.
	if p := in.Production; p != nil && !reached[6] && p.Reason == "" && p.Commit == in.Commit &&
		!p.StartedAt.IsZero() && !p.CompletedAt.IsZero() && !p.CompletedAt.Before(p.StartedAt) {
		r := &st[5]
		r.EnteredAt, r.LeftAt, r.Source, r.Reason = p.StartedAt, p.CompletedAt, "release_record", ""
		q.EnteredAt, q.Source = p.CompletedAt, "release_record"
		reached[5], reached[6] = true, true
		unknown[5], unknown[6] = "", ""
	}

	// handed_in ends when the next stage with a time begins.
	for _, s := range st[1:] {
		if !s.EnteredAt.IsZero() && !s.EnteredAt.Before(h.EnteredAt) {
			h.LeftAt = s.EnteredAt
			break
		}
	}

	// States: a stage with its fact is done; the first without one is
	// current, the later ones not reached. A stage whose source is missing is
	// unknown and does not stop the walk.
	current := false
	for i := range st {
		x := &st[i]
		switch {
		case reached[i]:
			x.State, x.WaitsOn = StageDone, ""
		case unknown[i] != "":
			*x = DeliveryStage{Stage: x.Stage, State: StageUnknown, Reason: unknown[i]}
		case current:
			*x = DeliveryStage{Stage: x.Stage, State: StageNotReached}
		default:
			x.State, current = StageCurrent, true
		}
	}
	return st
}

// externalDeliveryMatch is one submitted external work naming a commit.
type externalDeliveryMatch struct {
	note   externalNote
	commit string
	at     time.Time
}

// externalDeliveryCommit finds the external work whose submitted event names
// a commit starting with prefix: the earliest such event. Two different
// commits under one prefix are ErrInvalid naming both; none is pgx.ErrNoRows.
func externalDeliveryCommit(ctx context.Context, tx pgx.Tx, prefix string) (externalDeliveryMatch, error) {
	rows, err := tx.Query(ctx, `SELECT e.body_json->'external_work', s.sha, e.at FROM agentrt.event e
	  CROSS JOIN LATERAL jsonb_array_elements_text(e.body_json->'external_work'->'commit_shas') AS s(sha)
	  WHERE e.event_stream_id LIKE 'external-work:%' AND e.event_type = 'note'
	  AND e.body_json->'external_work'->>'type' = 'submitted'
	  AND jsonb_typeof(e.body_json->'external_work'->'commit_shas') = 'array'
	  AND left(s.sha, length($1)) = $1 ORDER BY e.event_id, s.sha LIMIT 50`, prefix)
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
	rows, err := tx.Query(ctx, `SELECT body FROM (SELECT DISTINCT ON (body_json->>'run', body_json->>'uid') event_id, body_json AS body FROM agentrt.event
	  WHERE event_type = 'pipeline_seen' AND body_json @> jsonb_build_object('revision', $1::text)
	  ORDER BY body_json->>'run', body_json->>'uid', event_id DESC) x ORDER BY event_id`, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PipelineRun{}
	for rows.Next() {
		var b struct {
			Run, Pipeline, Kind, Result, Repo, Revision string
			CreatedAt                                   time.Time `json:"created_at"`
			StartedAt                                   time.Time `json:"started_at"`
			CompletedAt                                 time.Time `json:"completed_at"`
		}
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &b); err != nil {
			return nil, err
		}
		out = append(out, PipelineRun{Run: b.Run, Pipeline: b.Pipeline, Kind: b.Kind, Result: b.Result, Repo: b.Repo, Revision: b.Revision,
			CreatedAt: b.CreatedAt, StartedAt: b.StartedAt, CompletedAt: b.CompletedAt})
	}
	return out, rows.Err()
}

// readExternalDelivery is ReadDelivery's answer for a commit only an external
// work names: the same response, with the work in external_work_id and no
// attempt or operation. pgx.ErrNoRows: no external work names it either.
func readExternalDelivery(ctx context.Context, tx pgx.Tx, q DeliveryQuery, now time.Time) (map[string]any, error) {
	m, err := externalDeliveryCommit(ctx, tx, q.Commit)
	if err != nil {
		return nil, err
	}
	in := ExternalDelivery{Work: m.note.ID, Registrant: m.note.Actor, Commit: m.commit, SubmittedAt: m.at}
	warnings := []string{"stages are matched by this exact commit; a later revision that carries it is not recognised"}
	if d, err := externalDescription(ctx, tx, m.note.Artifact); err != nil {
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
