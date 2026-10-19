package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Attempts.

// Fence names the caller of an attempt route: the host that holds the
// attempt and the epoch it was given.
type Fence struct {
	AttemptID string
	HostID    string
	Epoch     int64
}

type attemptRow struct {
	id, task, host, state        string
	no                           int
	epoch                        int64
	expires, now                 time.Time
	started, computed, persisted bool
	t                            taskRow // locked, when the caller asked for the task
}

func liveState(s string) bool { return s == "leased" || s == "running" || s == "stale" }

// lockAttempt locks the attempt row and, with withTask, then its task's row
// (attempt before task, as everywhere). The database clock is read after
// every lock is held, in a statement of its own: a clock value read before
// or during a lock wait may predate the end of the wait (T22 ⑤, review 148
// A3).
func lockAttempt(ctx context.Context, tx pgx.Tx, id string, withTask bool) (attemptRow, error) {
	var a attemptRow
	var host *string
	err := tx.QueryRow(ctx, `SELECT attempt_id, task_id, attempt_no, host_id, state, lease_epoch, lease_expires_at,
	  started_at IS NOT NULL, computed_at IS NOT NULL, persisted_at IS NOT NULL
	  FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, id).
		Scan(&a.id, &a.task, &a.no, &host, &a.state, &a.epoch, &a.expires, &a.started, &a.computed, &a.persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrFenced
	}
	if err != nil {
		return a, err
	}
	if host != nil {
		a.host = *host
	}
	if withTask {
		if a.t, err = readTask(ctx, tx, a.task, true); err != nil {
			return a, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&a.now)
	return a, err
}

// lockLive is the write-time fence of v5 §5.2 「写入时的隔离检查」 ③: with
// the attempt row (and, for routes that move the task, the task row) locked,
// the attempt's host is the caller's, its state is live, its epoch is the
// caller's, and the database time after the locks is before
// lease_expires_at plus the grace. It does not rely on the lease loop.
func lockLive(ctx context.Context, tx pgx.Tx, f Fence, withTask bool) (attemptRow, error) {
	a, err := lockAttempt(ctx, tx, f.AttemptID, withTask)
	if err != nil {
		return a, err
	}
	if a.host != f.HostID || a.epoch != f.Epoch || !liveState(a.state) || !a.now.Before(a.expires.Add(Grace)) {
		return a, ErrFenced
	}
	return a, nil
}

func attemptEvent(ctx context.Context, tx pgx.Tx, actor string, a attemptRow, typ string, body map[string]any) error {
	var mission string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, a.task).Scan(&mission); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: typ, Actor: actor, TaskID: a.task, AttemptID: a.id, Body: body})
	return err
}

// Beat is a heartbeat's answer.
type Beat struct {
	LeaseExpiresAt time.Time
	Stop           bool // a stop control covers this attempt: end it
}

// Heartbeat renews the lease. A lease that has not lapsed is extended through
// the liveness columns alone, with no event. One that has lapsed (stale, or
// past expiry before the lease loop got to it) is renewed with
// attempt_renewed and goes back to leased or running.
func Heartbeat(ctx context.Context, db DB, f Fence) (Beat, error) {
	var b Beat
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		a, err := lockLive(ctx, tx, f, false)
		if err != nil {
			return err
		}
		if a.state == "stale" || !a.now.Before(a.expires) {
			next := a.now.Add(LeaseTTL)
			to := "leased"
			if a.started {
				to = "running"
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_renewed",
				map[string]any{"lease_expires_at": timeText(next)}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = $2, lease_expires_at = $3, heartbeat_at = clock_timestamp()
			  WHERE attempt_id = $1`, a.id, to, next); err != nil {
				return err
			}
		} else if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt
		  SET lease_expires_at = GREATEST(lease_expires_at, clock_timestamp() + make_interval(secs => $2)), heartbeat_at = clock_timestamp()
		  WHERE attempt_id = $1`, a.id, LeaseTTL.Seconds()); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT lease_expires_at FROM agentrt.attempt WHERE attempt_id = $1`, a.id).Scan(&b.LeaseExpiresAt); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, a.task, false)
		if err != nil {
			return err
		}
		b.Stop, err = held(ctx, tx, []scope{{"global", ""}, {"mission", t.mission}, {"task", t.id}, {"host", f.HostID}}, "stop")
		return err
	})
	return b, err
}

