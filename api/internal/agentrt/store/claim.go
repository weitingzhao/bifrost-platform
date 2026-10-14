package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Claiming.

// Tool is one executor a host can run, with the model it would use.
type Tool struct {
	Executor string // actor id, e.g. exec:codex
	Provider string // model provider; required, independence is checked on it
	Model    string // may be empty until the executor starts
}

// ClaimRequest is a host's poll for work.
type ClaimRequest struct {
	HostID    string
	FreeSlots int
	Tools     []Tool // in the host's order of preference
}

// Claim is a leased attempt and what agentd needs to run it.
type Claim struct {
	TaskID, AttemptID, MissionID, Role, Repo, SpecArtifact, BaseCommit, PushedRef, Effect string
	Tool                                                                                  Tool
	AttemptNo                                                                             int
	Epoch                                                                                 int64
	HardLimit                                                                             time.Duration
	LeaseExpiresAt                                                                        time.Time
}

// dispatched are the effect classes round 2 hands out. External work is not
// dispatched in round 2 (v5 §6.2); its takeover rules are in place for when
// it is (review 148 A4).
var dispatched = []string{"none", "branch"}

type claimOpts struct {
	ttl     time.Duration
	effects []string
}

type candidate struct {
	id, mission, role, spec, subject, base, repo, effect string
	core                                                 bool
	needs                                                []string
	limitS                                               int
	created                                              time.Time
}

const candidatePage = 100

// ClaimNext leases the oldest ready task of an active mission that one of the
// host's tools may take, or returns nil. It records the poll on the host (a
// liveness column) whatever the outcome. Routing in 2.2 is v5 §5.1.2
// 「分流顺序」②–④ only: hard capability facts, the core rule and
// independence from the author; scores and quota are 2.4. Candidates are
// read in pages until one is taken or none is left, so tasks no tool here may
// take never hide the ones it may (review 148 B2).
func ClaimNext(ctx context.Context, db DB, r ClaimRequest) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: LeaseTTL, effects: dispatched})
}

func claimNext(ctx context.Context, db DB, r ClaimRequest, o claimOpts) (*Claim, error) {
	for _, tl := range r.Tools {
		if tl.Executor == "" || tl.Provider == "" {
			return nil, errors.New("store: every tool names its executor and its model provider")
		}
	}
	var claim *Claim
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var state string
		var slots int
		err := tx.QueryRow(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = $1
		  RETURNING state, COALESCE((capacity_json->>'slots')::int, 1)`, r.HostID).Scan(&state, &slots)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: host %s is not registered", ErrFenced, r.HostID)
		}
		if err != nil || state != "active" || r.FreeSlots <= 0 || len(r.Tools) == 0 {
			return err
		}
		// A global control takes this lock exclusively (SetControl).
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(742105)`); err != nil {
			return err
		}
		var live int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE host_id = $1 AND state IN ('leased','running','stale')`,
			r.HostID).Scan(&live); err != nil || live >= slots {
			return err
		}
		stop, err := held(ctx, tx, []scope{{"global", ""}, {"host", r.HostID}}, "stop", "pause", "drain")
		if err != nil || stop {
			return err
		}
		var after *candidate
		for {
			cands, err := candidates(ctx, tx, o.effects, after)
			if err != nil {
				return err
			}
			for i := range cands {
				if claim, err = tryCandidate(ctx, tx, r, cands[i], o.ttl); err != nil || claim != nil {
					return err
				}
			}
			if len(cands) < candidatePage {
				return nil
			}
			after = &cands[len(cands)-1]
		}
	})
	return claim, err
}

// candidates reads one page of ready tasks of active missions, oldest first,
// after the cursor. A review or verification of core work is core work
// (review 148 B4).
func candidates(ctx context.Context, tx pgx.Tx, effects []string, after *candidate) ([]candidate, error) {
	var at any
	var id string
	if after != nil {
		at, id = after.created, after.id
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.mission_id, t.role, t.spec_artifact_id,
	  COALESCE(t.subject_task_id, ''), COALESCE(t.base_commit, ''), COALESCE(t.repo, ''), t.effect_class,
	  t.core OR COALESCE(s.core, false), ARRAY(SELECT jsonb_array_elements_text(t.needs_json)), t.hard_limit_s, t.created_at
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id
	  WHERE t.state = 'ready' AND m.state = 'active' AND t.effect_class = ANY($1)
	    AND ($2::timestamptz IS NULL OR (t.created_at, t.task_id) > ($2::timestamptz, $3::text))
	  ORDER BY t.created_at, t.task_id LIMIT $4`, effects, at, id, candidatePage)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCandidate)
}

