package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
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
	WorkID                                                                                string // the mission's, for the Work trailer
	Tool                                                                                  Tool
	AttemptNo                                                                             int
	Epoch                                                                                 int64
	HardLimit                                                                             time.Duration
	LeaseExpiresAt                                                                        time.Time
	// Failed are the checks the task's earlier rounds failed, for an
	// implementation in a round after the first (TD-405, claimfailed.go).
	Failed []FailedCheck
	// SubjectReport is the report the attempt a check checks handed in, and
	// SubjectEffect its task's effect class, for a check (TD-409,
	// claimsubject.go).
	SubjectReport, SubjectEffect string
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
	envelope                                             string // the standing envelope its mission started under
	tenant                                               string // its mission's: whose routing rule applies
	class                                                string // its task class: which scores bear on it
	core                                                 bool
	needs, route                                         []string
	limitS                                               int
	created                                              time.Time
}

const candidatePage = 100

// ClaimNext leases the oldest ready task of an active mission that one of the
// host's tools may take, or returns nil. It records the poll on the host (a
// liveness column) whatever the outcome. Routing in 2.2 is v5 §5.1.2
// 「分流顺序」②–④: hard capability facts, the core rule and independence
// from the author; a tool whose account has a pool cooling down is not
// offered (2.4 slice ①); scores and pace are 2.4 slice ②. Candidates are
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
		// A host that states no slots has the most a rule may give (2.4 slice
		// ③): the signed per_host decides how many run.
		err := tx.QueryRow(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = $1
		  RETURNING state, COALESCE((capacity_json->>'slots')::int, $2)`, r.HostID, rules.MaxPerHost).Scan(&state, &slots)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: host %s is not registered", ErrFenced, r.HostID)
		}
		if err != nil || state != "active" || r.FreeSlots <= 0 || len(r.Tools) == 0 {
			return err
		}
		// A global control takes this lock exclusively (SetControl), and a
		// write of candidates or capability facts the other (LockCapabilities).
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(742105), pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		// The concurrency rule caps the host's slots; without one nothing is
		// dispatched (proposal §2).
		if slots, err = perHost(ctx, tx, slots); err != nil || slots == 0 {
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
		// A tool whose vendor account (the Owner's) has a pool cooling down takes nothing
		// until it is over (2.4 slice ①).
		if r.Tools, err = notCooling(ctx, tx, r.Tools); err != nil || len(r.Tools) == 0 {
			return err
		}
		// Held shared from here to the commit, the capability lock fixes the
		// candidates and restrictions this claim reads now.
		cp, err := readCapabilities(ctx, tx)
		if err != nil {
			return err
		}
		var after *candidate
		rc := ruleCache{}
		st := &pickState{}
		for {
			cands, err := candidates(ctx, tx, o.effects, after)
			if err != nil {
				return err
			}
			for i := range cands {
				// A mission that started inside a standing envelope is worked
				// only on its hosts, by its executors, while it is in force.
				req := r
				if cands[i].envelope != "" {
					env, eerr := rc.get(ctx, tx, cands[i].envelope)
					if eerr != nil {
						return eerr
					}
					var fits bool
					if req, fits = envelopeTools(env, r); !fits {
						continue
					}
				}
				if claim, err = tryCandidate(ctx, tx, req, cp, cands[i], o.ttl, rc, st); err != nil || claim != nil {
					return err
				}
			}
			if len(cands) < candidatePage {
				return nil
			}
			after = &cands[len(cands)-1]
		}
	})
	if err != nil && claim != nil {
		// Its commit failed: cut while it commits, pgx gives up on a commit
		// the server may still make (TD-399).
		return confirmClaim(ctx, db, claim, err)
	}
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// ConfirmLimit bounds the read-back of a claim whose commit failed.
const ConfirmLimit = 3 * time.Second

// confirmClaim reads back whether a claim whose commit failed (cerr) leased
// its attempt, under a context of its own: it takes the task's row, which
// the claim held to its end, so it reads once the claim's transaction has
// ended, committed or not. It needs a connection of its own, as a pool gives
// (a single connection the cut closed cannot read anything). A read-back
// that fails, or does not end within ConfirmLimit, says the lease is not
// known: the commit may still have landed.
func confirmClaim(ctx context.Context, db DB, claim *Claim, cerr error) (*Claim, error) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ConfirmLimit)
	defer cancel()
	var leased bool
	err := InTx(rctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(rctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR SHARE`, claim.TaskID); err != nil {
			return err
		}
		return tx.QueryRow(rctx, `SELECT EXISTS (SELECT 1 FROM agentrt.attempt WHERE attempt_id = $1)`, claim.AttemptID).Scan(&leased)
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("store: the commit of the claim of task %s failed (%w), and whether attempt %s was leased is not known: %v",
			claim.TaskID, cerr, claim.AttemptID, err)
	case !leased:
		return nil, cerr
	}
	return claim, nil
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
	rows, err := tx.Query(ctx, candidateColumns+`
	  WHERE t.state = 'ready' AND m.state = 'active' AND t.effect_class = ANY($1)
	    AND ($2::timestamptz IS NULL OR (t.created_at, t.task_id) > ($2::timestamptz, $3::text))
	  ORDER BY t.created_at, t.task_id LIMIT $4`, effects, at, id, candidatePage)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCandidate)
}