// Start marks a leased attempt and its task running. The provider, when
// given, must be the one the attempt was leased for: independence was checked
// against it. Starting an attempt that already started does nothing.
func Start(ctx context.Context, db DB, f Fence, model, provider string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		a, err := lockLive(ctx, tx, f, true)
		if err != nil {
			return err
		}
		var haveModel, haveProvider string
		if err = tx.QueryRow(ctx, `SELECT COALESCE(model, ''), COALESCE(model_provider, '') FROM agentrt.attempt WHERE attempt_id = $1`,
			a.id).Scan(&haveModel, &haveProvider); err != nil {
			return err
		}
		if (provider != "" && provider != haveProvider) || (model != "" && haveModel != "" && model != haveModel) {
			return fmt.Errorf("%w: attempt %s was leased for %s %s", ErrState, a.id, haveProvider, haveModel)
		}
		if a.started {
			return nil
		}
		if a.state != "leased" {
			return fmt.Errorf("%w: attempt %s is %s", ErrState, a.id, a.state)
		}
		body := map[string]any{}
		if model != "" {
			body["model"] = model
		}
		if provider != "" {
			body["model_provider"] = provider
		}
		if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_started", body); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'running', started_at = clock_timestamp(),
		  model = COALESCE(model, NULLIF($2, '')) WHERE attempt_id = $1`, a.id, model); err != nil {
			return err
		}
		return moveTask(ctx, tx, hostActor(f.HostID), a.t, "running", "")
	})
}

// Checkpoint records sample seq of a live attempt: the artifacts of the
// sample (log tail, patch), or that the patch was too large to keep. The key
// is the attempt plus the sample number, so each sample is recorded once and
// a retried sample gets the first answer (review 142 R2).
func Checkpoint(ctx context.Context, db DB, f Fence, seq int, artifacts []string, tooLarge bool) (map[string]any, error) {
	if seq < 1 {
		return nil, fmt.Errorf("%w: checkpoint numbers start at 1", ErrState)
	}
	req := map[string]any{"epoch": f.Epoch, "seq": seq, "artifacts": nonNil(artifacts), "too_large": tooLarge}
	return idempotent(ctx, db, hostActor(f.HostID), "checkpoint", fmt.Sprintf("%s:%d", f.AttemptID, seq), req, nil,
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, false)
			if err != nil {
				return nil, err
			}
			if err = haveArtifacts(ctx, tx, artifacts); err != nil {
				return nil, err
			}
			body := map[string]any{"checkpoint_seq": seq}
			if len(artifacts) > 0 {
				body["artifacts"] = artifacts
			}
			if tooLarge {
				body["too_large"] = true
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_checkpointed", body); err != nil {
				return nil, err
			}
			return map[string]any{"checkpoint_seq": seq}, nil
		})
}

var endHows = []string{"exit", "hard_limit", "over_capacity", "cancelled", "daemon_restart", "host_reboot", "lease_lost"}

// Computed records that the executor ended and its result is on the host.
// exitCode is nil when the process had none (killed by a signal).
func Computed(ctx context.Context, db DB, f Fence, exitCode *int, endHow, conclusion string) (map[string]any, error) {
	return ComputedWith(ctx, db, f, Ending{ExitCode: exitCode, EndHow: endHow, Conclusion: conclusion})
}

// Ending is how an executor ended, with what it used and what its vendor
// said about its pools (2.4 slice ①); an agentd older than 2.4 sends neither.
type Ending struct {
	ExitCode           *int
	EndHow, Conclusion string
	Usage              *Usage
	Pools              []PoolReading
}

// ComputedWith is Computed with the attempt's usage and pool readings: the
// usage goes to the ledger with the wall time since the start, and the
// readings to the pools of the attempt's own tool, in the same transaction.
func ComputedWith(ctx context.Context, db DB, f Fence, e Ending) (map[string]any, error) {
	exitCode, endHow, conclusion := e.ExitCode, e.EndHow, e.Conclusion
	if !slices.Contains(endHows, endHow) || !slices.Contains([]string{"done", "failed", "none"}, conclusion) {
		return nil, fmt.Errorf("%w: end %q and conclusion %q", ErrState, endHow, conclusion)
	}
	req := map[string]any{"epoch": f.Epoch, "end_how": endHow, "conclusion": conclusion}
	body := map[string]any{"end_how": endHow, "conclusion": conclusion}
	if exitCode != nil {
		req["exit_code"], body["exit_code"] = *exitCode, *exitCode
	}
	// The usage is not part of the request's identity: a retry after agentd
	// restarted measures again from the output, and must get the first
	// answer, never a key conflict that would hold the ending forever. The
	// first report is the one kept.
	return idempotent(ctx, db, hostActor(f.HostID), "computed", f.AttemptID, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		a, err := lockLive(ctx, tx, f, false)
		if err != nil {
			return nil, err
		}
		if !a.started || a.computed {
			return nil, fmt.Errorf("%w: attempt %s is not started-and-uncomputed", ErrState, a.id)
		}
		var executor string
		var wallS int64
		if err = tx.QueryRow(ctx, `SELECT executor_actor_id, GREATEST(0, EXTRACT(EPOCH FROM clock_timestamp() - started_at))::bigint
		  FROM agentrt.attempt WHERE attempt_id = $1`, a.id).Scan(&executor, &wallS); err != nil {
			return nil, err
		}
		// A report the platform cannot take never fails the ending: the
		// attempt is computed without it, and the answer says why (as with a
		// heartbeat's actions, review 264 A1).
		out := map[string]any{"computed": true}
		if rerr := checkPoolReport(executor, e.Usage, e.Pools); rerr != nil {
			e.Usage, e.Pools = nil, nil
			out["usage_refused"] = rerr.Error()
		}
		var pools []string
		if e.Usage != nil {
			pools = append(pools, e.Usage.Pool)
		}
		for _, r := range e.Pools {
			pools = append(pools, r.Pool)
		}
		if err = lockPools(ctx, tx, pools...); err != nil {
			return nil, err
		}
		if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_computed", body); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET computed_at = clock_timestamp(), exit_code = $2, end_how = $3, conclusion = $4
		  WHERE attempt_id = $1`, a.id, exitCode, endHow, conclusion); err != nil {
			return nil, err
		}
		if err = chargeAttempt(ctx, tx, hostActor(f.HostID), a, wallS, e.Usage); err != nil {
			return nil, err
		}
		for _, r := range e.Pools {
			source := "reading"
			if r.Limited {
				source = "exit_text"
			}
			if err = measurePools(ctx, tx, hostActor(f.HostID), source, []PoolReading{r}); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
}

