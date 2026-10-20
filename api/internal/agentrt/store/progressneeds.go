package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The progress projection's needs_you (progress.go has the rest): the plan
// changes waiting on a decision card, and what each holds up.

// needsYou lists what the Owner may act on (SPEC §2.1): plan changes on a
// decision card no decision is recorded for, missions outside the standing
// envelope waiting on their card, and blocked tasks on their open card
// (blockedcard.go). Whether the card is still open
// is the question store's to say, which this read does not reach: an item is
// a candidate, and the Console keeps it only while that question is open.
// Waits he cannot act on are the missions' statuses.
func needsYou(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT p.event_id, p.event_stream_id, p.body_json->>'reason', p.at,
	    p.body_json->>'question_id', (p.body_json->'changes')::text
	  FROM agentrt.event p
	  WHERE p.event_type = 'plan_change_proposed' AND p.at > $1::timestamptz - make_interval(secs => $2) AND p.body_json ? 'question_id'
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.at >= p.at
	      AND d.event_stream_id = p.event_stream_id AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.at, p.event_id LIMIT $3`, now, pendingChangeWindow.Seconds(), pendingChangeLimit+1)
	if err != nil {
		return nil, err
	}
	type pending struct {
		event                    int64
		stream, reason, question string
		at                       time.Time
		changes                  []PlanChange
	}
	var list []pending
	for rows.Next() {
		var p pending
		var doc string
		if err = rows.Scan(&p.event, &p.stream, &p.reason, &p.at, &p.question, &doc); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(doc), &p.changes); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	truncated := len(list) > pendingChangeLimit
	if truncated {
		list = list[:pendingChangeLimit]
	}
	// One card is one item: a second proposal naming a card already listed
	// adds its event to that item. Plan changes and missions share one
	// bound, oldest first (review 190 B4).
	type entry struct {
		at time.Time
		id string
		it map[string]any
	}
	var entries []entry
	byQuestion := map[string]map[string]any{}
	for _, p := range list {
		if it, seen := byQuestion[p.question]; seen {
			it["also_refs"] = append(it["also_refs"].([]string), fmt.Sprint(p.event))
			continue
		}
		root := streamRoot(p.stream)
		title, options := "Plan change to "+root+": "+firstLine(p.reason), []string{CardApply, CardRefuse}
		after := "Apply: the controller lands the change on its next pass (about 15 seconds). Do not apply: the plan stays as it is."
		var blocks any
		if _, isRecorder := recorderImportOf(p.changes); isRecorder {
			// A recorder log is history: no plan or mission waits on it.
			work := strings.TrimPrefix(p.stream, RecorderStream(""))
			title, options = "Import recorder log "+work+": "+firstLine(p.reason), []string{RecorderApply, RecorderRefuse}
			after = "Import: the controller imports the log on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "a recorder log is history; no plan or mission waits on it"}
		} else if _, isImport := importOf(p.changes); isImport {
			// An import adds a plan no mission's work sits on yet.
			title, options = "Import plan "+root+": "+firstLine(p.reason), []string{ImportApply, ImportRefuse}
			after = "Import: the controller imports the plan on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "an import adds a plan; no mission's work sits on it yet"}
		} else {
			var berr error
			if blocks, berr = changeBlocks(ctx, tx, p.changes); berr != nil {
				return nil, berr
			}
		}
		it := map[string]any{"id": "question:" + p.question, "kind": "plan_change", "ref": fmt.Sprint(p.event), "also_refs": []string{},
			"title": title, "since": p.at.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(p.at) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": p.question, "options": options},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": blocks, "after": after}
		byQuestion[p.question] = it
		entries = append(entries, entry{p.at, p.question, it})
	}
	cards, more, err := missionCards(ctx, tx, now, byQuestion)
	if err != nil {
		return nil, err
	}
	blocked, moreBlocked, err := blockedTaskCards(ctx, tx, now, byQuestion)
	if err != nil {
		return nil, err
	}
	more = more || moreBlocked
	for _, c := range append(cards, blocked...) {
		entries = append(entries, entry{c.at, c.question, c.it})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	truncated = truncated || more
	if len(entries) > pendingChangeLimit {
		entries, truncated = entries[:pendingChangeLimit], true
	}
	items := make([]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.it)
	}
	return map[string]any{"count": len(items), "items": items, "truncated": truncated}, nil
}

type missionCard struct {
	at       time.Time
	question string
	it       map[string]any
}

// missionCards are the missions outside the standing envelope waiting on
// their card, oldest first, at most the bound (more says there were more),
// leaving out a card already listed.
func missionCards(ctx context.Context, tx pgx.Tx, now time.Time, listed map[string]map[string]any) ([]missionCard, bool, error) {
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.title, s.at, s.body_json->>'question_id', COALESCE(s.body_json->>'reason', ''),
	    (SELECT count(*) FROM agentrt.task t WHERE t.mission_id = m.mission_id AND t.state NOT IN ('accepted','rejected','cancelled'))
	  FROM agentrt.mission m
	  JOIN LATERAL (SELECT e.at, e.body_json FROM agentrt.event e WHERE e.mission_id = m.mission_id
	    AND e.event_type = 'mission_state_changed' ORDER BY e.event_id DESC LIMIT 1) s ON true
	  WHERE m.state = 'awaiting_owner' AND s.body_json ? 'question_id' ORDER BY s.at, m.mission_id LIMIT $1`, pendingChangeLimit+1)
	if err != nil {
		return nil, false, err
	}
	type row struct {
		mission, title, question, why string
		at                            time.Time
		open                          int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var c row
		return c, r.Scan(&c.mission, &c.title, &c.at, &c.question, &c.why, &c.open)
	})
	if err != nil {
		return nil, false, err
	}
	more := len(list) > pendingChangeLimit
	if more {
		list = list[:pendingChangeLimit]
	}
	var cards []missionCard
	for _, c := range list {
		if _, seen := listed[c.question]; seen {
			continue
		}
		it := map[string]any{"id": "question:" + c.question, "kind": "mission", "ref": c.mission, "also_refs": []string{},
			"title": "Mission " + c.mission + ": " + firstLine(c.title), "since": c.at.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(c.at) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": c.question, "options": []string{MissionStart, MissionNotStart}},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": map[string]any{"missions": []string{c.mission}, "unfinished_tasks": c.open, "why": c.why},
			"after": "Start: the controller makes it active on its next pass (about 15 seconds). Do not start: it stops."}
		listed[c.question] = it
		cards = append(cards, missionCard{c.at, c.question, it})
	}
	return cards, more, nil
}

