package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The live landing's second gate and its result (S3 B1, CUT §2.5 steps 6–7,
// §11.4). Three steps, never one: the authorization transaction here writes
// reserved and main_intent; the push to main is the caller's, outside any
// transaction, only by the caller that committed this intent and only before
// its absolute deadline; RecordPushResult writes what the remote said under
// the stream lock, accepting whatever the main reconciler wrote first.

// LiveObservation is what the gate reads outside the database under its
// locks: main's head, the latest CI for R, and the release window.
type LiveObservation struct {
	Main          string
	CI            landing.CIRun
	WindowBlocked bool
}

// Authorization is a committed main_intent: the only licence to push, held
// by the caller that wrote it. Deadline is the intent's time plus
// PushTimeout; the push never starts at or past it.
type Authorization struct {
	Repo, Op, Root, R, M0 string
	IntentSeq             int64
	IntentAt              time.Time
	Deadline              time.Time
	RuleVersion           int
	QuotaDay              string
}

// ErrLandingDayChanged: the UTC day turned between the gate's locks and its
// decision; the transaction rolls back whole and the next pass takes the
// new day's quota lock first (review 660 R-03).
var ErrLandingDayChanged = errors.New("landing gate: the UTC day changed during the gate")

// AuthorizeLandingPush runs the live second gate for op in one transaction,
// in the global lock order: capability (shared), the tenant's landing rule
// (shared), the plan step, the task, the mission (FOR SHARE), the day's
// quota stream, the repository stream. Under them it reads the facts (Git
// included) and observes main, CI and the window, then takes the decision
// time (CUT §9.7) and selects the rule again at it. It writes no intent unless
// every check passes: the repository is not stopped (B6), the operation is a
// live branch_pushed with R on M0, EligibleToPush, main is still M0, the
// window is free, and R's latest CI, created after prepared, succeeded. Then
// it records ci_seen when necessary. A core/pinned soft failure writes
// awaiting_grant with no quota reservation. Otherwise it writes reserved on
// the day's quota stream (once per root) and main_intent carrying
// landing_gate (the rule version, the decision time, the quota day and that
// CI: what the authorization rests on, review 662 N-02); and returns the
// Authorization, whose deadline counts from the intent's own time. A refusal is a Decision and no
// error; the caller parks or fails the operation on its next pass.
func AuthorizeLandingPush(ctx context.Context, db DB, src LandingSource, op string, observe func(context.Context, landing.Operation) (LiveObservation, error), repos ...string) (Authorization, landing.Decision, error) {
	return authorizeLandingPush(ctx, db, src, op, observe, false, landing.Repository(repos...))
}

// RecheckWaitingLanding runs the same gate for a waiting operation that has
// no usable grant (review 694 R-06, §5.1 step 4): when the rule now in force
// fails nothing, it writes main_intent on the rule's authority
// (landing_rule_clear), after every unwaivable check; otherwise it writes
// nothing and the operation keeps waiting. The controller calls it once per
// rule version per operation.
func RecheckWaitingLanding(ctx context.Context, db DB, src LandingSource, op string, observe func(context.Context, landing.Operation) (LiveObservation, error), repos ...string) (Authorization, landing.Decision, error) {
	return authorizeLandingPush(ctx, db, src, op, observe, true, landing.Repository(repos...))
}