// BranchHead reads the commit a branch of a repository on the main Git
// server points at. The HTTP layer passes a Gitea reader, tests a fake.
type BranchHead func(ctx context.Context, repo, ref string) (string, error)

// ErrNoHead: no reader of branch heads is configured, so a pushed branch
// cannot be checked yet; agentd tries again later.
var ErrNoHead = errors.New("store: no reader of branch heads is configured")

// Result is what agentd hands over: the report, other artifacts, and the
// branch and commit it pushed, if it pushed one.
type Result struct {
	Report    string
	Artifacts []string
	Ref       string
	Commit    string
}

func (r Result) request(epoch int64) map[string]any {
	return map[string]any{"epoch": epoch, "report": r.Report, "artifacts": nonNil(r.Artifacts), "ref": r.Ref, "commit": r.Commit}
}

func (r Result) all() []string {
	if r.Report == "" {
		return r.Artifacts
	}
	return append([]string{r.Report}, r.Artifacts...)
}

// checkHead is the Gitea half of the result check: the branch of the task's
// repository (read from the task, not from the caller) points at the commit
// reported.
func (r Result) checkHead(db DB, attemptID string, head BranchHead) func(context.Context) error {
	return func(ctx context.Context) error {
		if (r.Ref == "") != (r.Commit == "") {
			return fmt.Errorf("%w: a pushed branch needs both its ref and its commit", ErrState)
		}
		if r.Ref == "" {
			return nil
		}
		if head == nil {
			return ErrNoHead
		}
		var repo string
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT COALESCE(t.repo, '') FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
			  WHERE a.attempt_id = $1`, attemptID).Scan(&repo)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFenced
		}
		if err != nil {
			return err
		}
		if repo == "" {
			return fmt.Errorf("%w: the task has no repository to push to", ErrState)
		}
		got, err := head(ctx, repo, r.Ref)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", r.Ref, err)
		}
		if got != r.Commit {
			return fmt.Errorf("%w: %s is at %s, not %s", ErrState, r.Ref, got, r.Commit)
		}
		return nil
	}
}

// fits checks the result against the attempt and the database: the branch is
// the attempt's own, a branch task pushed one and a none task did not, every
// artifact is stored here, and the report is one (reportFits).
func (r Result) fits(ctx context.Context, tx pgx.Tx, a attemptRow) error {
	var effect string
	if err := tx.QueryRow(ctx, `SELECT effect_class FROM agentrt.task WHERE task_id = $1`, a.task).Scan(&effect); err != nil {
		return err
	}
	switch {
	case r.Ref != "" && r.Ref != AttemptRef(a.task, a.no):
		return fmt.Errorf("%w: attempt %s pushes only to %s", ErrState, a.id, AttemptRef(a.task, a.no))
	case effect == "branch" && r.Ref == "":
		return fmt.Errorf("%w: a branch task's result is a pushed branch", ErrState)
	case effect == "none" && r.Ref != "":
		return fmt.Errorf("%w: a task of effect none pushes nothing", ErrState)
	}
	if err := haveArtifacts(ctx, tx, r.all()); err != nil {
		return err
	}
	return reportFits(ctx, tx, r.Report)
}

// reportKinds are the kinds a result's report may be stored as.
var reportKinds = map[string]bool{"report": true, "verify": true, "review": true}

// reportFits checks the artifact a result names as its report: stored as a
// report, a verification or a review, and with no local path in it (H8),
// whatever kind it was uploaded as. A log tail or a patch, which H8 lets
// through as raw output, cannot take the report's place (review 156 A1).
func reportFits(ctx context.Context, tx pgx.Tx, id string) error {
	if id == "" {
		return nil
	}
	var kind string
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT a.artifact_kind, b.body FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id)
	  WHERE a.artifact_id = $1`, id).Scan(&kind, &body); err != nil {
		return err
	}
	if !reportKinds[kind] {
		return fmt.Errorf("%w: the report %s is stored as %s, not as a report, a verification or a review", ErrState, id, kind)
	}
	if p, found := LocalPath(kind, body); found {
		return fmt.Errorf("%w: the report %s has a local path in it: %s", ErrState, id, p)
	}
	if ref, found := WorkspaceRef(kind, body); found {
		return fmt.Errorf("%w: the report %s refers to a task's workspace: %s", ErrState, id, ref)
	}
	return nil
}

