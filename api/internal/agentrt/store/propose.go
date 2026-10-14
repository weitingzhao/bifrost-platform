package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Proposals (v5 §5.2 「路由草图」): the missions and tasks that threads and
// seats propose, the actor a thread writes as, and the dispatch pass that
// makes a proposed task ready once it may run.

// ErrInvalid: the request breaks a rule of the proposal itself (a field
// missing or out of range); nothing was written.
var ErrInvalid = errors.New("store: invalid proposal")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// threadSession is what a thread names itself by: the X-Bifrost-Session
// header the questions routes already take.
var threadSession = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ThreadActor is the actor of the thread with that session id.
func ThreadActor(session string) (string, error) {
	if !threadSession.MatchString(session) {
		return "", invalid("X-Bifrost-Session %q is not a session id", session)
	}
	return "thread:" + session, nil
}

// EnsureThread registers a thread's actor the first time it writes:
// actor_registered by the controller, in the caller's transaction, so the
// thread's event and its actor row commit together. An actor that exists
// already is left alone; one of another kind is not a thread.
func EnsureThread(ctx context.Context, tx pgx.Tx, actor string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742107, hashtext($1))`, actor); err != nil {
		return err
	}
	var kind string
	err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1`, actor).Scan(&kind)
	if err == nil {
		if kind != "thread" {
			return invalid("actor %s is a %s, not a thread", actor, kind)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = Append(ctx, tx, Event{Stream: "system", Type: "actor_registered", Actor: "controller",
		Body: map[string]any{"actors": []string{actor}, "note": "first write of a thread"}}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'thread', $2)`,
		actor, "Thread "+actor[len("thread:"):])
	return err
}

// ActorKind is the kind of a registered actor; pgx.ErrNoRows when there is
// none.
func ActorKind(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
	var kind string
	err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1 AND retired_at IS NULL`, actor).Scan(&kind)
	return kind, err
}

// MissionProposal is a mission a thread or seat proposes (POST /missions).
type MissionProposal struct {
	WorkID, Title, Goal string
	Tenant              string // dev (the default) or business
	Deadline            time.Time
	Success             []string
	Repos               []string
	MaxAction           string // none or branch (the default); round 2 dispatches nothing above branch
	MaxAttempts         int
	MaxWallHours        int
}

// DefaultMissionDays is how long a mission runs when the proposal names no
// deadline (v5 §5.1.2: 默认 7 天).
const DefaultMissionDays = 7

func (p MissionProposal) check(now time.Time) (MissionProposal, error) {
	if p.Title == "" || p.Goal == "" {
		return p, invalid("a mission has a title and a goal")
	}
	if len(p.Success) == 0 || slices.Contains(p.Success, "") {
		return p, invalid("a mission names its success criteria, none empty")
	}
	if slices.Contains(p.Repos, "") {
		return p, invalid("a repo name is empty")
	}
	if p.Tenant == "" {
		p.Tenant = "dev"
	}
	if p.Tenant != "dev" && p.Tenant != "business" {
		return p, invalid("tenant %q is not dev or business", p.Tenant)
	}
	if p.MaxAction == "" {
		p.MaxAction = "branch"
	}
	if p.MaxAction != "none" && p.MaxAction != "branch" {
		return p, invalid("max_action %q is not none or branch", p.MaxAction)
	}
	if p.MaxAttempts < 0 || p.MaxWallHours < 0 {
		return p, invalid("a budget is not negative")
	}
	if p.Deadline.IsZero() {
		p.Deadline = now.Add(DefaultMissionDays * 24 * time.Hour)
	}
	if !p.Deadline.After(now) {
		return p, invalid("the deadline %s has passed", p.Deadline.Format(time.RFC3339))
	}
	return p, nil
}

func (p MissionProposal) request() map[string]any {
	return map[string]any{"work_id": p.WorkID, "title": p.Title, "goal": p.Goal, "tenant": p.Tenant,
		"deadline_at": timeText(p.Deadline), "success": nonNil(p.Success), "repos": nonNil(p.Repos),
		"max_action": p.MaxAction, "max_attempts": p.MaxAttempts, "max_wall_hours": p.MaxWallHours}
}

// EnvelopePending is why a proposed mission stays proposed: no signed
// standing envelope says it may start by itself (v5 §5.2: 落在
// standing-envelope 内自动 active; the rule format waits on the Owner).
const EnvelopePending = "no signed standing-envelope rule; the mission waits as proposed"