func authorizeLandingPush(ctx context.Context, db DB, src LandingSource, op string, observe func(context.Context, landing.Operation) (LiveObservation, error), recheck bool, repo string) (Authorization, landing.Decision, error) {
	stream := "landing:" + repo
	if !landing.RepoSupported(repo) {
		return Authorization{}, landing.Decision{Reason: "repo"}, nil
	}
	var auth Authorization
	var decision landing.Decision
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		auth, decision = Authorization{}, landing.Decision{}
		state, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, exists := state.Operations[op]
		if !exists {
			return fmt.Errorf("unknown landing operation")
		}
		var tenant, step, mission string
		if err = tx.QueryRow(ctx, `SELECT m.tenant, COALESCE(t.plan_node_id,''), m.mission_id FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, o.Task).Scan(&tenant, &step, &mission); err != nil {
			return err
		}
		hook("landing gate: binding read", op)
		day := DecisionAt().UTC().Format("2006-01-02")
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1, hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
			return err
		}
		if err = lockLandingBinding(ctx, tx, o.Task, tenant, step, mission); err != nil {
			return err
		}
		quota := "landing-quota:" + tenant + ":" + day
		if err = lockStream(ctx, tx, quota); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		if stopped, err := repoStopped(ctx, tx, stream); err != nil || stopped {
			decision = landing.Decision{Reason: "stopped"}
			return err
		}
		state, err = LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o = state.Operations[op]
		if !rules.GrantSupported(repo) && o.Phase == "awaiting_grant" {
			decision = landing.Decision{Reason: "floor"}
			return nil
		}
		// C-5: a waiting operation goes on only with a grant the stream
		// shows usable (same candidate, unrevoked, unconsumed, unexpired);
		// without one the gate reads nothing outside the database.
		waiting := o.Phase == "awaiting_grant"
		var grant *grantView
		if waiting {
			why := landing.RootBound(state, o.Task, DecisionAt(), false)
			if why == "" && state.LatestOperation(o.Task) == op {
				_, order, err := readLandingGrants(ctx, tx, stream)
				if err != nil {
					return err
				}
				grant = candidateGrant(order, op, o, DecisionAt())
			}
			if why != "" || grant == nil && (!recheck || state.LatestOperation(o.Task) != op) {
				if why == "" {
					why = "awaiting_grant"
				}
				decision = landing.Decision{Reason: why}
				return nil
			}
		}
		f, err := LandingFacts(ctx, tx, src, "prod", "live", o.Task, op, "", DecisionAt())
		if err != nil {
			return err
		}
		if f.Repo != repo {
			return fmt.Errorf("landing operation repository mismatch")
		}
		o = f.Operation
		if waiting && o.Phase != "awaiting_grant" || !waiting && o.Phase != "branch_pushed" || o.DryRun || o.R == "" || o.M0 == "" {
			decision = landing.Decision{Reason: "not_ready"}
			return nil
		}
		obs, err := observe(ctx, o)
		if err != nil {
			return err
		}
		// The reads above took as long as they took: decide now, on the
		// rule in force now (CUT §9.7, review 656 B-B1).
		final := DecisionAt()
		if final.UTC().Format("2006-01-02") != day {
			return ErrLandingDayChanged
		}
		g, _, err := landingFactsDB(ctx, tx, src, "prod", "live", o.Task, op, final)
		if err != nil {
			return err
		}
		g.SingleCommit, g.Changes, g.History, g.DependenciesEffective = f.SingleCommit, f.Changes, f.History, f.DependenciesEffective
		// The root's six hours (and eight operations) as of final, under the
		// stream lock (review 662 B-02): once main_intent is written only the
		// reconciler ends it, so a slow read must not carry it past its
		// window. The CI step's own check ends it failed{bound} next pass.
		budget, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		if why := landing.RootBound(budget, o.Task, final, false); why != "" {
			decision = landing.Decision{Reason: why}
			return nil
		}
		if decision = landing.EligibleToClaim(g, final); !decision.Allowed {
			return nil
		}
		if grant != nil && !g.GrantEnabled {
			// The Owner's signed external host entry is still in force: its
			// removal (a tightening) stops grants at once (§2.1).
			decision = landing.Decision{Reason: "grant_not_enabled"}
			return nil
		}
		// A grant's CI identity is judged before the ordinary CI check
		// (review 698 R-02): a newer run of R than the one approved, whether
		// running, red or green, ends the grant on the record (W runs again,
		// the status says so); evidence not readable yet decides nothing.
		var grantKey *GrantCIKey
		var grantRun GrantCIEvidence
		if grant != nil {
			if grantKey, err = landingCIKeyOf(ctx, tx, op); err != nil {
				return err
			}
			if grantKey == nil || src.GrantPatch == nil || src.GrantCI == nil {
				decision = landing.Decision{Reason: "grant_evidence"}
				return nil
			}
			if grantRun, err = readGrantCI(ctx, src, *grantKey, o.PreparedAt); err != nil {
				return err
			}
			// A PipelineRun name can be reused after deletion. Require a
			// complete object identity and a newer creation time;
			// missing, older or future-dated evidence cannot retire a grant.
			approvedCreatedAt, parseErr := time.Parse(time.RFC3339Nano, grant.CI.CreatedAt)
			if parseErr == nil && grant.CI.Run != "" && grant.CI.UID != "" &&
				grantRun.Run != "" && grantRun.UID != "" &&
				(grantRun.Run != grant.CI.Run || grantRun.UID != grant.CI.UID) &&
				grantRun.CreatedAt.After(approvedCreatedAt) && !grantRun.CreatedAt.After(final) {
				decision = landing.Decision{Reason: "grant_stale"}
				return refuseGrant(ctx, tx, grant, "grant_stale", final, mission)
			}
		}
		switch {
		case obs.Main != o.M0 && waiting:
			// §5.4: a waiting operation for a main that moved is spent, and
			// its grant with it; the next claim prepares k+1.
			if !landing.ValidSHA(obs.Main) {
				decision = landing.Decision{Reason: "main_changed"}
				return nil
			}
			decision = landing.Decision{Reason: "main_changed"}
			_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
				Body: map[string]any{"text": "Landing granted operation discarded: main advanced to " + obs.Main,
					"landing": map[string]any{"v": landing.Version, "kind": "discarded", "op": op, "why": "main_changed"}}})
			return err
		case obs.Main != o.M0:
			decision = landing.Decision{Reason: "main_changed"}
			return nil
		case obs.WindowBlocked:
			decision = landing.Decision{Reason: "window"}
			return nil
		case obs.CI.R != o.R || obs.CI.Status != "Succeeded" || !obs.CI.CreatedAt.After(o.PreparedAt) || obs.CI.CreatedAt.After(final):
			decision = landing.Decision{Reason: "ci"}
			if repo == "bifrost-trade-infra" {
				decision.Reason = "ci_unknown"
				if obs.CI.Status == "Failed" {
					decision.Reason = "ci_red"
				}
			}
			return nil
		}
		// main_intent needs R's success on the stream (protocol): the CI the
		// gate decided on, recorded unless it already is.
		if landing.CIChanged(o.CI, obs.CI) {
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
				Body: map[string]any{"text": "Landing ci_seen at the second gate", "landing": landing.CIRecord(op, obs.CI)}}); err != nil {
				return err
			}
		}
		_, soft := landing.PolicyChecks(g, final)
		var use map[string]any
		rule := GrantRule{ID: g.Rule.ID, Version: int64(g.Rule.Version)}
		if waiting {
			if err = tx.QueryRow(ctx, `SELECT encode(sha256(convert_to(body_text,'UTF8')),'hex') FROM agentrt.rule WHERE rule_id=$1 AND rule_version=$2`,
				g.Rule.ID, g.Rule.Version).Scan(&rule.SHA256); err != nil {
				return err
			}
		}
		var clear map[string]any
		if waiting && grant == nil {
			// §5.1 step 4 (review 694 R-06): the rule in force fails nothing
			// now: main_intent on the rule's authority; otherwise wait on.
			if len(soft) > 0 {
				decision = landing.Decision{Reason: "awaiting_grant"}
				return nil
			}
			clear = toBody(LandingRuleClear{V: LandingGrantVersion, Op: op, Rule: rule, ChecksVersion: rules.LandingChecksVersion,
				DecidedAt: final.UTC().Format(time.RFC3339Nano)})
		}
		if grant != nil {
			// §3.3 / §5.5: the grant is usable only for this very candidate
			// (its digest recomputed now, on the strict read of its pinned
			// CI and R's patch), before its expiry at the decision, and only
			// when it waives every soft failure under the rule in force now.
			// Otherwise nothing is reserved or pushed; a grant refused for
			// good is recorded spent (review 694 R-05) and a new one needed.
			key, ci := grantKey, grantRun
			if ci.Run == "" || grantCIProblem(o, key, ci, final) {
				decision = landing.Decision{Reason: "grant_evidence"} // nothing newer yet: no verdict on the grant
				return nil
			}
			patch, err := src.GrantPatch(ctx, o.A, o.M0, o.R)
			if err != nil {
				return err
			}
			acc, rev, body, err := readGrantChecks(ctx, tx, o.Task, o.Attempt)
			if err != nil {
				return err
			}
			var stepID string
			if err = tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id,'') FROM agentrt.task WHERE task_id=$1`, o.Task).Scan(&stepID); err != nil {
				return err
			}
			trust := grant.Action == ActionCoreLanding && trustAnswered(body)
			cand, err := buildCandidate(g, stepID, op, o, acc, rev, trust, patch, ci, key, grant.STGRelease).Digest()
			if err != nil {
				return err
			}
			softNow := refusalSet(soft)
			switch {
			case !final.Before(grant.exp):
				decision = landing.Decision{Reason: "grant_expired"}
				return nil
			case grantCIProblem(o, key, ci, final) || cand != grant.CandidateDigest:
				decision = landing.Decision{Reason: "grant_stale"}
				return refuseGrant(ctx, tx, grant, "grant_stale", final, mission)
			case !rules.LandingWaivesCover(soft, refusalsOf(grant.Waives)):
				decision = landing.Decision{Reason: "grant_insufficient"}
				return refuseGrant(ctx, tx, grant, "grant_insufficient", final, mission)
			}
			use = toBody(LandingGrantUse{V: LandingGrantVersion, GrantID: grant.GrantID, CandidateDigest: cand, SoftNow: softNow,
				Rule: rule, ChecksVersion: rules.LandingChecksVersion, DecidedAt: final.UTC().Format(time.RFC3339Nano)})
		} else if len(soft) > 0 {
			decision = landing.Decision{Reason: "awaiting_grant"}
			return appendAwaitingGrant(ctx, tx, g, op, obs.CI, soft)
		}
		q, err := LandingState(ctx, tx, quota)
		if err != nil {
			return err
		}
		// Each record the gate writes is dated on the quota day it locked:
		// one that lands past midnight rolls the whole gate back (review 661
		// R-03), never a partial write on the wrong day.
		onDay := func(a Appended) error {
			if a.At.UTC().Format("2006-01-02") != day {
				return ErrLandingDayChanged
			}
			return nil
		}
		if _, reserved := q.Reservations[o.Task]; !reserved {
			r, err := Append(ctx, tx, Event{Stream: quota, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
				Body: landingReservationBody(repo, op, o.Task)})
			if err == nil {
				err = onDay(r)
			}
			if err != nil {
				return err
			}
		}
		hook("landing gate: reserved", op)
		body := map[string]any{"text": fmt.Sprintf("Landing main_intent; second gate rule version %d", g.Rule.Version),
			"landing": map[string]any{"v": landing.Version, "kind": "main_intent", "op": op, "R": o.R, "M0": o.M0},
			"landing_gate": map[string]any{"v": LandingGateVersion, "op": op, "rule_version": g.Rule.Version,
				"decided_at": final.UTC().Format(time.RFC3339Nano), "quota_day": day,
				"ci_run": obs.CI.Run, "ci_created_at": obs.CI.CreatedAt.UTC().Format(time.RFC3339Nano)}}
		if use != nil {
			// Consumption is this event: no separate consumed record (§5.5).
			body["landing_grant_use"] = use
			body["text"] = fmt.Sprintf("Landing main_intent on grant %s; second gate rule version %d", grant.GrantID, g.Rule.Version)
		}
		if clear != nil {
			body["landing_rule_clear"] = clear
			body["text"] = fmt.Sprintf("Landing main_intent: rule version %d now fails nothing of the waiting operation", g.Rule.Version)
		}
		hook("landing gate: before main_intent", op)
		a, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission, Body: body})
		if err == nil {
			err = onDay(a)
		}
		if err != nil {
			return err
		}
		auth = Authorization{Repo: repo, Op: op, Root: o.Task, R: o.R, M0: o.M0, IntentSeq: a.Seq, IntentAt: a.At,
			Deadline: a.At.Add(PushTimeout), RuleVersion: g.Rule.Version, QuotaDay: day}
		decision = landing.Decision{Allowed: true}
		return nil
	})
	if err != nil {
		return Authorization{}, landing.Decision{}, err
	}
	return auth, decision, nil
}