func (r Result) body() map[string]any {
	body := map[string]any{}
	if all := r.all(); len(all) > 0 {
		body["artifacts"] = all
	}
	if r.Ref != "" {
		body["pushed_ref"], body["pushed_commit"] = r.Ref, r.Commit
	}
	return body
}

// Persist records that the result has left the host (attempt_persisted):
// the report and artifacts are stored here, and the branch, if any, points at
// the commit reported. Only a persisted attempt can be accepted.
func Persist(ctx context.Context, db DB, f Fence, r Result, head BranchHead) (map[string]any, error) {
	if r.Report == "" {
		return nil, fmt.Errorf("%w: a result has a report", ErrState)
	}
	return idempotent(ctx, db, hostActor(f.HostID), "persist", f.AttemptID, r.request(f.Epoch), r.checkHead(db, f.AttemptID, head),
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, false)
			if err != nil {
				return nil, err
			}
			if !a.computed || a.persisted {
				return nil, fmt.Errorf("%w: attempt %s is not computed-and-unpersisted", ErrState, a.id)
			}
			if err = r.fits(ctx, tx, a); err != nil {
				return nil, err
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_persisted", r.body()); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), report_artifact_id = $2,
			  pushed_ref = NULLIF($3, ''), pushed_commit = NULLIF($4, '') WHERE attempt_id = $1`, a.id, r.Report, r.Ref, r.Commit); err != nil {
				return nil, err
			}
			return map[string]any{"persisted": true}, nil
		})
}

// Submit hands a persisted attempt over for verification. The key is the
// attempt: an attempt has one result, so this is the same as keying by its
// pushed commit.
func Submit(ctx context.Context, db DB, f Fence) (map[string]any, error) {
	return idempotent(ctx, db, hostActor(f.HostID), "submit", f.AttemptID, map[string]any{"epoch": f.Epoch}, nil,
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, true)
			if err != nil {
				return nil, err
			}
			hook("submitting", a.id)
			if !a.persisted {
				return nil, fmt.Errorf("%w: attempt %s is not persisted", ErrState, a.id)
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_submitted", map[string]any{}); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'submitted' WHERE attempt_id = $1`, a.id); err != nil {
				return nil, err
			}
			if err = moveTask(ctx, tx, hostActor(f.HostID), a.t, "submitted", ""); err != nil {
				return nil, err
			}
			return map[string]any{"submitted": true}, nil
		})
}

