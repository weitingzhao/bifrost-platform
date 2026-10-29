package agentrtapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// SessionHeader names the calling thread, as on the questions routes.
const SessionHeader = "X-Bifrost-Session"

// writer is who a proposal is recorded as (v5 §5.1.2 actor; the design names
// seats and threads as proposers but not how a token becomes one, so the
// runtime fixes it here): the admin token is the Owner's, so it writes as
// owner; a token named seat:<x> writes as that seat (it must be registered);
// any other token writes as the thread its X-Bifrost-Session header names,
// thread:<session>, registered on its first write. The session is what the
// thread says it is: it attributes the write, it grants nothing (the role
// does).
func writer(r *http.Request) (store.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	w := store.Writer{Token: p.Name}
	var err error
	switch {
	case p.Role == actuation.RoleIntake:
		// Defence only: an intake token is in no route's ability but the
		// intake routes', which write as intakeWriter says (§5.2, R2-A1).
		return w, fmt.Errorf("%w: an intake token writes only on the intake routes", store.ErrNotYours)
	case p.Role == actuation.RoleAdmin:
		w.Actor = "owner"
	case strings.HasPrefix(p.Name, "seat:"):
		w.Actor = p.Name
	default:
		w.Actor, err = store.ThreadActor(r.Header.Get(SessionHeader))
	}
	return w, err
}

type budgetBody struct {
	MaxAttempts  int `json:"max_attempts"`
	MaxWallHours int `json:"max_wall_hours"`
}

type missionBody struct {
	IdempotencyKey string      `json:"idempotency_key"`
	WorkID         string      `json:"work_id"`
	Title          string      `json:"title"`
	Goal           string      `json:"goal"`
	Tenant         string      `json:"tenant"`
	DeadlineAt     *time.Time  `json:"deadline_at"`
	Success        []string    `json:"success"`
	Repos          []string    `json:"repos"`
	MaxAction      string      `json:"max_action"`
	Budget         *budgetBody `json:"budget"`
	NoMerge        string      `json:"no_merge"`    // why its accepted results stay off main (TD-383)
	NeedsOwner     string      `json:"needs_owner"` // why its cards wait for the Owner himself (TD-390)
}

