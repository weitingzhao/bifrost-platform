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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
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
// keeps the token that first wrote as it (review 175 B3), and only that
// token may write as it or read its mail afterwards (TD-370). Relayed is a
// write the runtime makes for the actor from what it sent before (a brief
// answering its mail), not a request: no token is held to the session.
type Writer struct {
	Actor, Token string
	Relayed      bool
}

// ErrNotYours: a thread's session is another token's (TD-370); nothing was
// written or read.
var ErrNotYours = errors.New("store: the session is another token's")

// registeredBy starts the note of a thread's registration, which names the
// token after it; registrations before the token had a field of its own
// (2026-10-19) are read by it.
const registeredBy = "first write of a thread, by token "

// holdsToken checks the writer of a registered thread against the token its
// registration names: a session is the token's that first wrote as it, so
// another token cannot write as the thread or read its mail (TD-370). A
// relayed write is the runtime's own and is not held.
func holdsToken(ctx context.Context, tx pgx.Tx, w Writer) error {
	if w.Relayed {
		return nil
	}
	var token, note string
	err := tx.QueryRow(ctx, `SELECT COALESCE(body_json->>'token', ''), COALESCE(body_json->>'note', '') FROM agentrt.event
	  WHERE event_stream_id = 'system' AND event_type = 'actor_registered' AND body_json->'actors' ? $1
	  ORDER BY event_id LIMIT 1`, w.Actor).Scan(&token, &note)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if t, ok := strings.CutPrefix(note, registeredBy); token == "" && ok {
		token = t
	}
	switch {
	case token == "":
		return fmt.Errorf("%w: %s names no token that registered it, so no token may act as it; a new session can", ErrNotYours, w.Actor)
	case token != w.Token:
		return fmt.Errorf("%w: %s was registered by another token; a thread writes and reads its mail with the token it first wrote with",
			ErrNotYours, w.Actor)
	}
	return nil
}

// holdsSessionTx is the check a thread's write makes (CheckWriter: not
// retired, of its kind, its session held to its token), for a read of its
// mail or a replay of its key, which make no write: a thread not registered
// yet holds nothing here, nor does any other actor. It takes the thread's
// writer lock first and keeps it to the end of the transaction, so a first
// write registering the session cannot commit between this check and the
// read after it (review 320 A2, A3): the read sees the session as it was
// checked. Its callers take no lock a writer waits on while holding the
// writer's lock: a key's lock comes before it, as in a write.
func holdsSessionTx(ctx context.Context, tx pgx.Tx, w Writer) error {
	if k := kindOf(w.Actor); k != "thread" && k != "agent" {
		return nil
	}
	if err := writerLock(ctx, tx, w.Actor); err != nil {
		return err
	}
	_, err := CheckWriter(ctx, tx, w)
	hook("held", w.Actor)
	return err
}

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
	case strings.HasPrefix(actor, "agent:"):
		return "agent" // an external agent bringing issues (0009; holdsWriter, intake.go)
	}
	return ""
}

// ThreadsPerDay bounds how many threads proposals and uploads register in a
// day: a session is what a caller says it is, so without a bound any token
// could grow the actor table and the system stream without end (review 175
// B3). Controls are not bounded: anyone authenticated may stop or pause
// (v5 §5.2), and a new session must not find that taken by proposals
// (review 176 part 2 A1); each control is a row of its own anyway.
const ThreadsPerDay = 200

// CheckWriter checks a writer without writing: its actor is of the kind its
// name says and not retired, and a thread's writer holds its session
// (holdsToken; ErrNotYours). It reports whether the actor is registered; a
// thread that is not is fine (its first write registers it), any other
// actor that is not is ErrInvalid.
func CheckWriter(ctx context.Context, tx pgx.Tx, w Writer) (bool, error) {
	want := kindOf(w.Actor)
	if want == "" {
		return false, invalid("%q is not an actor name", w.Actor)
	}
	var kind string
	var retired bool
	err := tx.QueryRow(ctx, `SELECT actor_kind, retired_at IS NOT NULL FROM agentrt.actor WHERE actor_id = $1`, w.Actor).Scan(&kind, &retired)
	switch {
	case err == nil && retired:
		return true, invalid("actor %s is retired", w.Actor)
	case err == nil && kind != want:
		return true, invalid("actor %s is a %s, not a %s", w.Actor, kind, want)
	case err == nil && (want == "thread" || want == "agent"):
		return true, holdsWriter(ctx, tx, w)
	case err == nil:
		return true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	case want != "thread":
		return false, invalid("actor %s is not registered", w.Actor)
	}
	return false, nil
}

