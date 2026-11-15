package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Proposals (v5 §5.2 「路由草图」): the missions and tasks that threads and
// seats propose, the actor a thread writes as, and the dispatch pass that
// makes a proposed task ready once it may run.

// ErrInvalid: the request breaks a rule of its own (a field missing or out
// of range, a writer that may not write); nothing was written.
var ErrInvalid = eventlog.ErrInvalid

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// The writer of a request, its checks and a thread's registration are the
// actor package's (the base every domain's write routes call); these are its
// names as this package has always written them.
type Writer = actor.Writer

var ErrNotYours = actor.ErrNotYours

const ThreadsPerDay = actor.ThreadsPerDay

func ThreadActor(session string) (string, error) { return actor.ThreadActor(session) }

func holdsToken(ctx context.Context, tx pgx.Tx, w Writer) error { return actor.HoldsToken(ctx, tx, w) }

func holdsSessionTx(ctx context.Context, tx pgx.Tx, w Writer) error {
	return actor.HoldsSessionTx(ctx, tx, w)
}

func kindOf(a string) string { return actor.KindOf(a) }

func CheckWriter(ctx context.Context, tx pgx.Tx, w Writer) (bool, error) {
	return actor.CheckWriter(ctx, tx, w)
}

func writerLock(ctx context.Context, tx pgx.Tx, a string) error { return actor.Lock(ctx, tx, a) }

func ensureWriter(ctx context.Context, tx pgx.Tx, w Writer, bounded bool) error {
	return actor.Ensure(ctx, tx, w, bounded)
}

func ActorKind(ctx context.Context, tx pgx.Tx, a string) (string, error) {
	return actor.ActorKind(ctx, tx, a)
}

// What only this package knows, registered with the actor package: the
// binding of an external agent's token (intake.go) and the day's limit of
// new threads where the tuning rule sets it.
func init() {
	// holdsWriter (intake.go) is the hold of an agent's token to its intake
	// client; the actor package asks it for agents only.
	actor.RegisterAgentHolder(holdsWriter)
	actor.RegisterThreadLimit(func(ctx context.Context, tx pgx.Tx) (int, error) {
		tune, err := tuningIn(ctx, tx)
		if err != nil {
			return 0, err
		}
		return tune.Int(rules.TuneThreadsPerDay), nil
	})
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
	Wave                             string
	Urgent                           bool
	Ref                              string
	PlanNode, OffPlanReason, Subject string
	Role, Class, Title, SpecArtifact string
	Effect, Repo, BaseCommit         string
	Core                             bool
	Needs, DependsOn                 []string
	HardLimit                        time.Duration
}

// effectRank orders effect classes; a task's effect is at most its
// mission's max_action.
var effectRank = map[string]int{"none": 0, "branch": 1, "external": 2}

// MaxBatch is the most tasks one withdrawal or one superseding names. A
// proposal has no such bound since cleanup wave 1 (2026-11-11).
const MaxBatch = 50

// MaxHardLimit is the longest an attempt may be given (zero means the
// default of an hour).
const MaxHardLimit = 24 * time.Hour

// ErrBusy: a write found the rows it needs locked, every time it tried.
var ErrBusy = errors.New("store: busy, try again")

// busyTries is how often a batch is tried when a row it needs is locked.
const busyTries = 5

// lockNotAvailable is NOWAIT's error.
func lockNotAvailable(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "55P03"
}

// dispatchPage is how many proposed tasks Dispatch reads at a time.
