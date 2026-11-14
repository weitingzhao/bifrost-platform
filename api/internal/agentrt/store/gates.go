package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
)

// The architecture table's gates, run by the runtime itself (TD-397; the
// Owner chose B on 2026-10-20 「发版后运行时按架构对照的行派门槛任务，06 上的确定性
// 执行者在部署的提交上跑，记成 gate 验收」; CUT-td397-gate-runs-2026-10-20.md).
//
// A row's done rule names its gates (gates_passed: a test id and the sha256
// of a gate spec, gaterun). Every pass of the facts loop, RunGates
//   - dispatches (dispatchGates): for what the platform's own environment
//     runs as last recorded (deploy_seen, read again live and found the same),
//     each gate of a row's rule not yet dispatched for that deployment gets a
//     task, all of them in one batch mission the controller makes inside the
//     standing envelope; a key (env, commit, deploy_ref, test, spec) is
//     dispatched once, whatever became of its task;
//   - ends what cannot run and closes what has ended (gateend.go), whatever
//     the deployment read or the rules say.
//
// A gate task is a verify task with no subject (0007), class gate, effect
// none, off the plan, its base the deployed commit and its spec the gate's:
// the deterministic executor runs it on that commit as it runs any task
// whose spec has no check fields. Its result is recorded by ConcludeWith as a
// gate verification (gateconclude.go), which GatePassed reads. A gate task
// never gets a blocked card, is never blocked by the budget or withdrawal
// passes, and never reaches the Owner: what cannot run is cancelled with why.
//
// Which tasks are a batch's is the mission's scope_json.gate_run, written
// when the controller makes it and never after: {env, commit, deploy_ref,
// gates: [{task, test, spec_sha256}]}. No route sets it; a task is a gate
// member only if every field of it is what that list says (isGateMember).

const (
	// GateClass is a gate task's task class.
	GateClass = "gate"
	// GateOffPlanReason is why a gate task is off the plan tree.
	GateOffPlanReason = "a gate of the architecture table, run on the commit the environment runs (TD-397)"
	// GateSuccess is a gate batch's success criterion.
	GateSuccess = "each gate's result on the deployed commit is recorded"
	// GateBatchHours bounds a batch: its budget's hours and its deadline.
	GateBatchHours = 6
	// GateUnclaimed is how long a ready gate task waits for a host.
	GateUnclaimed = 2 * time.Hour
)

// gateEntry is one gate of a batch: its task and the gate it runs.
type gateEntry struct{ Task, Test, Spec string }

// gateRun is a batch as its mission's scope_json.gate_run says.
type gateRun struct {
	Env, Commit string
	Ref         map[string]any
	Gates       []gateEntry
}

var (
	commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
	gateIDRe = regexp.MustCompile(`^T[0-9]{2}[a-z]?$`)
	spec64Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// parseGateRun reads a gate_run as the controller writes it, or says it is
// not one: an environment, a 40-character commit, a deploy reference that
// names something, and gates each with a task id, a test id and a spec
// sha256, no task twice.
func parseGateRun(v any) (gateRun, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 4 {
		return gateRun{}, false
	}
	var g gateRun
	g.Env, _ = m["env"].(string)
	g.Commit, _ = m["commit"].(string)
	g.Ref, _ = m["deploy_ref"].(map[string]any)
	list, _ := m["gates"].([]any)
	if !envNames[g.Env] || !commitRe.MatchString(g.Commit) || len(g.Ref) == 0 || len(list) == 0 {
		return gateRun{}, false
	}
	seen := map[string]bool{}
	for _, x := range list {
		e, ok := x.(map[string]any)
		if !ok || len(e) != 3 {
			return gateRun{}, false
		}
		var ge gateEntry
		ge.Task, _ = e["task"].(string)
		ge.Test, _ = e["test"].(string)
		ge.Spec, _ = e["spec_sha256"].(string)
		if !taskIDPattern.MatchString(ge.Task) || !gateIDRe.MatchString(ge.Test) || !spec64Re.MatchString(ge.Spec) || seen[ge.Task] {
			return gateRun{}, false
		}
		seen[ge.Task] = true
		g.Gates = append(g.Gates, ge)
	}
	return g, true
}

var envNames = map[string]bool{"dev": true, "stg": true, "prod": true}

var taskIDPattern = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)

