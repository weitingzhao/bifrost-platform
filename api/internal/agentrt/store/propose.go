package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Proposals (v5 §5.2 「路由草图」): the missions and tasks that threads and
// seats propose, the actor a thread writes as, and the dispatch pass that
// makes a proposed task ready once it may run.

// ErrInvalid: the request breaks a rule of its own (a field missing or out
// of range, a writer that may not write); nothing was written.
var ErrInvalid = errors.New("store: invalid request")

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

// Writer is who a write is recorded as (Actor) and the token that made it
// (Token, the principal's name). A thread names itself, so its registration
// keeps the token that first wrote as it (review 175 B3).
type Writer struct{ Actor, Token string }

// kindOf is the kind of actor a name says it is ("" when it says none).
func kindOf(actor string) string {
	switch {
	case actor == "owner", actor == "controller":
		return actor
	case strings.HasPrefix(actor, "thread:"):
		return "thread"
	case strings.HasPrefix(actor, "seat:"):
		return "seat"
	case strings.HasPrefix(actor, "host:"):
		return "host"
	case strings.HasPrefix(actor, "exec:"):
		return "executor"
	}
	return ""
}

// ThreadsPerDay bounds how many threads are registered in a day: a session
// is what a caller says it is, so without a bound any token could grow the
// actor table and the system stream without end (review 175 B3).
const ThreadsPerDay = 200

// ensureWriter checks the writer's actor in the caller's transaction: it is
// of the kind its name says and not retired. A thread is registered the
// first time it writes (actor_registered by the controller, naming the
// token), within ThreadsPerDay; any other actor must be registered already.
func ensureWriter(ctx context.Context, tx pgx.Tx, w Writer) error {
	want := kindOf(w.Actor)
	if want == "" {
		return invalid("%q is not an actor name", w.Actor)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742107, hashtext($1))`, w.Actor); err != nil {
		return err
	}
	var kind string
	var retired bool
	err := tx.QueryRow(ctx, `SELECT actor_kind, retired_at IS NOT NULL FROM agentrt.actor WHERE actor_id = $1`, w.Actor).Scan(&kind, &retired)
	switch {
	case err == nil && retired:
		return invalid("actor %s is retired", w.Actor)
	case err == nil && kind != want:
		return invalid("actor %s is a %s, not a %s", w.Actor, kind, want)
	case err == nil:
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	case want != "thread":
		return invalid("actor %s is not registered", w.Actor)
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.actor WHERE actor_kind = 'thread'
	  AND created_at > clock_timestamp() - interval '24 hours'`).Scan(&recent); err != nil {
		return err
	}
	if recent >= ThreadsPerDay {
		return invalid("%d threads were registered in the last day, the most there may be; %s must wait", recent, w.Actor)
	}
	if _, err = Append(ctx, tx, Event{Stream: "system", Type: "actor_registered", Actor: "controller",
		Body: map[string]any{"actors": []string{w.Actor}, "note": "first write of a thread, by token " + w.Token}}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'thread', $2)`,
		w.Actor, fmt.Sprintf("Thread %s (first written by token %s)", w.Actor[len("thread:"):], w.Token))
	return err
}

// ActorKind is the kind of a registered actor that is not retired;
// pgx.ErrNoRows when there is none.
func ActorKind(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
	var kind string
	err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1 AND retired_at IS NULL`, actor).Scan(&kind)
	return kind, err
}

// MissionProposal is a mission a thread or seat proposes (POST /missions).
type MissionProposal struct {
	WorkID, Title, Goal string
	Tenant              string    // dev (the default) or business
	Deadline            time.Time // zero: DefaultMissionDays from when it is made
	Success             []string
	Repos               []string
	MaxAction           string // none or branch (the default); round 2 dispatches nothing above branch
	MaxAttempts         int
	MaxWallHours        int
}

// DefaultMissionDays is how long a mission runs when the proposal names no
// deadline (v5 §5.1.2: 默认 7 天).
const DefaultMissionDays = 7

// check is everything about a proposal that does not depend on when it is
// made, with the defaults that do not either.
func (p MissionProposal) check() (MissionProposal, error) {
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
	return p, nil
}

// request is the proposal as its idempotency key's digest sees it: the
// deadline as given ("" when it was left to the default), so a retry is the
// same request whenever it comes.
func (p MissionProposal) request() map[string]any {
	deadline := ""
	if !p.Deadline.IsZero() {
		deadline = timeText(p.Deadline)
	}
	return map[string]any{"work_id": p.WorkID, "title": p.Title, "goal": p.Goal, "tenant": p.Tenant,
		"deadline_at": deadline, "success": nonNil(p.Success), "repos": nonNil(p.Repos),
		"max_action": p.MaxAction, "max_attempts": p.MaxAttempts, "max_wall_hours": p.MaxWallHours}
}

