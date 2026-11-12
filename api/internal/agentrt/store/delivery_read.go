package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ReadDelivery (goal 3): one read of a change's delivery stages, by commit
// or by task. The change is resolved, its landing operation, grant, push,
// STG verifications and PROD deploys are read in one read-only snapshot, and
// FoldDelivery orders them. The release cycles live outside the runtime
// database: the caller reads them and passes what it got. A commit that no
// landing operation or attempt names, and an external work handed in, is
// answered from its recorded pipeline runs (delivery_external.go).

// DeliveryQuery names a change: exactly one of Commit (a 7 to 40 character
// lowercase hex prefix) or Task.
type DeliveryQuery struct {
	Commit, Task string
	// Production is supplied by the server's trusted release reader, never URL input.
	Production *DeliveryProduction
}

var deliveryCommitPrefix = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
var deliveryTaskID = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)

// deliveryCandidate is one place a commit names a change: a landing
// operation (Op set; Claim its claim number) or an attempt's pushed commit.
type deliveryCandidate struct {
	Repo, Op, Task, Attempt, Commit string
	Claim                           int
}

// change is what tells two candidates apart: the task, else the operation.
func (c deliveryCandidate) change() string {
	if c.Task != "" {
		return "task " + c.Task
	}
	return "operation " + c.Op + " on " + c.Repo
}

// resolveDeliveryCommit picks the change prefix names among candidates:
// candidates not starting with prefix are ignored; of one change, its latest
// landing operation (else its attempt) is the answer. Two different changes
// are ErrInvalid naming both; none is pgx.ErrNoRows.
func resolveDeliveryCommit(prefix string, candidates []deliveryCandidate) (deliveryCandidate, error) {
	if !deliveryCommitPrefix.MatchString(prefix) {
		return deliveryCandidate{}, fmt.Errorf("%w: commit must be 7 to 40 lowercase hex characters", ErrInvalid)
	}
	var best deliveryCandidate
	found := false
	for _, c := range candidates {
		if len(c.Commit) < len(prefix) || c.Commit[:len(prefix)] != prefix {
			continue
		}
		if found && c.change() != best.change() {
			return deliveryCandidate{}, fmt.Errorf("%w: commit %s is ambiguous: %s (%s) and %s (%s)", ErrInvalid, prefix, best.change(), best.Commit, c.change(), c.Commit)
		}
		if !found || (c.Op != "" && (best.Op == "" || c.Claim > best.Claim)) {
			best, found = c, true
		}
	}
	if !found {
		return deliveryCandidate{}, pgx.ErrNoRows
	}
	return best, nil
}