// LandingGateVersion is the version of the landing_gate record.
const LandingGateVersion = "1"

// badLandingGate is why a landing_gate body is refused, or "".
func badLandingGate(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_gate is not an object"
	}
	if m["v"] != LandingGateVersion {
		return "landing_gate version is not 1"
	}
	for _, k := range []string{"op", "decided_at", "quota_day", "ci_run", "ci_created_at"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_gate needs " + k
		}
	}
	for _, k := range []string{"decided_at", "ci_created_at"} {
		if _, err := time.Parse(time.RFC3339Nano, m[k].(string)); err != nil {
			return "landing_gate " + k + " is not a time"
		}
	}
	if _, err := time.Parse("2006-01-02", m["quota_day"].(string)); err != nil {
		return "landing_gate quota_day is not a day"
	}
	// The grounds agree (review 663 N-02): decided on its quota day, on a
	// CI created no later than the decision.
	decided, _ := time.Parse(time.RFC3339Nano, m["decided_at"].(string))
	created, _ := time.Parse(time.RFC3339Nano, m["ci_created_at"].(string))
	if decided.UTC().Format("2006-01-02") != m["quota_day"] {
		return "landing_gate quota_day is not the decision's day"
	}
	if created.After(decided) {
		return "landing_gate CI is later than the decision"
	}
	// Appends carry Go integers; canonical reads of stored JSONB carry
	// json.Number. Validate both without rounding a stored rule version.
	version, ok := cardNumber(m["rule_version"])
	if !ok {
		return "landing_gate needs rule_version"
	}
	if version < 1 {
		return "landing_gate rule_version is not a version"
	}
	return ""
}