// writerLock serializes the writes of one actor (and its registration). It
// touches no row and no stream, so a transaction may take it before any other
// lock (review 187 R1); taking it again later in the same transaction is a
// no-op.
func writerLock(ctx context.Context, tx pgx.Tx, actor string) error {
	if kindOf(actor) == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742107, hashtext($1))`, actor)
	return err
}

// ensureWriter checks the writer in the caller's transaction and registers a
// thread the first time it writes: actor_registered by the controller,
// naming the token. Every write route calls it before any lock of the rows
// it writes (review 176 A1): its locks are the writer's own, then, for a
// registration, the registration lock and the system stream. bounded holds
// a registration to ThreadsPerDay.
func ensureWriter(ctx context.Context, tx pgx.Tx, w Writer, bounded bool) error {
	if err := writerLock(ctx, tx, w.Actor); err != nil {
		return err
	}
	registered, err := CheckWriter(ctx, tx, w)
	if err != nil || registered {
		return err
	}
	// One registration at a time, so the count below is the count when it
	// commits (review 176 B1).
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742109)`); err != nil {
		return err
	}
	if bounded {
		var recent int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.actor WHERE actor_kind = 'thread'
		  AND created_at > clock_timestamp() - interval '24 hours'`).Scan(&recent); err != nil {
			return err
		}
		hook("counted", w.Actor)
		if recent >= ThreadsPerDay {
			return invalid("%d threads were registered in the last day, the most there may be; %s must wait", recent, w.Actor)
		}
	}
	// The registration names the token the session is held to (TD-370); a
	// relayed write holds it to none.
	body := map[string]any{"actors": []string{w.Actor}, "note": registeredBy + w.Token, "token": w.Token}
	if w.Relayed {
		body = map[string]any{"actors": []string{w.Actor}, "note": "first write of a thread, relayed by the runtime (" + w.Token + ")"}
	}
	if _, err = Append(ctx, tx, Event{Stream: "system", Type: "actor_registered", Actor: "controller", Body: body}); err != nil {
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
	// NoMerge: why its accepted results stay off main, "" for none (a drill,
	// a benchmark replay; TD-383 「不合 A」); scope_json.no_merge, prune.go.
	NoMerge string
	// NeedsOwner: why its cards wait for the Owner himself, "" for none (a
	// mission that must wait for him; TD-390): a card-auto-answer rule never
	// answers them. Walks do not set it (TD-393: the paths behind his buttons
	// are covered by the Console's tests). scope_json.needs_owner.
	NeedsOwner string
	// bases are a goal's, by repository (ProposeGoal); no route sets them.
	bases map[string]string
	// assessmentOf is the stream of the item an assessment's mission
	// assesses (assess.go, TD-391); no route sets it.
	assessmentOf string
	// gateRun is a batch of gate tasks (gates.go, TD-397); no route sets it.
	gateRun   map[string]any
	issueTake bool // take_issue's mission for its issue, the only one a BI- work id may have (issuetake.go); no route sets it
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
	p.NoMerge = strings.TrimSpace(p.NoMerge)
	p.NeedsOwner = strings.TrimSpace(p.NeedsOwner)
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
	req := map[string]any{"work_id": p.WorkID, "title": p.Title, "goal": p.Goal, "tenant": p.Tenant,
		"deadline_at": deadline, "success": nonNil(p.Success), "repos": nonNil(p.Repos),
		"max_action": p.MaxAction, "max_attempts": p.MaxAttempts, "max_wall_hours": p.MaxWallHours}
	if p.NoMerge != "" {
		// Only when given: a key taken before the field was has the same digest.
		req["no_merge"] = p.NoMerge
	}
	if p.NeedsOwner != "" {
		req["needs_owner"] = p.NeedsOwner // only when given, as no_merge
	}
	return req
}

// EnvelopePending is why a proposed mission stays proposed when no signed
// standing envelope is in force for its tenant (v5 §5.2: 落在
// standing-envelope 内自动 active).
const EnvelopePending = "no standing-envelope rule is in force: the mission waits for the Owner"

// ProposeMission records mission_proposed with a number the platform gives
// (M-<n>, one more than the largest there is, under a lock two proposals
// share). The idempotency key is the writer's: a retry with the same key and
// request gets the first answer, even after the deadline has passed; the
// same key with another request is ErrConflict. A mission outside the
// standing envelope in force goes on the Owner's decision card through ask
// and waits as awaiting_owner (missiondecide.go); without ask it stays
// proposed.
func ProposeMission(ctx context.Context, db DB, w Writer, key string, p MissionProposal, ask Ask) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	p, err := p.check()
	if err != nil {
		return nil, err
	}
	return idempotentAs(ctx, db, w, "missions", key, p.request(), nil, func(tx pgx.Tx) (map[string]any, error) {
		// Only a new proposal is held to the bounds: a key taken before
		// them gets its first answer again (review 320 A1).
		if err := p.bounded(); err != nil {
			return nil, err
		}
		return proposeMissionTx(ctx, tx, w, p, key, ask)
	})
}

// proposeMissionTx is ProposeMission's write, in a transaction its caller
// holds (a goal proposes its mission and its plan task in one; goal.go). p
// has been checked. key and ask put a mission outside the envelope on the
// Owner's card; a goal's or a brief's mission passes no ask and stays
// proposed there.
func proposeMissionTx(ctx context.Context, tx pgx.Tx, w Writer, p MissionProposal, key string, ask Ask) (map[string]any, error) {
	// Only a new proposal is held to the clock (review 175 A4).
	now := time.Now()
	deadline := p.Deadline
	if deadline.IsZero() {
		deadline = now.Add(DefaultMissionDays * 24 * time.Hour)
	} else if !deadline.After(now) {
		return nil, invalid("the deadline %s has passed", deadline.Format(time.RFC3339))
	}
	if err := ensureWriter(ctx, tx, w, true); err != nil {
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
	// A proposal stating no budget takes the tenant's budget rule's; one
	// inside the standing envelope in force starts at once (proposal §2).
	asked := p // the proposal as it came, for the Owner's card
	if p.MaxAttempts == 0 && p.MaxWallHours == 0 {
		b, err := inForce(ctx, tx, rules.KindBudget+":"+p.Tenant)
		if err != nil {
			return nil, err
		}
		if b != nil {
			p.MaxAttempts, p.MaxWallHours = b.Budget.Mission.MaxAttempts, b.Budget.Mission.MaxWallHours
		}
	}
	env, err := inForce(ctx, tx, rules.KindEnvelope+":"+p.Tenant)
	if err != nil {
		return nil, err
	}
	if _, err := CreateMission(ctx, tx, w.Actor, Mission{ID: id, WorkID: p.WorkID, Title: p.Title, Goal: p.Goal,
		Deadline: deadline, Tenant: p.Tenant, Success: p.Success, Repos: p.Repos, Bases: p.bases, MaxAction: p.MaxAction,
		MaxAttempts: p.MaxAttempts, MaxWallHours: p.MaxWallHours, NoMerge: p.NoMerge, NeedsOwner: p.NeedsOwner, AssessmentOf: p.assessmentOf,
		GateRun: p.gateRun, Token: w.Token, issueTake: p.issueTake}); err != nil {
		return nil, err
	}
	inside, why := envelopeFit(env, p)
	if !inside {
		// Outside the envelope the Owner decides, on a card.
		if ask == nil {
			return map[string]any{"mission_id": id, "state": "proposed", "waiting": why}, nil
		}
		q, err := askMission(ctx, tx, w, key, id, asked, why, ask)
		if err != nil {
			return nil, err
		}
		return map[string]any{"mission_id": id, "state": "awaiting_owner", "waiting": why, "question_id": q}, nil
	}
	if err := MoveMission(ctx, tx, "controller", id, "active", fmt.Sprintf("inside %s v%d", env.ID, env.Version)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE agentrt.mission SET envelope_rule_id = $2, envelope_rule_version = $3 WHERE mission_id = $1`,
		id, env.ID, env.Version); err != nil {
		return nil, err
	}
	return map[string]any{"mission_id": id, "state": "active", "envelope": map[string]any{"rule_id": env.ID, "rule_version": env.Version}}, nil
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
		case t.Subject != "":
			return invalid("task %s: only verification and review tasks have a subject, and the runtime makes those", t.Ref)
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
		if t.Core {
			if err := checkCore(t.Role, t.Repo); err != nil {
				return invalid("task %s: %v", t.Ref, err)
			}
		}
		// A branch task's result is a branch pushed from a checkout of its repo
		// at its base commit; without both agentd has nothing to check out, and
		// the executor would start in an empty directory and hand in nothing
		// (TD-421, STG M-113).
		if t.Effect == "branch" {
			if missing := missingCheckout(t.Repo, t.BaseCommit); missing != "" {
				return invalid("task %s: a branch task names its repo and base_commit (the commit agentd checks out); it has no %s", t.Ref, missing)
			}
		}
		refs[t.Ref] = true
	}
	return nil
}