func scanCandidate(row pgx.CollectableRow) (candidate, error) {
	var c candidate
	err := row.Scan(&c.id, &c.mission, &c.role, &c.spec, &c.subject, &c.base, &c.repo, &c.effect, &c.core, &c.needs, &c.limitS, &c.created)
	return c, err
}

// tryCandidate leases c to the first tool that may take it, or returns nil.
// What it locks for a candidate it does not take is released at once by
// rolling back to a savepoint, so a skipped candidate holds nothing until the
// claim commits (review 148 B1).
func tryCandidate(ctx context.Context, tx pgx.Tx, r ClaimRequest, c candidate, ttl time.Duration) (*Claim, error) {
	hook("candidate", c.id)
	tool, found, err := pickTool(ctx, tx, r.Tools, c)
	if err != nil || !found {
		return nil, err
	}
	hook("picked", c.id)
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	cl, err := take(ctx, sp, r.HostID, tool, c, ttl)
	if err != nil || cl == nil {
		if rerr := sp.Rollback(ctx); err == nil {
			err = rerr
		}
		return nil, err
	}
	if err := sp.Commit(ctx); err != nil {
		return nil, err
	}
	return cl, nil
}

// take locks the candidate if no one holds it and it is still ready and
// still of the mission it was read with (a plan change may have moved it;
// the next poll reads it afresh, review 149 A1). A review or verification
// then holds its subject, so no author attempt of the subject is leased
// meanwhile, and its tool is checked again under that lock (review 149 A2).
// Then the mission: the claim leases only if it is still active and no
// control holds the task. The mission row is shared by claims and taken
// exclusively by MoveMission and by a control on the mission, and the task
// row by a control on the task, so these checks are exact: a pause or stop
// either commits before the claim or is seen by it (review 148 A2). A plan
// task takes the mission row exclusively, so two plan tasks of one mission
// never lease together (task_one_live_plan).
func take(ctx context.Context, tx pgx.Tx, hostID string, tl Tool, c candidate, ttl time.Duration) (*Claim, error) {
	var mission string
	err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 AND state = 'ready' FOR NO KEY UPDATE SKIP LOCKED`,
		c.id).Scan(&mission)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || mission != c.mission {
		return nil, err
	}
	if c.subject != "" {
		// Skipped, not waited for, when someone holds the subject (an author
		// claim, a plan change moving subject and review): waiting here while
		// holding the review could close a cycle with a change that moves the
		// review next (review 150 B1). The next poll tries again.
		var held string
		err = tx.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE task_id = $1 FOR SHARE SKIP LOCKED`, c.subject).Scan(&held)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var still bool
		if still, err = eligible(ctx, tx, tl, c); err != nil || !still {
			return nil, err
		}
	}
	mode := "FOR SHARE"
	if c.role == "plan" {
		mode = "FOR NO KEY UPDATE"
	}
	var missionState string
	if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 `+mode, c.mission).Scan(&missionState); err != nil {
		return nil, err
	}
	if missionState != "active" {
		return nil, nil
	}
	stop, err := held(ctx, tx, []scope{{"mission", c.mission}, {"task", c.id}}, "stop", "pause")
	if err != nil || stop {
		return nil, err
	}
	if c.role == "plan" {
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.task WHERE mission_id = $1 AND role = 'plan' AND state IN ('leased','running'))`,
			c.mission).Scan(&busy); err != nil || busy {
			return nil, err
		}
	}
	hook("taken", c.id)
	return lease(ctx, tx, hostID, tl, c, ttl)
}