// ReadDelivery reads the delivery stages of q's change. cycles are the PROD
// release cycles of the change's lane as the caller read them; cyclesErr
// non-nil, or cycles nil, makes the PROD stages unknown with a warning.
func ReadDelivery(ctx context.Context, db DB, q DeliveryQuery, cycles []ReleaseCycle, cyclesErr error) (map[string]any, error) {
	if (q.Commit == "") == (q.Task == "") {
		return nil, fmt.Errorf("%w: name exactly one of commit or task", ErrInvalid)
	}
	if q.Task != "" && !deliveryTaskID.MatchString(q.Task) {
		return nil, fmt.Errorf("%w: task %q is not a task id", ErrInvalid, q.Task)
	}
	if q.Commit != "" && !deliveryCommitPrefix.MatchString(q.Commit) {
		return nil, fmt.Errorf("%w: commit must be 7 to 40 lowercase hex characters", ErrInvalid)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}

	var c deliveryCandidate
	if q.Commit != "" {
		candidates, err := deliveryCommitCandidates(ctx, tx, q.Commit)
		if err != nil {
			return nil, err
		}
		c, err = resolveDeliveryCommit(q.Commit, candidates)
		if errors.Is(err, pgx.ErrNoRows) {
			// No landing operation or attempt names it: an external work may.
			out, err := readExternalDelivery(ctx, tx, q, now)
			if err != nil {
				return nil, err
			}
			return out, tx.Commit(ctx)
		}
		if err != nil {
			return nil, err
		}
	} else if c, err = deliveryTaskCandidate(ctx, tx, q.Task); err != nil {
		return nil, err
	}

	warnings := []string{}
	landedResult := ""
	in := DeliveryInput{Attempt: DeliveryAttempt{TaskID: c.Task, AttemptID: c.Attempt}}
	if c.Repo == "bifrost-platform" {
		in.Production = q.Production
	}
	var planNode string
	if c.Task != "" {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id, '') FROM agentrt.task WHERE task_id = $1`, c.Task).Scan(&planNode); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if c.Attempt != "" {
		var submitted *time.Time
		err = tx.QueryRow(ctx, `SELECT executor_actor_id, COALESCE(ended_at, persisted_at) FROM agentrt.attempt WHERE attempt_id = $1`, c.Attempt).
			Scan(&in.Attempt.Executor, &submitted)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			warnings = append(warnings, "attempt "+c.Attempt+" is not in the runtime: when the change was handed in is unknown")
		case err != nil:
			return nil, err
		case submitted != nil:
			in.Attempt.SubmittedAt = *submitted
		}
	}

	if c.Op != "" {
		stream := "landing:" + c.Repo
		scan, err := scanLandingStream(ctx, tx, stream)
		if err != nil {
			return nil, err
		}
		in.Entries = deliveryEntries(scan.entries, c.Op)
		for _, e := range in.Entries {
			if e.Record.String("op") == c.Op && e.Record.String("kind") == "landed" {
				landedResult = e.Record.String("R")
			}
		}
		ev, found, err := LandingGrantEvidence(ctx, tx, c.Repo, c.Op)
		if err != nil {
			return nil, err
		}
		if found {
			// A landing grant is always the Owner's approval.
			in.Grant = &DeliveryGrant{Approver: "owner", ApprovedAt: ev.ApprovedAt}
		}
		var pusher, actor string
		err = tx.QueryRow(ctx, `SELECT COALESCE(body_json->>'landing_pusher', ''), actor_id FROM agentrt.event
		  WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing'->>'kind' = 'main_result'
		  AND body_json->'landing'->>'op' = $2 AND body_json->'landing'->>'outcome' = 'ok' ORDER BY seq DESC LIMIT 1`, stream, c.Op).Scan(&pusher, &actor)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		// The actor of main_result{ok} is the controller; the Gitea user
		// the push went as, when recorded beside it, names the pusher.
		in.Pusher = actor
		if pusher != "" {
			in.Pusher = pusher
		}
	}

	if s, err := StgReleaseState(ctx, tx); err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		warnings = append(warnings, "the STG release stream could not be read: "+err.Error())
	} else {
		in.Verified = deliveryVerified(s)
		in.Coverage = deliveryCoverage(s, StgMemberKey{Root: c.Task, Op: c.Op, R: landedResult})
	}

	switch {
	case cyclesErr != nil:
		warnings = append(warnings, "the release cycles could not be read: "+cyclesErr.Error()+"; the PROD stages are unknown")
	case cycles == nil:
		warnings = append(warnings, "the release cycles were not read; the PROD stages are unknown")
	default:
		in.Cycles = cycles
		if in.DeploySeen, err = deliveryDeploySeen(ctx, tx); err != nil {
			return nil, err
		}
	}
	stages := FoldDelivery(in)
	if in.Cycles == nil && in.Production == nil {
		for i := range stages {
			if s := &stages[i]; s.Stage == StageProdRelease || s.Stage == StageOnProd {
				s.State, s.Reason, s.WaitsOn = StageUnknown, "no release cycles given", ""
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"schema": "agentrt.delivery_response/v1", "commit": c.Commit, "task_id": c.Task, "attempt_id": c.Attempt,
		"plan_node_id": planNode, "repo": c.Repo, "operation": c.Op, "stages": stages, "warnings": warnings, "as_of": timeText(now)}, nil
}

// deliveryCommitCandidates are the landing operations whose R or A, the
// landed records, and the attempts whose pushed commit, start with prefix.
func deliveryCommitCandidates(ctx context.Context, tx pgx.Tx, prefix string) ([]deliveryCandidate, error) {
	streams := make([]string, 0, len(landing.LandingRepoNames()))
	for _, repo := range landing.LandingRepoNames() {
		streams = append(streams, "landing:"+repo)
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT event_stream_id, body_json->'landing'->>'op' FROM agentrt.event
	  WHERE event_stream_id = ANY($1) AND event_type = 'note' AND body_json ? 'landing'
	  AND body_json->'landing'->>'kind' IN ('claimed', 'prepared', 'main_intent', 'landed')
	  AND (left(body_json->'landing'->>'R', length($2)) = $2 OR left(body_json->'landing'->>'A', length($2)) = $2)`, streams, prefix)
	if err != nil {
		return nil, err
	}
	hits := map[string][]string{}
	for rows.Next() {
		var stream, op string
		if err = rows.Scan(&stream, &op); err != nil {
			rows.Close()
			return nil, err
		}
		hits[stream] = append(hits[stream], op)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var out []deliveryCandidate
	for _, stream := range streams {
		if len(hits[stream]) == 0 {
			continue
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return nil, err
		}
		repo := stream[len("landing:"):]
		for _, op := range hits[stream] {
			o, ok := s.Operations[op]
			if !ok {
				continue
			}
			c := deliveryCandidate{Repo: repo, Op: op, Task: o.Task, Attempt: o.Attempt, Claim: o.ClaimNumber, Commit: o.A}
			if len(o.R) >= len(prefix) && o.R[:len(prefix)] == prefix {
				c.Commit = o.R
			}
			out = append(out, c)
		}
	}
	rows, err = tx.Query(ctx, `SELECT a.attempt_id, a.task_id, COALESCE(t.repo, ''), a.pushed_commit FROM agentrt.attempt a
	  JOIN agentrt.task t USING (task_id) WHERE left(a.pushed_commit, length($1)) = $1 ORDER BY a.task_id, a.attempt_no`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c deliveryCandidate
		if err = rows.Scan(&c.Attempt, &c.Task, &c.Repo, &c.Commit); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// deliveryTaskCandidate is a task's latest landing operation; a task never
// claimed for landing is its latest attempt that pushed a commit.
func deliveryTaskCandidate(ctx context.Context, tx pgx.Tx, task string) (deliveryCandidate, error) {
	c := deliveryCandidate{Task: task}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(repo, '') FROM agentrt.task WHERE task_id = $1`, task).Scan(&c.Repo); err != nil {
		return c, err
	}
	if landing.RepoSupported(c.Repo) {
		s, err := LandingState(ctx, tx, "landing:"+c.Repo)
		if err != nil {
			return c, err
		}
		if op := s.LatestOperation(task); op != "" {
			o := s.Operations[op]
			c.Op, c.Attempt, c.Claim, c.Commit = op, o.Attempt, o.ClaimNumber, o.R
			if c.Commit == "" {
				c.Commit = o.A
			}
			return c, nil
		}
	}
	err := tx.QueryRow(ctx, `SELECT attempt_id, pushed_commit FROM agentrt.attempt WHERE task_id = $1 AND pushed_commit IS NOT NULL
	  ORDER BY attempt_no DESC LIMIT 1`, task).Scan(&c.Attempt, &c.Commit)
	return c, err
}

// deliveryEntries are op's landing records in stream order, and those of the
// legacy STG operation whose stg_pending carried op's landed R.
func deliveryEntries(entries []landing.Entry, op string) []landing.Entry {
	var r string
	for _, e := range entries {
		if e.Record.String("op") == op && e.Record.String("kind") == "landed" {
			r = e.Record.String("R")
		}
	}
	stgOp := ""
	if r != "" {
		for _, e := range entries {
			if e.Record.String("kind") != "stg_pending" {
				continue
			}
			pending, _ := e.Record["R"].([]any)
			if slices.Contains(pending, any(r)) {
				stgOp = e.Record.String("op")
				break
			}
		}
	}
	out := []landing.Entry{}
	for _, e := range entries {
		if o := e.Record.String("op"); o == op || (stgOp != "" && o == stgOp) {
			out = append(out, e)
		}
	}
	return out
}

// deliveryVerified are the revisions STG verified, oldest first: every
// release try verified, and every legacy STG operation verified.
func deliveryVerified(s StgState) []DeliveryRevision {
	type verified struct {
		DeliveryRevision
		seq int64
	}
	var all []verified
	for id, e := range s.Evidence {
		if p, at := e.Record.String("P"), StgDeployedAt(s, id); e.Record.String("source") == "external" && p != "" && !at.IsZero() {
			all = append(all, verified{DeliveryRevision{P: p, At: at}, e.Seq})
		}
	}
	for _, b := range s.Batches {
		for _, t := range b.Tries {
			if t != nil && t.Verify.Seq > 0 && t.Verify.Record.String("outcome") == "verified" && t.Verify.Record.String("P") != "" {
				all = append(all, verified{DeliveryRevision{P: t.Verify.Record.String("P"), At: t.Verify.At}, t.Verify.Seq})
			}
		}
	}
	for _, o := range s.Legacy.Operations {
		if o.Phase == "stg_verified" && o.P != "" {
			all = append(all, verified{DeliveryRevision{P: o.P, At: o.LastAt}, 0})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].At.Equal(all[j].At) {
			return all[i].At.Before(all[j].At)
		}
		return all[i].seq < all[j].seq
	})
	out := make([]DeliveryRevision, 0, len(all))
	for _, v := range all {
		out = append(out, v.DeliveryRevision)
	}
	return out
}

// Only the fold's validated covers relation establishes that this exact
// task/result was delivered; a matching current revision alone does not.
func deliveryCoverage(s StgState, member StgMemberKey) *DeliveryRevision {
	var out *DeliveryRevision
	for key, cover := range s.Covers {
		if key.Member != member {
			continue
		}
		e := s.Evidence[key.Evidence]
		if e.Seq == 0 || e.Record.String("P") == "" {
			continue
		}
		at := StgDeployedAt(s, key.Evidence)
		if at.IsZero() {
			at = cover.At
		}
		if out == nil || at.Before(out.At) {
			out = &DeliveryRevision{P: e.Record.String("P"), At: at}
		}
	}
	return out
}

// deliveryDeploySeen are the deploy_seen events of env prod, oldest first.
func deliveryDeploySeen(ctx context.Context, tx pgx.Tx) ([]DeliveryDeploySeen, error) {
	rows, err := tx.Query(ctx, `SELECT body_json->>'commit', at FROM agentrt.event
	  WHERE event_type = 'deploy_seen' AND body_json @> '{"env":"prod"}' AND COALESCE(body_json->>'commit', '') <> '' ORDER BY event_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryDeploySeen{}
	for rows.Next() {
		var d DeliveryDeploySeen
		if err = rows.Scan(&d.Commit, &d.At); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
