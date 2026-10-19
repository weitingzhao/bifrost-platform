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
// decision card no decision is recorded for, and missions outside the
// standing envelope waiting on their card. Whether the card is still open
// is the question store's to say, which this read does not reach: an item is
// a candidate, and the Console keeps it only while that question is open.
// Waits he cannot act on are the missions' statuses.
func needsYou(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT p.event_id, substr(p.event_stream_id, 6), p.body_json->>'reason', p.at,
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
		event                  int64
		root, reason, question string
		at                     time.Time
		changes                []PlanChange
	}
	var list []pending
	for rows.Next() {
		var p pending
		var doc string
		if err = rows.Scan(&p.event, &p.root, &p.reason, &p.at, &p.question, &doc); err != nil {
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
		blocks, berr := changeBlocks(ctx, tx, p.changes)
		if berr != nil {
			return nil, berr
		}
		it := map[string]any{"id": "question:" + p.question, "kind": "plan_change", "ref": fmt.Sprint(p.event), "also_refs": []string{},
			"title": "Plan change to " + p.root + ": " + firstLine(p.reason), "since": p.at.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(p.at) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": p.question, "options": []string{CardApply, CardRefuse}},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": blocks,
			"after": "Apply: the controller lands the change on its next pass (about 15 seconds). Do not apply: the plan stays as it is."}
		byQuestion[p.question] = it
		entries = append(entries, entry{p.at, p.question, it})
	}
	cards, more, err := missionCards(ctx, tx, now, byQuestion)
	if err != nil {
		return nil, err
	}
	for _, c := range cards {
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