// checkLandingGateAppend: landing_gate rides only on the controller's
// main_intent for the same operation, on the repository stream.
func checkLandingGateAppend(ev Event) error {
	v, exists := ev.Body["landing_gate"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	r, _ := ev.Body["landing"].(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || r == nil || r["kind"] != "main_intent" || r["op"] != m["op"] {
		return fmt.Errorf("landing_gate rides on the controller's main_intent for its operation")
	}
	return nil
}

// AuthorizationCurrent reads, just before the push, whether auth is still
// the operation's open intent (CUT §11.4 B1, review 663 R-01): the
// operation is at main_intent, its intent is the event auth names (seq,
// time, R, M0), and the deadline has not passed. Anything else (the
// reconciler moved it on, a result is recorded, the read fails) and the
// caller pushes nothing; the reconciler settles it. It re-checks the licence
// already granted, not the rules: a rule that changed since does not revoke
// it, and the deadline is the intent's, never a fresh one.
func AuthorizationCurrent(ctx context.Context, db DB, auth Authorization) (bool, error) {
	hook("landing live: before push check", auth.Op)
	stream := "landing:" + landing.Repository(authRepo(auth)...)
	current := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[auth.Op]
		if !ok || o.Phase != "main_intent" || o.R != auth.R || o.M0 != auth.M0 || o.Task != auth.Root {
			return nil
		}
		var at time.Time
		err = tx.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_stream_id = $1 AND seq = $2 AND event_type = 'note'
		  AND body_json->'landing'->>'kind' = 'main_intent' AND body_json->'landing'->>'op' = $3`, stream, auth.IntentSeq, auth.Op).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		current = at.Equal(auth.IntentAt) && DecisionAt().Before(auth.Deadline)
		return nil
	})
	if err != nil {
		return false, err
	}
	return current, nil
}

// checkLandingGateGrounds checks landing_gate against the record it rides
// on and the operation (review 664 N-01): the decision is on the intent's
// own UTC day and no later than it, and the CI it names is the success the
// operation has on record (the stream as it is before this intent, under
// the append's stream lock).
func checkLandingGateGrounds(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	v, exists := ev.Body["landing_gate"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	r, _ := ev.Body["landing"].(map[string]any)
	if m == nil || r == nil {
		return fmt.Errorf("landing_gate rides on its main_intent")
	}
	decided, err := time.Parse(time.RFC3339Nano, fmt.Sprint(m["decided_at"]))
	if err != nil {
		return fmt.Errorf("landing_gate decided_at is not a time")
	}
	if m["quota_day"] != at.UTC().Format("2006-01-02") {
		// The day turned between the gate's decision and its intent: the
		// gate rolls back as ErrLandingDayChanged (review 661 R-03).
		return fmt.Errorf("landing_gate is not on its intent's day: %w", ErrLandingDayChanged)
	}
	if decided.After(at) {
		return fmt.Errorf("landing_gate decided after its intent")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	o := s.Operations[fmt.Sprint(r["op"])]
	created, err := time.Parse(time.RFC3339Nano, fmt.Sprint(m["ci_created_at"]))
	if err != nil || o.CI.String("status") != "Succeeded" || o.CI.String("run") != m["ci_run"] || !o.CI.Time("created_at").Equal(created) {
		return fmt.Errorf("landing_gate names a CI the operation has not succeeded on")
	}
	return nil
}

// ErrLandingBindingChanged: the task's step, mission or tenant changed
// between the gate's first read and its task lock (a plan change rebound
// it). The gate rolls back whole; the next pass locks the new binding.
var ErrLandingBindingChanged = errors.New("landing gate: the task's binding changed during the gate")

// lockLandingBinding takes the plan step, task and mission locks of a
// final gate in the global order, after its capability and rule locks, and
// checks under the task lock that the binding they were chosen from (read
// with no lock) is still the task's (review 662 B-01). A rebind changes the
// task row, so once the task lock is held and the binding matches, the
// locked step and mission are the ones the facts will read.
func lockLandingBinding(ctx context.Context, tx pgx.Tx, task, tenant, step, mission string) error {
	if step != "" {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE`, step); err != nil {
			return err
		}
	}
	var nowStep, nowMission string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id,''), mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, task).Scan(&nowStep, &nowMission); err != nil {
		return err
	}
	var nowTenant string
	if err := tx.QueryRow(ctx, `SELECT tenant FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission).Scan(&nowTenant); err != nil {
		return err
	}
	if nowStep != step || nowMission != mission || nowTenant != tenant {
		return ErrLandingBindingChanged
	}
	return nil
}

// RecordPushResult writes what the remote said about auth's push, under the
// stream lock (CUT §2.5 step 7). The operation must still be at this intent:
// if the main reconciler already moved it (it read main first), its record
// stands and nothing is written; the push is never repeated. ok records
// main_result{ok}: landed follows from main's evidence (§10); denied records
// main_result{denied}, which ReconcileDenied ends failed{denied} with the
// repository stopped (B6); non_ff and unknown are recorded as they are.
func RecordPushResult(ctx context.Context, db DB, auth Authorization, outcome string) (bool, error) {
	stream := "landing:" + landing.Repository(authRepo(auth)...)
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[auth.Op]
		if !ok || o.Phase != "main_intent" || o.R != auth.R {
			return nil // the reconciler moved it first: its record stands
		}
		if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: auth.Root,
			Body: map[string]any{"text": "Landing main_result: " + outcome, "landing": map[string]any{"v": landing.Version, "kind": "main_result", "op": auth.Op, "outcome": outcome}}}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

// repoStopped: a failed{denied} stopped the repository and no Owner's
// resume has answered that stop's generation yet (B6, CUT §11.4).
func repoStopped(ctx context.Context, tx pgx.Tx, stream string) (bool, error) {
	var stopped, resumed int
	err := tx.QueryRow(ctx, `SELECT
	    COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0),
	    COALESCE(max((body_json->'landing_resume'->>'generation')::int), 0)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&stopped, &resumed)
	return stopped > resumed, err
}