const candidateColumns = `SELECT t.task_id, t.mission_id, t.role, t.spec_artifact_id,
	  COALESCE(t.subject_task_id, ''), COALESCE(t.base_commit, ''), COALESCE(t.repo, ''), t.effect_class,
	  t.core OR COALESCE(s.core, false), ARRAY(SELECT jsonb_array_elements_text(t.needs_json)),
	  ARRAY(SELECT jsonb_array_elements_text(t.route_json)), t.hard_limit_s, t.created_at, COALESCE(m.envelope_rule_id, ''), m.tenant, t.task_class
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id`

func scanCandidate(row pgx.CollectableRow) (candidate, error) {
	var c candidate
	err := row.Scan(&c.id, &c.mission, &c.role, &c.spec, &c.subject, &c.base, &c.repo, &c.effect, &c.core, &c.needs, &c.route,
		&c.limitS, &c.created, &c.envelope, &c.tenant, &c.class)
	return c, err
}

// candidateOf reads one task as a candidate, whatever its state.
func candidateOf(ctx context.Context, tx pgx.Tx, taskID string) (candidate, error) {
	rows, err := tx.Query(ctx, candidateColumns+` WHERE t.task_id = $1`, taskID)
	if err != nil {
		return candidate{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanCandidate)
}

// tryCandidate leases c to the first tool that may take it, or returns nil.
// What it locks for a candidate it does not take is released at once by
// rolling back to a savepoint, so a skipped candidate holds nothing until the
// claim commits (review 148 B1).
func tryCandidate(ctx context.Context, tx pgx.Tx, r ClaimRequest, cp capabilities, c candidate, ttl time.Duration, rc ruleCache, st *pickState) (*Claim, error) {
	hook("candidate", c.id)
	_, _, found, err := pickTool(ctx, tx, r.Tools, cp, c, st)
	if err != nil || !found {
		return nil, err
	}
	hook("picked", c.id)
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	cl, err := take(ctx, sp, r, cp, c, ttl, rc, st)
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
// meanwhile (review 149 A2). Everything the route and the hard checks read is
// then fixed until the claim commits: the task's route by the task lock, the
// subject's attempts by the subject lock, candidates and restrictions by the
// capability lock; so the task is read again and the tool picked again, and
// that pick is the one leased (review 154 A1).
// Then the mission: the claim leases only if it is still active and no
// control holds the task. The mission row is shared by claims and taken
// exclusively by MoveMission and by a control on the mission, and the task
// row by a control on the task, so these checks are exact: a pause or stop
// either commits before the claim or is seen by it (review 148 A2). A plan
// task takes the mission row exclusively, so two plan tasks of one mission
// never lease together (task_one_live_plan).
func take(ctx context.Context, tx pgx.Tx, r ClaimRequest, cp capabilities, c candidate, ttl time.Duration, rc ruleCache, st *pickState) (*Claim, error) {
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
	}
	if c, err = candidateOf(ctx, tx, c.id); err != nil {
		return nil, err
	}
	tl, why, found, err := pickTool(ctx, tx, r.Tools, cp, c, st)
	if err != nil || !found {
		return nil, err
	}
	// Held exclusively, so the claims of one mission count its attempts one
	// at a time and the last one its budget allows goes to one host (review
	// 186 A3).
	var m missionBudget
	if err = tx.QueryRow(ctx, `SELECT state, tenant, budget_json, created_at, COALESCE(envelope_rule_id, '') FROM agentrt.mission
	  WHERE mission_id = $1 FOR NO KEY UPDATE`, c.mission).Scan(&m.state, &m.tenant, &m.budget, &m.created, &m.envelope); err != nil {
		return nil, err
	}
	if m.state != "active" {
		return nil, nil
	}
	limit, fits, err := m.allows(ctx, tx, rc, c)
	if err != nil || !fits {
		return nil, err
	}
	c.limitS = limit
	scopes := []scope{{"mission", c.mission}, {"task", c.id}}
	if c.subject != "" {
		// A check of a stopped or paused task stops with it (T09).
		scopes = append(scopes, scope{"task", c.subject})
	}
	stop, err := held(ctx, tx, scopes, "stop", "pause")
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
	return lease(ctx, tx, r.HostID, tl, c, ttl, why)
}

// pickTool returns the host tool the claim uses and why (pick.go): of the
// task's route (route_json), each candidate still active or on probation
// that one of the host's tools matches and neither the hard checks
// (routeInput.exclusion) nor the competence step (unfit) exclude now offers
// the first such tool; the pick among them goes by standing, quota and
// rotation. A task with an empty route goes to nobody.
func pickTool(ctx context.Context, tx pgx.Tx, tools []Tool, cp capabilities, c candidate, st *pickState) (Tool, map[string]any, bool, error) {
	if len(c.route) == 0 {
		return Tool{}, nil, false, nil
	}
	in, err := inputWith(ctx, tx, cp, c)
	if err != nil {
		return Tool{}, nil, false, err
	}
	// An executor that just failed the task waits, and one that failed it
	// failuresToStop times in a row is passed by (TD-381, retry.go).
	run, err := readFailureRun(ctx, tx, c.id)
	if err != nil {
		return Tool{}, nil, false, err
	}
	byID := map[string]jobCandidate{}
	for _, jc := range in.candidates {
		byID[jc.id] = jc
	}
	var opts []pick
	for pos, id := range c.route {
		jc, ok := byID[id]
		if !ok {
			continue
		}
		for _, tl := range tools {
			if jc.fits(tl) && len(in.exclusion(tl)) == 0 && len(in.unfit(jc)) == 0 && run.holdsBack(tl.Executor) == "" {
				tier := 0
				if jc.status == "probation" {
					tier = 1
				}
				opts = append(opts, pick{tool: tl, jc: jc, pos: pos, tier: tier})
				break
			}
		}
	}
	best, why, err := st.choose(ctx, tx, opts, c.tenant)
	if err != nil || best == nil {
		return Tool{}, nil, false, err
	}
	return best.tool, why, true, nil
}

func lease(ctx context.Context, tx pgx.Tx, hostID string, tl Tool, c candidate, ttl time.Duration, why map[string]any) (*Claim, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt_no), 0) + 1 FROM agentrt.attempt WHERE task_id = $1`, c.id).Scan(&n); err != nil {
		return nil, err
	}
	id, epoch, err := leaseAttemptWith(ctx, tx, "controller", c.mission, c.id, tl, hostID, n, ttl, why)
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
	if err = tx.QueryRow(ctx, `SELECT a.lease_expires_at, COALESCE(m.work_id, '') FROM agentrt.attempt a
	  JOIN agentrt.mission m ON m.mission_id = $2 WHERE a.attempt_id = $1`, id, c.mission).Scan(&cl.LeaseExpiresAt, &cl.WorkID); err != nil {
		return nil, err
	}
	switch {
	case c.role == "implement":
		cl.Failed, err = failedChecks(ctx, tx, c.id, n)
	case c.subject != "":
		cl.SubjectReport, cl.SubjectEffect, err = subjectOf(ctx, tx, c.spec)
	}
	return cl, err
}