// sameRef is whether two deploy references are the same object.
func sameRef(a, b map[string]any) bool {
	x, err1 := canon.Encode(a)
	y, err2 := canon.Encode(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// isGateMember is whether a task is a member of its gate batch
// (gateMemberOf): the one test of membership, wherever a gate task is kept
// from what an ordinary task gets (cards, the withdrawal and budget passes,
// moves) and wherever its batch uses it (review td397-5 B2).
func isGateMember(ctx context.Context, tx pgx.Tx, taskID string) (bool, error) {
	var mission string
	err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, taskID).Scan(&mission)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	b, ok, err := readGateBatch(ctx, tx, mission, "")
	if err != nil || !ok {
		return false, err
	}
	_, member, err := gateMemberOf(ctx, tx, b, taskID)
	return member, err
}

// gateBatch is a batch mission as read: its run, and its own fields.
type gateBatch struct {
	mission, state, envelope, repo string
	run                            gateRun
	created                        time.Time
}

// readGateBatch reads a mission as a gate batch, with the given lock
// clause; ok is false when it is not one the controller made.
func readGateBatch(ctx context.Context, tx pgx.Tx, mission, lock string) (gateBatch, bool, error) {
	b := gateBatch{mission: mission}
	var by string
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT state, created_by_actor_id, created_at, COALESCE(envelope_rule_id, ''), (scope_json->'gate_run')::text,
	    COALESCE(scope_json->'repos'->>0, '') FROM agentrt.mission WHERE mission_id = $1 `+lock, mission).
		Scan(&b.state, &by, &b.created, &b.envelope, &doc, &b.repo)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (doc == nil || by != "controller" || b.repo == "")) {
		return b, false, nil
	}
	if err != nil {
		return b, false, err
	}
	v, err := canon.Parse(doc)
	if err != nil {
		return b, false, nil
	}
	var ok bool
	b.run, ok = parseGateRun(v)
	return b, ok, nil
}

// gateMemberOf is the entry of task t in batch b, when t is its member:
// every field of the task what the entry and the batch say.
func gateMemberOf(ctx context.Context, tx pgx.Tx, b gateBatch, taskID string) (gateEntry, bool, error) {
	var e gateEntry
	found := false
	for _, x := range b.run.Gates {
		if x.Task == taskID {
			e, found = x, true
		}
	}
	if !found {
		return e, false, nil
	}
	var mission, role, class, effect, spec, repo, base, by string
	var offPlan bool
	var subject *string
	err := tx.QueryRow(ctx, `SELECT mission_id, role, task_class, effect_class, spec_artifact_id, COALESCE(repo, ''), COALESCE(base_commit, ''),
	    created_by_actor_id, off_plan, subject_task_id FROM agentrt.task WHERE task_id = $1`, taskID).
		Scan(&mission, &role, &class, &effect, &spec, &repo, &base, &by, &offPlan, &subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, false, nil
	}
	if err != nil {
		return e, false, err
	}
	ok := mission == b.mission && role == "verify" && class == GateClass && effect == "none" && spec == "sha256:"+e.Spec &&
		repo == b.repo && base == b.run.Commit && by == "controller" && offPlan && subject == nil
	return e, ok, nil
}

// storedGateSpec checks that the spec a gate pins is stored, is a task
// spec, and is a gate spec (gaterun.ParseSpec).
func storedGateSpec(ctx context.Context, tx pgx.Tx, sum string) error {
	var kind string
	var body []byte
	err := tx.QueryRow(ctx, `SELECT a.artifact_kind, b.body FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id)
	  WHERE a.artifact_id = $1`, "sha256:"+sum).Scan(&kind, &body)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("its spec %s is not stored", sum)
	case err != nil:
		return err
	case kind != "task_spec":
		return fmt.Errorf("its spec %s is a %s, not a task_spec", sum, kind)
	}
	_, err = gaterun.ParseSpec(body)
	return err
}