// Salvage keeps the result of an attempt that lost its lease (v5 §5.2,
// §6.2). Only the attempt's host may salvage it, and only once its lease plus
// the grace has passed, whatever state the attempt is in (review 148 A1):
// before that the answer is ErrInGrace and agentd tries again later.
// Attempts that were submitted have nothing to salvage. The result is checked
// as persist checks it; when it holds a report and the attempt was not
// persisted, the attempt gets persisted_at, so a new verification task may
// accept it. Neither the attempt's state nor the task's changes.
func Salvage(ctx context.Context, db DB, attemptID, hostID string, r Result, head BranchHead) (map[string]any, error) {
	return idempotent(ctx, db, hostActor(hostID), "salvage", attemptID, r.request(0), r.checkHead(db, attemptID, head),
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockAttempt(ctx, tx, attemptID, false)
			if err != nil {
				return nil, err
			}
			if a.host != hostID {
				return nil, ErrFenced
			}
			if a.state == "submitted" || a.state == "finished" {
				return nil, fmt.Errorf("%w: attempt %s was %s", ErrState, a.id, a.state)
			}
			if a.now.Before(a.expires.Add(Grace)) {
				return nil, ErrInGrace
			}
			if err = r.fits(ctx, tx, a); err != nil {
				return nil, err
			}
			if err = attemptEvent(ctx, tx, hostActor(hostID), a, "attempt_salvaged", r.body()); err != nil {
				return nil, err
			}
			persisted := a.persisted
			if !persisted && r.Report != "" {
				if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), report_artifact_id = $2,
				  pushed_ref = NULLIF($3, ''), pushed_commit = NULLIF($4, '') WHERE attempt_id = $1`, a.id, r.Report, r.Ref, r.Commit); err != nil {
					return nil, err
				}
				persisted = true
			}
			return map[string]any{"salvaged": true, "persisted": persisted}, nil
		})
}

// Ended records that the attempt's process group is gone on its host
// (attempt_ended). An attempt already ended keeps its state; a live one ends
// as failed and its task moves by effect class (§6.2, T01). The key is the
// attempt, so a repeated report gets the first answer.
func Ended(ctx context.Context, db DB, attemptID, hostID, endHow string, groupCleared bool) (map[string]any, error) {
	if !slices.Contains(endHows, endHow) {
		return nil, fmt.Errorf("%w: end %q", ErrState, endHow)
	}
	req := map[string]any{"end_how": endHow, "group_cleared": groupCleared}
	return idempotent(ctx, db, hostActor(hostID), "ended", attemptID, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		a, err := lockAttempt(ctx, tx, attemptID, true)
		if err != nil {
			return nil, err
		}
		if a.host != hostID {
			return nil, ErrFenced
		}
		if err = attemptEvent(ctx, tx, hostActor(hostID), a, "attempt_ended",
			map[string]any{"end_how": endHow, "group_cleared": groupCleared}); err != nil {
			return nil, err
		}
		if !liveState(a.state) {
			return map[string]any{"state": a.state}, nil
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'failed', ended_at = clock_timestamp(),
		  end_how = COALESCE(end_how, $2) WHERE attempt_id = $1`, a.id, endHow); err != nil {
			return nil, err
		}
		if err = releaseTask(ctx, tx, hostActor(hostID), a.t, "attempt ended on its host: "+endHow); err != nil {
			return nil, err
		}
		return map[string]any{"state": "failed"}, nil
	})
}