// ProposeMission records mission_proposed with a number the platform gives
// (M-<n>, one more than the largest there is, under a lock two proposals
// share). The idempotency key is the caller's: a retry with the same key and
// request gets the first answer; the same key with another request is
// ErrConflict.
func ProposeMission(ctx context.Context, db DB, actor, key string, p MissionProposal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	// The deadline default is part of the request only when the caller set
	// one: a retry tomorrow of a proposal without a deadline is the same
	// request.
	given := p.Deadline
	p, err := p.check(time.Now())
	if err != nil {
		return nil, err
	}
	req := p.request()
	if given.IsZero() {
		req["deadline_at"] = ""
	}
	return idempotent(ctx, db, actor, "missions", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742108)`); err != nil {
			return nil, err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(substr(mission_id, 3)::int), 0) + 1 FROM agentrt.mission`).Scan(&n); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("M-%d", n)
		hook("numbered", id)
		if _, err := CreateMission(ctx, tx, actor, Mission{ID: id, WorkID: p.WorkID, Title: p.Title, Goal: p.Goal,
			Deadline: p.Deadline, Tenant: p.Tenant, Success: p.Success, Repos: p.Repos, MaxAction: p.MaxAction,
			MaxAttempts: p.MaxAttempts, MaxWallHours: p.MaxWallHours}); err != nil {
			return nil, err
		}
		return map[string]any{"mission_id": id, "state": "proposed", "waiting": EnvelopePending}, nil
	})
}

// ensureWriter: a thread is registered on its first write; any other actor
// must be registered already.
func ensureWriter(ctx context.Context, tx pgx.Tx, actor string) error {
	if len(actor) > len("thread:") && actor[:len("thread:")] == "thread:" {
		return EnsureThread(ctx, tx, actor)
	}
	if _, err := ActorKind(ctx, tx, actor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("actor %s is not registered", actor)
		}
		return err
	}
	return nil
}

// TaskProposal is one task of a POST /missions/{id}/tasks batch. Ref names
// it within the batch; DependsOn holds refs of the batch or ids of tasks of
// the same mission.
type TaskProposal struct {
	Ref                                     string
	PlanNode, OffPlanReason, Subject        string
	Role, Class, Title, SpecArtifact        string
	Effect, Repo, BaseCommit                string
	Core                                    bool
	Needs, DependsOn                        []string
	HardLimit                               time.Duration
}

// The roles a proposal may give: verification and review tasks are the
// controller's, made from the task they check (v5 §5.2 验收编排).
var proposableRoles = []string{"plan", "implement", "brief", "other"}

// effectRank orders effect classes; a task's effect is at most its
// mission's max_action.
var effectRank = map[string]int{"none": 0, "branch": 1, "external": 2}

// MaxBatch is the most tasks one proposal holds.
const MaxBatch = 50

func checkBatch(tasks []TaskProposal) error {
	if len(tasks) == 0 || len(tasks) > MaxBatch {
		return invalid("a proposal holds 1 to %d tasks", MaxBatch)
	}
	refs := map[string]bool{}
	for i, t := range tasks {
		switch {
		case t.Ref == "":
			return invalid("task %d has no ref", i)
		case refs[t.Ref]:
			return invalid("ref %q is used twice", t.Ref)
		case !slices.Contains(proposableRoles, t.Role):
			return invalid("task %s: role %q may not be proposed (one of %v)", t.Ref, t.Role, proposableRoles)
		case t.Class == "" || t.Title == "" || t.SpecArtifact == "":
			return invalid("task %s has a task_class, a title and a spec_artifact_id", t.Ref)
		case (t.PlanNode == "") == (t.OffPlanReason == ""):
			return invalid("task %s names its plan_node_id or, off the plan, its off_plan_reason (one of them)", t.Ref)
		case t.HardLimit < 0:
			return invalid("task %s: hard_limit_s is not negative", t.Ref)
		}
		if _, ok := effectRank[t.Effect]; !ok {
			return invalid("task %s: effect_class %q is not none, branch or external", t.Ref, t.Effect)
		}
		refs[t.Ref] = true
	}
	return nil
}

func batchRequest(missionID string, tasks []TaskProposal) map[string]any {
	list := make([]any, 0, len(tasks))
	for _, t := range tasks {
		list = append(list, map[string]any{"ref": t.Ref, "plan_node_id": t.PlanNode, "off_plan_reason": t.OffPlanReason,
			"subject_task_id": t.Subject, "role": t.Role, "task_class": t.Class, "title": t.Title,
			"spec_artifact_id": t.SpecArtifact, "effect_class": t.Effect, "repo": t.Repo, "base_commit": t.BaseCommit,
			"core": t.Core, "needs": nonNil(t.Needs), "depends_on": nonNil(t.DependsOn), "hard_limit_s": int64(t.HardLimit / time.Second)})
	}
	return map[string]any{"mission_id": missionID, "tasks": list}
}