// proposeMission is POST /missions: mission_proposed with a number the
// platform gives. Inside the standing envelope in force the mission is active
// at once; outside it, it waits as awaiting_owner on the Owner's decision
// card (the answer names the question), or stays proposed when the platform
// has no question store (the answer says why either way). A title, goal or
// success list longer than a card holds is refused (Owner 2026-10-15
// 「决策卡 A」).
func (a *API) proposeMission(w http.ResponseWriter, r *http.Request) {
	var b missionBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	p := store.MissionProposal{WorkID: b.WorkID, Title: b.Title, Goal: b.Goal, Tenant: b.Tenant, Success: b.Success,
		Repos: b.Repos, MaxAction: b.MaxAction, NoMerge: b.NoMerge, NeedsOwner: b.NeedsOwner}
	if b.DeadlineAt != nil {
		p.Deadline = *b.DeadlineAt
	}
	if b.Budget != nil {
		p.MaxAttempts, p.MaxWallHours = b.Budget.MaxAttempts, b.Budget.MaxWallHours
	}
	out, err := store.ProposeMission(r.Context(), a.DB, who, b.IdempotencyKey, p, a.Ask)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type taskBody struct {
	Ref            string   `json:"ref"`
	PlanNodeID     string   `json:"plan_node_id"`
	OffPlanReason  string   `json:"off_plan_reason"`
	SubjectTaskID  string   `json:"subject_task_id"`
	Role           string   `json:"role"`
	TaskClass      string   `json:"task_class"`
	Title          string   `json:"title"`
	SpecArtifactID string   `json:"spec_artifact_id"`
	EffectClass    string   `json:"effect_class"`
	Repo           string   `json:"repo"`
	BaseCommit     string   `json:"base_commit"`
	Core           bool     `json:"core"`
	Needs          []string `json:"needs"`
	DependsOn      []string `json:"depends_on"`
	HardLimitS     int      `json:"hard_limit_s"`
	// ExecutorPin pins the task to the external host (store/pin.go): only
	// the proposer seat a signed standing envelope names may set it.
	ExecutorPin string `json:"executor_pin"`
	// Providers declares whose work a pinned task's commit holds (§2.8):
	// only the entry's provider, alone, is taken.
	Providers []string `json:"providers"`
}

type tasksBody struct {
	IdempotencyKey string     `json:"idempotency_key"`
	Tasks          []taskBody `json:"tasks"`
}

// proposeTasks is POST /missions/{id}/tasks: a batch of task_proposed, all
// or nothing; the controller readies them (store.Dispatch). A batch that
// pins a task is store.ProposePinnedTasks's, its X-Bifrost-Session header
// recorded on each pinned task (via_session) and granting nothing.
func (a *API) proposeTasks(w http.ResponseWriter, r *http.Request) {
	var b tasksBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	tasks := make([]store.PinnedTask, 0, len(b.Tasks))
	pinned := false
	for _, t := range b.Tasks {
		// Bounded before it becomes a duration, which a large number would
		// wrap (review 175 B5).
		if t.HardLimitS < 0 || t.HardLimitS > int(store.MaxHardLimit/time.Second) {
			writeError(w, http.StatusUnprocessableEntity, "invalid",
				fmt.Sprintf("task %s: hard_limit_s is from 0 to %d", t.Ref, int(store.MaxHardLimit/time.Second)))
			return
		}
		tasks = append(tasks, store.PinnedTask{TaskProposal: store.TaskProposal{Ref: t.Ref, PlanNode: t.PlanNodeID, OffPlanReason: t.OffPlanReason,
			Subject: t.SubjectTaskID, Role: t.Role, Class: t.TaskClass, Title: t.Title, SpecArtifact: t.SpecArtifactID,
			Effect: t.EffectClass, Repo: t.Repo, BaseCommit: t.BaseCommit, Core: t.Core, Needs: t.Needs,
			DependsOn: t.DependsOn, HardLimit: time.Duration(t.HardLimitS) * time.Second}, ExecutorPin: t.ExecutorPin, Providers: t.Providers})
		pinned = pinned || t.ExecutorPin != ""
	}
	var out map[string]any
	if pinned {
		out, err = store.ProposePinnedTasks(r.Context(), a.DB, who, b.IdempotencyKey, chi.URLParam(r, "id"), r.Header.Get(SessionHeader), tasks)
	} else {
		plain := make([]store.TaskProposal, 0, len(tasks))
		for _, t := range tasks {
			plain = append(plain, t.TaskProposal)
		}
		out, err = store.ProposeTasks(r.Context(), a.DB, who, b.IdempotencyKey, chi.URLParam(r, "id"), plain)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getMission is the mission row with its tasks counted by state.
func (a *API) getMission(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(m) || jsonb_build_object('task_counts', COALESCE((
	    SELECT jsonb_object_agg(state, n) FROM (SELECT state, count(*) AS n FROM agentrt.task
	      WHERE mission_id = m.mission_id GROUP BY state) s), '{}'::jsonb))
	  FROM agentrt.mission m WHERE mission_id = $1`, chi.URLParam(r, "id"))
}

// taskStates are the states a task list may filter by.
var taskStates = []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying",
	"accepted", "rework", "rejected", "blocked", "paused", "cancelled"}

// listLimit is the most rows a list answers.
const listLimit = 500

// listTasks is GET /tasks?mission=&state=: tasks oldest first, at most
// listLimit, and whether there were more (truncated).
func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	mission, state := r.URL.Query().Get("mission"), r.URL.Query().Get("state")
	if state != "" && !slices.Contains(taskStates, state) {
		writeError(w, http.StatusBadRequest, "bad_request", "state is one of "+strings.Join(taskStates, ", "))
		return
	}
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT jsonb_build_object(
		    'tasks', COALESCE(jsonb_agg(to_jsonb(t) - 'n' ORDER BY t.created_at, t.task_id) FILTER (WHERE t.n <= $3), '[]'::jsonb),
		    'truncated', COALESCE(max(t.n) > $3, false))
		  FROM (SELECT x.*, row_number() OVER (ORDER BY x.created_at, x.task_id) AS n FROM agentrt.task x
		    WHERE ($1 = '' OR x.mission_id = $1) AND ($2 = '' OR x.state = $2) ORDER BY x.created_at, x.task_id LIMIT $3 + 1) t`,
			mission, state, listLimit).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