// sweepable is the condition for an attempt the lease loop moves: live and
// past its expiry, or stale and past the grace. Stale attempts within their
// grace never crowd out the rest (review 148 B3).
const sweepable = `((state IN ('leased','running') AND lease_expires_at < clock_timestamp())
     OR (state = 'stale' AND lease_expires_at + make_interval(secs => $1) <= clock_timestamp()))`

// Sweep pages: a pass reads at most sweepPages pages of sweepPage attempts.
const (
	sweepPage  = 100
	sweepPages = 10
)

// SweepLeases is one pass of the lease loop (v5 §5.2 控制器「租约」): live
// attempts past their expiry become stale; past the grace they become lost
// and their task moves by effect class. Each attempt moves in a transaction
// of its own, so a sweep never holds one mission's event stream while it
// waits for another's (review 149 B2); one that fails or that a writer holds
// does not stop the rest, and the pass reads on past them page by page
// (review 150 B2). It only changes state; the write-time fence is what stops
// late writes. It returns how many attempts it moved.
func SweepLeases(ctx context.Context, db DB) (int, int, error) {
	var stale, lost int
	var errs []error
	var at any
	var after string
	for page := 0; page < sweepPages; page++ {
		var rows []sweepRow
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			q, err := tx.Query(ctx, `SELECT attempt_id, lease_expires_at FROM agentrt.attempt WHERE `+sweepable+`
			  AND ($2::timestamptz IS NULL OR (lease_expires_at, attempt_id) > ($2::timestamptz, $3::text))
			  ORDER BY lease_expires_at, attempt_id LIMIT $4`, Grace.Seconds(), at, after, sweepPage)
			if err != nil {
				return err
			}
			rows, err = pgx.CollectRows(q, scanSweepRow)
			return err
		}); err != nil {
			return stale, lost, errors.Join(append(errs, err)...)
		}
		for _, r := range rows {
			moved, err := sweepOne(ctx, db, r.id)
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("attempt %s: %w", r.id, err))
			case moved == "stale":
				stale++
			case moved == "lost":
				lost++
			}
		}
		if len(rows) < sweepPage {
			break
		}
		at, after = rows[len(rows)-1].expires, rows[len(rows)-1].id
	}
	return stale, lost, errors.Join(errs...)
}

type sweepRow struct {
	id      string
	expires time.Time
}

func scanSweepRow(r pgx.CollectableRow) (sweepRow, error) {
	var x sweepRow
	err := r.Scan(&x.id, &x.expires)
	return x, err
}

// sweepOne moves one attempt if it still needs it and no one holds it.
func sweepOne(ctx context.Context, db DB, id string) (string, error) {
	var moved string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var got string
		err := tx.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE attempt_id = $2 AND `+sweepable+`
		  FOR UPDATE SKIP LOCKED`, Grace.Seconds(), id).Scan(&got)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		a, err := lockAttempt(ctx, tx, id, true)
		if err != nil {
			return err
		}
		switch {
		case !a.now.Before(a.expires.Add(Grace)):
			if err = attemptEvent(ctx, tx, "controller", a, "attempt_lost",
				map[string]any{"reason": "lease expired at " + timeText(a.expires) + " and the grace passed"}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'lost', ended_at = clock_timestamp(),
			  end_how = COALESCE(end_how, 'lease_lost') WHERE attempt_id = $1`, a.id); err != nil {
				return err
			}
			if err = releaseTask(ctx, tx, "controller", a.t, "attempt lost"); err != nil {
				return err
			}
			moved = "lost"
		case a.state != "stale":
			if err = attemptEvent(ctx, tx, "controller", a, "attempt_stale",
				map[string]any{"lease_expires_at": timeText(a.expires)}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'stale' WHERE attempt_id = $1`, a.id); err != nil {
				return err
			}
			moved = "stale"
		}
		hook("swept", id)
		return nil
	})
	if err != nil {
		return "", err
	}
	if moved != "" {
		hook("committed", id)
	}
	return moved, nil
}

func haveArtifacts(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var missing []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id), '{}') FROM unnest($1::text[]) AS id
	  WHERE NOT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = id)`, ids).Scan(&missing); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: artifacts not stored: %v", ErrState, missing)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