// stopRepo records the repository stopped by op's denied push, in the
// transaction that ends it failed{denied}: the stop and the ending commit
// together or not at all (B6).
func stopRepo(ctx context.Context, tx pgx.Tx, stream, repo, op, root string) error {
	var gen int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0) + 1 FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&gen); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": "Landing stopped for the repository: a push to main was denied; the Owner resumes it",
			"landing_stop": map[string]any{"repo": repo, "op": op, "root": root, "generation": gen}}})
	return err
}

// badLandingStop is why a stop or resume body is refused, or "".
func badLandingStop(name string, v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return name + " is not an object"
	}
	if r, _ := m["repo"].(string); r == "" {
		return name + " needs repo"
	}
	g, ok := m["generation"].(int64)
	if !ok {
		if f, isF := m["generation"].(float64); isF && f == float64(int64(f)) {
			g, ok = int64(f), true
		} else if i, isI := m["generation"].(int); isI {
			g, ok = int64(i), true
		}
	}
	if !ok || g < 1 {
		return name + " generation is not a positive integer"
	}
	if name == "landing_stop" {
		if op, _ := m["op"].(string); op == "" {
			return "landing_stop needs op"
		}
		if !landingRoot.MatchString(fmt.Sprint(m["root"])) {
			return "landing_stop root is not a task"
		}
	}
	return ""
}

// checkLandingStopAppend: only the controller stops or resumes, on the
// repository stream, never as a landing record.
func checkLandingStopAppend(ev Event) error {
	_, stop := ev.Body["landing_stop"]
	_, resume := ev.Body["landing_resume"]
	if !stop && !resume {
		return nil
	}
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) {
		return fmt.Errorf("a landing stop or resume is written by the controller on the repository stream")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("a landing stop or resume is not a landing record")
	}
	return nil
}

func authRepo(auth Authorization) []string {
	if auth.Repo == "" {
		return nil
	}
	return []string{auth.Repo}
}

func landingReservationBody(repo, op, root string) map[string]any {
	b := map[string]any{"text": "Landing quota reserved", "landing": map[string]any{"v": landing.Version, "kind": "reserved", "op": op, "root": root}}
	if repo != "bifrost-platform" {
		b["landing_repo"] = repo
	}
	return b
}