// EnvelopePending is why a proposed mission stays proposed: no signed
// standing envelope says it may start by itself (v5 §5.2: 落在
// standing-envelope 内自动 active; the rule format waits on the Owner).
const EnvelopePending = "no signed standing-envelope rule; the mission waits as proposed"

// ProposeMission records mission_proposed with a number the platform gives
// (M-<n>, one more than the largest there is, under a lock two proposals
// share). The idempotency key is the writer's: a retry with the same key and
// request gets the first answer, even after the deadline has passed; the
// same key with another request is ErrConflict.
func ProposeMission(ctx context.Context, db DB, w Writer, key string, p MissionProposal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	p, err := p.check()
	if err != nil {
		return nil, err
	}
	return idempotent(ctx, db, w.Actor, "missions", key, p.request(), nil, func(tx pgx.Tx) (map[string]any, error) {
		// Only a new proposal is held to the clock (review 175 A4).
		now := time.Now()
		deadline := p.Deadline
		if deadline.IsZero() {
			deadline = now.Add(DefaultMissionDays * 24 * time.Hour)
		} else if !deadline.After(now) {
			return nil, invalid("the deadline %s has passed", deadline.Format(time.RFC3339))
		}
		if err := ensureWriter(ctx, tx, w); err != nil {
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
		if _, err := CreateMission(ctx, tx, w.Actor, Mission{ID: id, WorkID: p.WorkID, Title: p.Title, Goal: p.Goal,
			Deadline: deadline, Tenant: p.Tenant, Success: p.Success, Repos: p.Repos, MaxAction: p.MaxAction,
			MaxAttempts: p.MaxAttempts, MaxWallHours: p.MaxWallHours}); err != nil {
			return nil, err
		}
		return map[string]any{"mission_id": id, "state": "proposed", "waiting": EnvelopePending}, nil
	})
}

// TaskProposal is one task of a POST /missions/{id}/tasks batch. Ref names
// it within the batch; DependsOn holds refs of the batch or ids of tasks of
// the same mission.
type TaskProposal struct {
	Ref                              string
	PlanNode, OffPlanReason, Subject string
	Role, Class, Title, SpecArtifact string
	Effect, Repo, BaseCommit         string
	Core                             bool
	Needs, DependsOn                 []string
	HardLimit                        time.Duration
}

// The roles a proposal may give: verification and review tasks are the
// controller's, made from the task they check (v5 §5.2 验收编排).
var proposableRoles = []string{"plan", "implement", "brief", "other"}

// effectRank orders effect classes; a task's effect is at most its
// mission's max_action.
var effectRank = map[string]int{"none": 0, "branch": 1, "external": 2}

// MaxBatch is the most tasks one proposal holds.
const MaxBatch = 50

// MaxHardLimit is the longest an attempt may be given (zero means the
// default of an hour).
const MaxHardLimit = 24 * time.Hour

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
		case t.HardLimit < 0 || t.HardLimit > MaxHardLimit || t.HardLimit%time.Second != 0:
			return invalid("task %s: hard_limit_s is whole seconds from 0 to %d", t.Ref, int(MaxHardLimit/time.Second))
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

// lockBatch takes, before the batch's first event (review 175 A2), every
// lock its writes take: the dependency graph when it adds an edge, then its
// steps, then the tasks it names outside itself (subjects and dependencies),
// each set in id order. Without it the batch would hold the mission's stream
// when its first dependency asks for the graph lock, the reverse of
// AddDependency's order.
func lockBatch(ctx context.Context, tx pgx.Tx, tasks []TaskProposal) error {
	refs := map[string]bool{}
	var steps, named []string
	edges := false
	for _, t := range tasks {
		refs[t.Ref] = true
	}
	for _, t := range tasks {
		if t.PlanNode != "" {
			steps = append(steps, t.PlanNode)
		}
		if t.Subject != "" {
			named = append(named, t.Subject)
		}
		for _, d := range t.DependsOn {
			edges = true
			if !refs[d] {
				named = append(named, d)
			}
		}
	}
	if edges {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7421000001)`); err != nil {
			return err
		}
	}
	slices.Sort(steps)
	slices.Sort(named)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = ANY($1) ORDER BY plan_node_id FOR SHARE`,
		slices.Compact(steps)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = ANY($1) ORDER BY task_id FOR SHARE`, slices.Compact(named))
	return err
}

// ProposeTasks records a batch of tasks of one mission in one transaction:
// every task_proposed, then every dependency. Any task that does not fit
// (the checks above, the mission's state and max_action, the spec being a
// task_spec artifact, and every guard of the database: the step's mission,
// a dependency cycle) leaves nothing of the batch. Tasks start proposed; the
// dispatch pass makes them ready. A mission that stops while the batch is
// written keeps it proposed: dispatch only readies tasks of active missions.
func ProposeTasks(ctx context.Context, db DB, w Writer, key, missionID string, tasks []TaskProposal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := checkBatch(tasks); err != nil {
		return nil, err
	}
	return idempotent(ctx, db, w.Actor, "missions/"+missionID+"/tasks", key, batchRequest(missionID, tasks), nil,
		func(tx pgx.Tx) (map[string]any, error) {
			if err := lockBatch(ctx, tx, tasks); err != nil {
				return nil, err
			}
			if err := ensureWriter(ctx, tx, w); err != nil {
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
				if _, err := ProposeTask(ctx, tx, w.Actor, Task{ID: id, MissionID: missionID, PlanNode: t.PlanNode,
					OffPlanReason: t.OffPlanReason, Subject: t.Subject, Role: t.Role, Class: t.Class, Title: t.Title,
					SpecArtifact: t.SpecArtifact, Effect: t.Effect, Repo: t.Repo, BaseCommit: t.BaseCommit, Core: t.Core,
					Needs: t.Needs, HardLimit: t.HardLimit}); err != nil {
					return nil, err
				}
				hook("proposed", id)
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
					if _, err := AddDependency(ctx, tx, w.Actor, missionID, ids[t.Ref], on); err != nil {
						return nil, err
					}
				}
			}
			return map[string]any{"mission_id": missionID, "tasks": out}, nil
		})
}

// dispatchPage is how many proposed tasks Dispatch reads at a time.
const dispatchPage = 500

// Dispatch is one pass of readying proposed tasks: a task of an active
// mission whose dependencies are all accepted becomes ready (its route is
// decided and the hosts woken); one with a dependency rejected or cancelled
// is blocked, with that reason. It reads every proposed task of an active
// mission, a page at a time after a cursor, so tasks that wait never keep
// later ones from being looked at (review 175 A3). One task that fails does
// not stop the others. It returns how many tasks it moved.
func Dispatch(ctx context.Context, db DB) (int, error) {
	moved := 0
	var errs []error
	var at any
	var after string
	for {
		type row struct {
			id      string
			created time.Time
		}
		var page []row
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id, t.created_at FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
			  WHERE t.state = 'proposed' AND m.state = 'active'
			    AND ($1::timestamptz IS NULL OR (t.created_at, t.task_id) > ($1::timestamptz, $2::text))
			  ORDER BY t.created_at, t.task_id LIMIT $3`, at, after, dispatchPage)
			if err != nil {
				return err
			}
			page, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
				var x row
				return x, r.Scan(&x.id, &x.created)
			})
			return err
		}); err != nil {
			return moved, errors.Join(append(errs, err)...)
		}
		for _, x := range page {
			did, err := dispatchOne(ctx, db, x.id)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", x.id, err))
			}
			if did {
				moved++
			}
		}
		if len(page) < dispatchPage {
			return moved, errors.Join(errs...)
		}
		at, after = page[len(page)-1].created, page[len(page)-1].id
	}
}

func dispatchOne(ctx context.Context, db DB, id string) (bool, error) {
	hook("dispatching", id)
	moved := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		// The task first, exclusively: AddDependency takes it the same way
		// and adds an edge only to a proposed task (review 175 A1), so the
		// edges read below are all there are until this commits.
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
		blocked, waiting := "", false
		for _, d := range deps {
			switch d.state {
			case "accepted":
			case "rejected", "cancelled":
				if blocked == "" {
					blocked = fmt.Sprintf("dependency %s is %s", d.id, d.state)
				}
			default:
				waiting = true
			}
		}
		// The mission last (task before mission), shared, so it cannot stop
		// or pause before this commits; a task of a mission that is not
		// active moves neither way (review 175 B2).
		var mstate string
		if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, t.mission).Scan(&mstate); err != nil {
			return err
		}
		switch {
		case mstate != "active":
			return nil
		case blocked != "":
			moved = true
			return moveTask(ctx, tx, "controller", t, "blocked", blocked)
		case waiting:
			return nil
		}
		moved = true
		return moveTask(ctx, tx, "controller", t, "ready", "")
	})
	return moved && err == nil, err
}