// ProposeTasks records a batch of tasks of one mission in one transaction:
// every task_proposed, then every dependency. Any task that does not fit
// (the checks above, the mission's state and max_action, the spec being a
// task_spec artifact, and every guard of the database: the step's mission,
// a dependency cycle) leaves nothing of the batch. Tasks start proposed; the
// dispatch pass makes them ready. A mission that stops while the batch is
// written keeps it proposed: dispatch only readies tasks of active missions.
func ProposeTasks(ctx context.Context, db DB, actor, key, missionID string, tasks []TaskProposal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := checkBatch(tasks); err != nil {
		return nil, err
	}
	return idempotent(ctx, db, actor, "missions/"+missionID+"/tasks", key, batchRequest(missionID, tasks), nil,
		func(tx pgx.Tx) (map[string]any, error) {
			if err := ensureWriter(ctx, tx, actor); err != nil {
				return nil, err
			}
			var state string
			var maxAction *string
			err := tx.QueryRow(ctx, `SELECT state, scope_json->>'max_action' FROM agentrt.mission WHERE mission_id = $1`,
				missionID).Scan(&state, &maxAction)
			if err != nil {
				return nil, err
			}
			if !slices.Contains([]string{"proposed", "awaiting_owner", "active", "paused"}, state) {
				return nil, fmt.Errorf("%w: mission %s is %s and takes no new tasks", ErrState, missionID, state)
			}
			ceiling := "branch"
			if maxAction != nil {
				ceiling = *maxAction
			}
			ids := map[string]string{}
			out := make([]any, 0, len(tasks))
			for _, t := range tasks {
				if effectRank[t.Effect] > effectRank[ceiling] {
					return nil, invalid("task %s: effect_class %s is above the mission's max_action %s", t.Ref, t.Effect, ceiling)
				}
				var kind string
				if err := tx.QueryRow(ctx, `SELECT artifact_kind FROM agentrt.artifact WHERE artifact_id = $1`, t.SpecArtifact).Scan(&kind); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return nil, invalid("task %s: spec %s is not stored (upload it first)", t.Ref, t.SpecArtifact)
					}
					return nil, err
				}
				if kind != "task_spec" {
					return nil, invalid("task %s: spec %s is a %s, not a task_spec", t.Ref, t.SpecArtifact, kind)
				}
				id := NewTaskID()
				if _, err := ProposeTask(ctx, tx, actor, Task{ID: id, MissionID: missionID, PlanNode: t.PlanNode,
					OffPlanReason: t.OffPlanReason, Subject: t.Subject, Role: t.Role, Class: t.Class, Title: t.Title,
					SpecArtifact: t.SpecArtifact, Effect: t.Effect, Repo: t.Repo, BaseCommit: t.BaseCommit, Core: t.Core,
					Needs: t.Needs, HardLimit: t.HardLimit}); err != nil {
					return nil, err
				}
				ids[t.Ref] = id
				out = append(out, map[string]any{"ref": t.Ref, "task_id": id})
			}
			for _, t := range tasks {
				for _, d := range t.DependsOn {
					on, inBatch := ids[d]
					if !inBatch {
						var m string
						if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, d).Scan(&m); err != nil {
							if errors.Is(err, pgx.ErrNoRows) {
								return nil, invalid("task %s depends on %q, which is neither a ref of this proposal nor a task", t.Ref, d)
							}
							return nil, err
						}
						if m != missionID {
							return nil, invalid("task %s depends on %s of mission %s; dependencies stay within a mission", t.Ref, d, m)
						}
						on = d
					}
					if _, err := AddDependency(ctx, tx, actor, missionID, ids[t.Ref], on); err != nil {
						return nil, err
					}
				}
			}
			return map[string]any{"mission_id": missionID, "tasks": out}, nil
		})
}

// Dispatch is one pass of readying proposed tasks: a task of an active
// mission whose dependencies are all accepted becomes ready (its route is
// decided and the hosts woken); one with a dependency rejected or cancelled
// is blocked, with that reason. One task that fails does not stop the
// others. It returns how many tasks it moved.
func Dispatch(ctx context.Context, db DB) (int, error) {
	var ids []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
		  WHERE t.state = 'proposed' AND m.state = 'active' ORDER BY t.created_at, t.task_id LIMIT 500`)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		return 0, err
	}
	moved := 0
	var errs []error
	for _, id := range ids {
		did, err := dispatchOne(ctx, db, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		}
		if did {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

func dispatchOne(ctx context.Context, db DB, id string) (bool, error) {
	hook("dispatching", id)
	moved := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		t, err := readTask(ctx, tx, id, true)
		if err != nil || t.state != "proposed" {
			return err
		}
		// The dependencies, after the task (a dependent before what it waits
		// for), held so none changes state before this commits.
		rows, err := tx.Query(ctx, `SELECT s.task_id, s.state FROM agentrt.task_dependency d
		  JOIN agentrt.task s ON s.task_id = d.depends_on_task_id WHERE d.task_id = $1 ORDER BY s.task_id FOR SHARE OF s`, id)
		if err != nil {
			return err
		}
		type dep struct{ id, state string }
		deps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (dep, error) {
			var d dep
			return d, r.Scan(&d.id, &d.state)
		})
		if err != nil {
			return err
		}
		waiting := false
		for _, d := range deps {
			switch d.state {
			case "accepted":
			case "rejected", "cancelled":
				moved = true
				return moveTask(ctx, tx, "controller", t, "blocked", fmt.Sprintf("dependency %s is %s", d.id, d.state))
			default:
				waiting = true
			}
		}
		// The mission last (task before mission), shared, so it cannot stop
		// or pause before this commits.
		var mstate string
		if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, t.mission).Scan(&mstate); err != nil {
			return err
		}
		if waiting || mstate != "active" {
			return nil
		}
		moved = true
		return moveTask(ctx, tx, "controller", t, "ready", "")
	})
	return moved && err == nil, err
}