// blockedTaskCards are the blocked tasks whose card is open, oldest card
// first, at most the bound (more says there were more), leaving out a card
// already listed. Each names its task and mission, so the Console's goal card
// shows it on that goal.
func blockedTaskCards(ctx context.Context, tx pgx.Tx, now time.Time, listed map[string]map[string]any) ([]missionCard, bool, error) {
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.mission_id, t.title, o.body_json->>'question_id', o.at,
	    COALESCE(b.body_json->>'reason', ''),
	    (SELECT count(*) FROM agentrt.task u WHERE u.mission_id = t.mission_id AND u.state NOT IN ('accepted','rejected','cancelled'))
	  FROM agentrt.event o JOIN agentrt.task t ON t.task_id = o.task_id
	  LEFT JOIN agentrt.event b ON b.event_id = (o.body_json->>'blocked_event_id')::bigint
	  WHERE o.event_type = 'owner_wait_opened' AND t.state = 'blocked' AND `+cardOpen+`
	  ORDER BY o.at, o.event_id LIMIT $1`, pendingChangeLimit+1)
	if err != nil {
		return nil, false, err
	}
	type row struct {
		task, mission, title, question, why string
		at                                  time.Time
		open                                int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var c row
		return c, r.Scan(&c.task, &c.mission, &c.title, &c.question, &c.at, &c.why, &c.open)
	})
	if err != nil {
		return nil, false, err
	}
	more := len(list) > pendingChangeLimit
	if more {
		list = list[:pendingChangeLimit]
	}
	var cards []missionCard
	for _, c := range list {
		if _, seen := listed[c.question]; seen {
			continue
		}
		it := map[string]any{"id": "question:" + c.question, "kind": "blocked_task", "ref": c.task, "also_refs": []string{},
			"task_id": c.task, "mission_id": c.mission,
			"title": "Blocked in " + c.mission + ": " + firstLine(c.title), "since": c.at.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(c.at) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": c.question, "options": []string{BlockedRetry, BlockedDrop}},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": map[string]any{"missions": []string{c.mission}, "unfinished_tasks": c.open, "why": c.why},
			"after": "One more round: the controller readies it on its next pass (about 15 seconds), with room in its mission's " +
				"budget for the round. Do not do it: it is cancelled, and its mission finishes once every other task is accepted; " +
				"work left with no check of its latest result is blocked and gets a card of its own."}
		listed[c.question] = it
		cards = append(cards, missionCard{c.at, c.question, it})
	}
	return cards, more, nil
}

// changeBlocks is what a waiting plan change holds up (SPEC §2.1), from one
// set: the steps it touches (a node that is a step, or the steps under a
// node that is an item: an added node's parent, a retitled or re-ruled node,
// a rebound step, a moved task's target) and the tasks it moves. Its
// missions are those of these steps (a task on a step is of the step's
// mission), of the moved tasks (where they come from) and the missions a
// change binds to; its unfinished tasks are those on these steps and the
// moved ones. Unknown when it touches no mission.
func changeBlocks(ctx context.Context, tx pgx.Tx, changes []PlanChange) (any, error) {
	var nodes, tasks, bound []string
	for _, c := range changes {
		switch c.Op {
		case "add_node":
			nodes = append(nodes, c.Parent)
		case "move_task":
			nodes = append(nodes, c.Node)
			tasks = append(tasks, c.Task)
		default:
			nodes = append(nodes, c.Node)
		}
		// Only these two bind a mission (planchange.go applyChanges); a
		// mission_id on another op changes nothing.
		if c.MissionID != "" && (c.Op == "rebind_step" || c.Op == "add_node") {
			bound = append(bound, c.MissionID)
		}
	}
	var missions []string
	var open int
	err := tx.QueryRow(ctx, `WITH steps AS (SELECT plan_node_id, mission_id FROM agentrt.plan_node WHERE node_kind = 'step'
	      AND (plan_node_id = ANY($1) OR parent_plan_node_id = ANY($1))),
	    held AS (SELECT task_id FROM agentrt.task WHERE state NOT IN ('accepted','rejected','cancelled')
	      AND (plan_node_id IN (SELECT plan_node_id FROM steps) OR task_id = ANY($2)))
	  SELECT ARRAY(SELECT DISTINCT m FROM (SELECT mission_id FROM steps
	      UNION ALL SELECT mission_id FROM agentrt.task WHERE task_id = ANY($2) UNION ALL SELECT unnest($3::text[])) x(m)
	      WHERE m IS NOT NULL ORDER BY 1),
	    (SELECT count(*) FROM held)`, nodes, tasks, bound).Scan(&missions, &open)
	if err != nil {
		return nil, err
	}
	if len(missions) == 0 {
		return map[string]any{"unknown": "the change touches no step a mission's work sits on"}, nil
	}
	return map[string]any{"missions": missions, "unfinished_tasks": open}, nil
}