// missingCheckout names what a checkout lacks: "repo", "base_commit", both, or
// nothing.
func missingCheckout(repo, base string) string {
	var m []string
	if repo == "" {
		m = append(m, "repo")
	}
	if base == "" {
		m = append(m, "base_commit")
	}
	return strings.Join(m, " and ")
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
// steps, then the tasks outside it that it depends on, each set in id order.
// Without it the batch would hold the mission's stream when its first
// dependency asks for the graph lock, the reverse of AddDependency's order.
// The tasks it depends on are taken FOR KEY SHARE, which no state change
// waits for or holds against (Conclude and Dispatch hold tasks FOR NO KEY
// UPDATE; review 176 A2); steps are taken FOR SHARE NOWAIT, so the batch
// never waits holding some of them: busy, it fails at once with
// lock_not_available, and ProposeTasks tries the whole batch again.
func lockBatch(ctx context.Context, tx pgx.Tx, tasks []TaskProposal) error {
	refs := map[string]bool{}
	var steps, deps []string
	for _, t := range tasks {
		refs[t.Ref] = true
	}
	for _, t := range tasks {
		if t.PlanNode != "" {
			steps = append(steps, t.PlanNode)
		}
		for _, d := range t.DependsOn {
			if !refs[d] {
				deps = append(deps, d)
			}
		}
	}
	if slices.ContainsFunc(tasks, func(t TaskProposal) bool { return len(t.DependsOn) > 0 }) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7421000001)`); err != nil {
			return err
		}
	}
	slices.Sort(steps)
	slices.Sort(deps)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = ANY($1) ORDER BY plan_node_id FOR SHARE NOWAIT`,
		slices.Compact(steps)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = ANY($1) ORDER BY task_id FOR KEY SHARE`, slices.Compact(deps))
	hook("batch locked", "")
	return err
}

// ErrBusy: a write found the rows it needs locked, every time it tried.
var ErrBusy = errors.New("store: busy, try again")

// busyTries is how often a batch is tried when a row it needs is locked.
const busyTries = 5

// lockNotAvailable is NOWAIT's error.
func lockNotAvailable(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "55P03"
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
	for try := 1; ; try++ {
		out, err := proposeTasks(ctx, db, w, key, missionID, tasks)
		if !lockNotAvailable(err) {
			return out, err
		}
		if try == busyTries {
			return nil, fmt.Errorf("%w: mission %s: %v", ErrBusy, missionID, err)
		}
		time.Sleep(time.Duration(try*try) * 20 * time.Millisecond)
	}
}

func proposeTasks(ctx context.Context, db DB, w Writer, key, missionID string, tasks []TaskProposal) (map[string]any, error) {
	return idempotentAs(ctx, db, w, "missions/"+missionID+"/tasks", key, batchRequest(missionID, tasks), nil,
		func(tx pgx.Tx) (map[string]any, error) {
			if err := ensureWriter(ctx, tx, w, true); err != nil {
				return nil, err
			}
			return proposeTasksTx(ctx, tx, w, missionID, tasks)
		})
}

// proposeTasksTx is ProposeTasks's write, in a transaction its caller holds
// with the writer ensured (a goal's plan task, goal.go; a plan's tasks,
// planresult.go). tasks have passed checkBatch. It sets no pin.
func proposeTasksTx(ctx context.Context, tx pgx.Tx, w Writer, missionID string, tasks []TaskProposal) (map[string]any, error) {
	return proposeBatchTx(ctx, tx, w, missionID, tasks, nil)
}

// proposeBatchTx is the write of a batch; p, when not nil, pins some of its
// tasks (pin.go), each checked before any task is written.
func proposeBatchTx(ctx context.Context, tx pgx.Tx, w Writer, missionID string, tasks []TaskProposal, p *pinning) (map[string]any, error) {
	if err := lockBatch(ctx, tx, tasks); err != nil {
		return nil, err
	}
	var state string
	var maxAction *string
	var gates bool
	err := tx.QueryRow(ctx, `SELECT state, scope_json->>'max_action', scope_json ? 'gate_run' FROM agentrt.mission WHERE mission_id = $1`,
		missionID).Scan(&state, &maxAction, &gates)
	if err != nil {
		return nil, err
	}
	if gates {
		// The controller's alone (gates.go, review td397-2 A3).
		return nil, invalid("mission %s is a batch of gates and takes no proposed tasks", missionID)
	}
	if !slices.Contains([]string{"proposed", "awaiting_owner", "active", "paused"}, state) {
		return nil, fmt.Errorf("%w: mission %s is %s and takes no new tasks", ErrState, missionID, state)
	}
	ceiling := "branch"
	if maxAction != nil {
		ceiling = *maxAction
	}
	if p != nil {
		for _, t := range tasks {
			if pin := p.pins[t.Ref]; pin != "" {
				if err := checkPin(ctx, tx, w, missionID, t, pin, p.providers[t.Ref]); err != nil {
					return nil, err
				}
			}
		}
	}
	ids := map[string]string{}
	out := make([]any, 0, len(tasks))
	for _, t := range tasks {
		if effectRank[t.Effect] > effectRank[ceiling] {
			return nil, invalid("task %s: effect_class %s is above the mission's max_action %s", t.Ref, t.Effect, ceiling)
		}
		pinned := p != nil && p.pins[t.Ref] != ""
		specID := t.SpecArtifact
		if pinned {
			// The platform's spec, never the proposer's (§2.8).
			var err error
			if specID, err = pinnedSpec(ctx, tx, t); err != nil {
				return nil, err
			}
		} else if err := checkSpecArtifact(ctx, tx, t); err != nil {
			return nil, err
		}
		id := NewTaskID()
		task := Task{ID: id, MissionID: missionID, PlanNode: t.PlanNode,
			OffPlanReason: t.OffPlanReason, Subject: t.Subject, Role: t.Role, Class: t.Class, Title: t.Title,
			SpecArtifact: specID, Effect: t.Effect, Repo: t.Repo, BaseCommit: t.BaseCommit, Core: t.Core,
			Needs: t.Needs, HardLimit: t.HardLimit, Token: w.Token}
		if pinned {
			task.pin, task.viaSession, task.providers = p.pins[t.Ref], p.viaSession, p.providers[t.Ref]
		}
		if _, err := ProposeTask(ctx, tx, w.Actor, task); err != nil {
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
}

// checkSpecArtifact checks the spec a proposed task names: stored, a
// task_spec, and referring to no task's workspace.
func checkSpecArtifact(ctx context.Context, tx pgx.Tx, t TaskProposal) error {
	var kind string
	var spec []byte
	if err := tx.QueryRow(ctx, `SELECT a.artifact_kind, b.body FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id)
	  WHERE a.artifact_id = $1`, t.SpecArtifact).Scan(&kind, &spec); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("task %s: spec %s is not stored (upload it first)", t.Ref, t.SpecArtifact)
		}
		return err
	}
	if kind != "task_spec" {
		return invalid("task %s: spec %s is a %s, not a task_spec", t.Ref, t.SpecArtifact, kind)
	}
	// A spec stored some other way than an upload is held to the
	// upload's rule too (T11).
	if ref, found := WorkspaceRef(kind, spec); found {
		return invalid("task %s: spec %s refers to a task's workspace: %s", t.Ref, t.SpecArtifact, ref)
	}
	return nil
}

// dispatchPage is how many proposed tasks Dispatch reads at a time.
