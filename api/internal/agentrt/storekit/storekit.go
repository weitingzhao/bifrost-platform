// Package storekit holds fixtures that tests of several packages use to put
// agentrt state in place: a mission proposed, a mission moved. It is for
// tests only. No binary calls anything here, and nothing outside a _test.go
// file may import it: the platform itself has no route that proposes a
// mission or moves one, so these writes are not part of what the store
// publishes.
//
// It is a package of its own, not part of agentrttest: agentrttest is the
// disposable PostgreSQL and Redis the tests run on and carries no business
// state machine (its row in config/domain-ownership.json says so), and these
// fixtures are one (a mission and its moves). It imports only the event log
// and the canonical encoding, not store: a foundation package may not import
// the mixed store (codeguard's foundation wall), and without that import the
// tests inside package store can use it too.
//
// Origin: 2026-11-16, a classification of the 831 tests in the shared
// packages found 249 that test one domain but borrow another's functions to
// set up state, and three production functions of store (CreateMission,
// CreatePlanNode, MoveMission) that no binary calls yet 179 live tests use
// as fixtures; by LAW-10 every test belongs to a
// domain and tests reach other domains only through what those publish.
// CreateMission and MoveMission came here on 2026-11-17 with their
// behaviour unchanged. CreatePlanNode is still in store: it calls store's
// unexported check of a plan view's binding, which reads agentrt.event for
// a key no function of eventlog asks about, and this package may not carry
// SQL on the ledger of its own.
package storekit

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Mission is the part of a mission row CreateMission writes. The success
// criteria, repos, bases and budget are optional here (an empty list or a
// zero is left out of the row's JSON); MaxAction defaults to branch and
// Tenant to dev. Bases are a goal's: the commit of each repository's main its
// tasks start from (scope_json.bases).
type Mission struct {
	ID, WorkID, Title, Goal string
	Deadline                time.Time
	Tenant, MaxAction       string
	Success, Repos          []string
	Bases                   map[string]string
	MaxAttempts             int
	MaxWallHours            int
	NoMerge                 string         // why its accepted results stay off main, "" for none (scope_json.no_merge, TD-383)
	NeedsOwner              string         // why its cards wait for the Owner himself, "" for none (scope_json.needs_owner, TD-390)
	GateRun                 map[string]any // a batch of gate tasks, the controller's own (scope_json.gate_run, gates.go, TD-397)
	AssessmentOf            string         // the stream of the item it assesses, "" for none (scope_json.assessment_of, TD-391)
	Token                   string         // the token that proposed it, when a route did (review 176 B2)
}

// CreateMission writes mission_proposed and the mission row.
func CreateMission(ctx context.Context, tx pgx.Tx, actor string, m Mission) (eventlog.Appended, error) {
	if m.Tenant == "" {
		m.Tenant = "dev"
	}
	if m.MaxAction == "" {
		m.MaxAction = "branch"
	}
	success := map[string]any{}
	if len(m.Success) > 0 {
		success["criteria"] = m.Success
	}
	scope := map[string]any{"max_action": m.MaxAction}
	if len(m.Repos) > 0 {
		scope["repos"] = m.Repos
	}
	if len(m.Bases) > 0 {
		bases := make(map[string]any, len(m.Bases))
		for k, v := range m.Bases {
			bases[k] = v
		}
		scope["bases"] = bases
	}
	if m.NoMerge != "" {
		scope["no_merge"] = m.NoMerge
	}
	if m.NeedsOwner != "" {
		scope["needs_owner"] = m.NeedsOwner
	}
	if m.AssessmentOf != "" {
		scope["assessment_of"] = m.AssessmentOf
	}
	if m.GateRun != nil {
		scope["gate_run"] = m.GateRun
	}
	budget := map[string]any{}
	if m.MaxAttempts > 0 {
		budget["max_attempts"] = m.MaxAttempts
	}
	if m.MaxWallHours > 0 {
		budget["max_wall_hours"] = m.MaxWallHours
	}
	var docs [3][]byte
	for i, v := range []any{success, scope, budget} {
		var err error
		if docs[i], err = canon.Encode(v); err != nil {
			return eventlog.Appended{}, err
		}
	}
	body := map[string]any{"title": m.Title}
	if m.Token != "" {
		body["token"] = m.Token
	}
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: missionStream(m.ID), Type: "mission_proposed", Actor: actor,
		MissionID: m.ID, Body: body})
	if err != nil {
		return ev, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.mission (mission_id, work_id, title, goal, success_json, scope_json,
	  tenant, state, deadline_at, budget_json, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,'proposed',$8,$9::jsonb,$10)`,
		m.ID, eventlog.Nullable(m.WorkID), m.Title, m.Goal, string(docs[0]), string(docs[1]), m.Tenant, m.Deadline, string(docs[2]), actor)
	return ev, err
}

// missionStream names the event stream of a mission.
func missionStream(missionID string) string { return "mission:" + missionID }

// missionMoves is every mission state change there is: the table storekit
// moves missions by, and the plan's event vocabulary checks a mission's states
// against (planstore/vocab.go, through MissionMoves). One copy since
// 2026-11-17.
var missionMoves = map[string][]string{
	"proposed":       {"awaiting_owner", "active", "stopped"},
	"awaiting_owner": {"active", "stopped"},
	"active":         {"paused", "done", "stopped", "expired"},
	"paused":         {"active", "stopped", "expired"},
}

// MissionMoves is a copy of the table this package moves missions by, for
// the plan's event vocabulary.
func MissionMoves() map[string][]string {
	out := make(map[string][]string, len(missionMoves))
	for from, to := range missionMoves {
		out[from] = slices.Clone(to)
	}
	return out
}

// MoveMission changes a mission's state with mission_state_changed. It holds
// the mission row exclusively, so a claim of one of its tasks either commits
// first or sees the new state.
func MoveMission(ctx context.Context, tx pgx.Tx, actor, missionID, to, reason string) error {
	var from string
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, missionID).Scan(&from); err != nil {
		return err
	}
	if !slices.Contains(missionMoves[from], to) {
		return fmt.Errorf("%w: mission %s %s -> %s", eventlog.ErrState, missionID, from, to)
	}
	body := map[string]any{"from": from, "to": to}
	if reason != "" {
		body["reason"] = reason
	}
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: missionStream(missionID), Type: "mission_state_changed", Actor: actor,
		MissionID: missionID, Body: body}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state = $2, version = version + 1 WHERE mission_id = $1`, missionID, to)
	return err
}