func pickTool(ctx context.Context, tx pgx.Tx, tools []Tool, c candidate) (Tool, bool, error) {
	for _, tl := range tools {
		ok, err := eligible(ctx, tx, tl, c)
		if err != nil || ok {
			return tl, ok, err
		}
	}
	return Tool{}, false, nil
}

// eligible applies the hard checks to one tool. Restrictions (cannot,
// must_not, only) bind whether or not the Owner has signed them yet: a
// restriction is a tightening, and tightening needs no signature.
//   - a cannot or must_not fact on any need of the task excludes the tool;
//   - a need that some executor holds an only fact on is open to holders only;
//   - core work needs a holder of only author_core, or only review_core for a
//     review or verification; with no holder nobody may take it;
//   - a review or verification never goes to an executor or a model provider
//     that made an attempt of its subject, nor to anyone when an attempt of
//     the subject has no recorded provider (independence cannot be shown).
func eligible(ctx context.Context, tx pgx.Tx, tl Tool, c candidate) (bool, error) {
	subjects := append([]string{}, c.needs...)
	core := ""
	if c.core {
		core = "author_core"
		if c.role == "review" || c.role == "verify" {
			core = "review_core"
		}
		subjects = append(subjects, core)
	}
	var excluded bool
	err := tx.QueryRow(ctx, `SELECT
	  EXISTS (SELECT 1 FROM agentrt.capability_fact WHERE retired_at IS NULL AND fact_kind IN ('cannot','must_not')
	          AND executor_actor_id = $1 AND (model_provider IS NULL OR model_provider = $2) AND subject = ANY($3))
	  OR EXISTS (SELECT 1 FROM agentrt.capability_fact f WHERE f.retired_at IS NULL AND f.fact_kind = 'only' AND f.subject = ANY($3)
	          AND NOT EXISTS (SELECT 1 FROM agentrt.capability_fact g WHERE g.retired_at IS NULL AND g.fact_kind = 'only'
	                  AND g.subject = f.subject AND g.executor_actor_id = $1 AND (g.model_provider IS NULL OR g.model_provider = $2)))
	  OR ($4::text <> '' AND NOT EXISTS (SELECT 1 FROM agentrt.capability_fact WHERE retired_at IS NULL AND fact_kind = 'only'
	          AND subject = $4 AND executor_actor_id = $1 AND (model_provider IS NULL OR model_provider = $2)))
	  OR ($5::text <> '' AND EXISTS (SELECT 1 FROM agentrt.attempt WHERE task_id = $5
	          AND (executor_actor_id = $1 OR model_provider IS NULL OR model_provider = $2)))`,
		tl.Executor, tl.Provider, subjects, core, c.subject).Scan(&excluded)
	return !excluded, err
}

func lease(ctx context.Context, tx pgx.Tx, hostID string, tl Tool, c candidate, ttl time.Duration) (*Claim, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt_no), 0) + 1 FROM agentrt.attempt WHERE task_id = $1`, c.id).Scan(&n); err != nil {
		return nil, err
	}
	id, epoch, err := leaseAttempt(ctx, tx, "controller", c.mission, c.id, tl, hostID, n, ttl)
	if err != nil {
		return nil, err
	}
	t, err := readTask(ctx, tx, c.id, true)
	if err != nil {
		return nil, err
	}
	if err = moveTask(ctx, tx, "controller", t, "leased", "claimed by host "+hostID); err != nil {
		return nil, err
	}
	cl := &Claim{TaskID: c.id, AttemptID: id, MissionID: c.mission, Role: c.role, Repo: c.repo, SpecArtifact: c.spec,
		BaseCommit: c.base, PushedRef: AttemptRef(c.id, n), Effect: c.effect, Tool: tl, AttemptNo: n, Epoch: epoch,
		HardLimit: time.Duration(c.limitS) * time.Second}
	err = tx.QueryRow(ctx, `SELECT lease_expires_at FROM agentrt.attempt WHERE attempt_id = $1`, id).Scan(&cl.LeaseExpiresAt)
	return cl, err
}
