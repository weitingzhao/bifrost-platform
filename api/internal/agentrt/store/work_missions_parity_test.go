package store

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"reflect"
	"testing"
	"time"
)

func frozenMissionsInWork(ctx context.Context, tx pgx.Tx, now time.Time, live map[string]*ProgressAttempt, usual map[string]usualTime) (map[string]any, map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.title, m.goal, m.state, m.deadline_at, COALESCE(m.work_id, ''),
	    s.at, COALESCE(s.body_json->>'reason', ''), COALESCE(s.actor_id, ''), COALESCE(s.body_json->>'question_id', ''), l.at
	  FROM agentrt.mission m
	  LEFT JOIN LATERAL (SELECT e.at, e.body_json, e.actor_id FROM agentrt.event e WHERE e.mission_id = m.mission_id
	    AND e.event_type IN ('mission_proposed','mission_state_changed') ORDER BY e.event_id DESC LIMIT 1) s ON true
	  LEFT JOIN LATERAL (SELECT e.at FROM agentrt.event e WHERE e.mission_id = m.mission_id ORDER BY e.event_id DESC LIMIT 1) l ON true
	  WHERE m.state IN ('proposed','awaiting_owner','active','paused') ORDER BY substr(m.mission_id, 3)::int LIMIT $1`, progressMissions+1)
	if err != nil {
		return nil, nil, err
	}
	var missions []*progressMission
	byID := map[string]*progressMission{}
	for rows.Next() {
		m := &progressMission{}
		var deadline time.Time
		var stateAt, lastAt *time.Time
		if err = rows.Scan(&m.id, &m.title, &m.goal, &m.state, &deadline, &m.work, &stateAt, &m.stateReason, &m.stateBy,
			&m.stateQuestion, &lastAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		m.deadline = deadline.UTC().Format(time.RFC3339)
		m.stateAt, m.lastAt = stamp(stateAt), stamp(lastAt)
		missions = append(missions, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	truncated := len(missions) > progressMissions
	if truncated {
		missions = missions[:progressMissions]
	}
	ids := make([]string, 0, len(missions))
	for _, m := range missions {
		byID[m.id] = m
		ids = append(ids, m.id)
	}
	// The slices: the steps of each mission, by the counting rule, and the
	// next steps not started, in plan order.
	srows, err := tx.Query(ctx, `SELECT s.mission_id, s.plan_node_id, s.title, s.state, COALESCE(e.body_json->>'cause', ''), s.owner_step
	  FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
	  LEFT JOIN agentrt.event e ON e.event_id = s.state_event_id
	  WHERE s.node_kind = 'step' AND s.mission_id = ANY($1)
	  ORDER BY i.ordinal, s.ordinal, s.plan_node_id`, ids)
	if err != nil {
		return nil, nil, err
	}
	for srows.Next() {
		var mission, id, title, state, cause string
		var owner bool
		if err = srows.Scan(&mission, &id, &title, &state, &cause, &owner); err != nil {
			srows.Close()
			return nil, nil, err
		}
		m := byID[mission]
		m.steps = append(m.steps, plan.Step{ID: id, State: state, Manual: state == plan.StateDone && cause == "done_rule_manual"})
		if state == plan.StatePlanned && len(m.next) < 2 {
			m.next = append(m.next, map[string]any{"plan_node_id": id, "title": title, "owner_step": owner})
		}
	}
	srows.Close()
	if err = srows.Err(); err != nil {
		return nil, nil, err
	}
	// A mission's tasks are not read any more: it has none in work.
	list := make([]any, 0, len(missions))
	etas := make(map[string]map[string]any, len(missions))
	for _, m := range missions {
		etas[m.id] = missionETA(m, usual, now)
		list = append(list, missionDoc(m, etas[m.id]))
	}
	return map[string]any{"missions": list, "truncated": truncated}, ownerNext(missions, etas, truncated, now), nil
}

func TestWorkMissionsFrozenQueryPG(t *testing.T) {
	t.Parallel()
	db := agentrttest.New(t)
	pool := db.Pool(t, db.Admin)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := pool.Exec(ctx, `CREATE SCHEMA agentrt;
 CREATE TABLE agentrt.mission(mission_id text,title text,goal text,state text,deadline_at timestamptz,work_id text);
 CREATE TABLE agentrt.event(event_id bigint,mission_id text,at timestamptz,body_json jsonb,actor_id text,event_type text);
 CREATE TABLE agentrt.plan_node(mission_id text,plan_node_id text,parent_plan_node_id text,title text,state text,state_event_id bigint,node_kind text,owner_step bool,ordinal int)`)
	must(err)
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for _, count := range []int{0, 1, 5, 101} {
		_, err = pool.Exec(ctx, `TRUNCATE agentrt.mission,agentrt.event,agentrt.plan_node`)
		must(err)
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("M-%d", i+1)
			_, err = pool.Exec(ctx, `INSERT INTO agentrt.mission VALUES($1,'Title','goal',$2,$3,NULL)`, id, []string{"proposed", "awaiting_owner", "active", "paused"}[i%4], now)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO agentrt.event VALUES($1,$2,$3,'{"reason":"why","question_id":"q","cause":"done_rule_manual"}','actor','mission_proposed')`, int64(i+1), id, now.Add(-time.Minute))
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO agentrt.plan_node VALUES($1,$2,NULL,'item','',NULL,'item',false,$3)`, id, id+"-item", i)
			must(err)
			for j := 0; j < 4; j++ {
				_, err = pool.Exec(ctx, `INSERT INTO agentrt.plan_node VALUES($1,$2,$3,'step',$4,$5,'step',$6,$7)`, id, fmt.Sprintf("%s-%d", id, j), id+"-item", []string{"planned", "done", "active", "planned"}[j], int64(i+1), j%2 == 0, j)
				must(err)
			}
		}
		tx, err := pool.Begin(ctx)
		must(err)
		want, wn, we := frozenMissionsInWork(ctx, tx, now, nil, map[string]usualTime{})
		got, gn, ge := missionsInWork(ctx, tx, now, nil, map[string]usualTime{})
		if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(wn, gn) || fmt.Sprint(we) != fmt.Sprint(ge) {
			t.Fatalf("count %d: old %#v %#v %v != new %#v %#v %v", count, want, wn, we, got, gn, ge)
		}
		must(tx.Rollback(ctx))
	}
}
