package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

var stepT0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func stepAt(s int) time.Time { return stepT0.Add(time.Duration(s) * time.Second) }

func stepEv(id int64, typ string, s int, body map[string]any) StepTimelineEvent {
	return StepTimelineEvent{EventID: id, Type: typ, At: stepAt(s), PlanNode: "p_step", TaskID: "t_1", AttemptID: "a_1", Actor: "controller", Body: body}
}

func stepLand(id int64, s int, r landing.Record) StepLandingEntry {
	return StepLandingEntry{EventID: id, Entry: landing.Entry{At: stepAt(s), Record: r}}
}

// stepTimelineRows is each event as kind/source/waited_on/wait_s.
func stepTimelineRows(es []StepEvent) []string {
	out := []string{}
	for _, e := range es {
		b, _ := json.Marshal([]any{e.Kind, e.Source, e.WaitedOn, e.WaitS})
		out = append(out, string(b))
	}
	return out
}

func stepWant(t *testing.T, got []StepEvent, want [][]any) {
	t.Helper()
	g := stepTimelineRows(got)
	w := []string{}
	for _, x := range want {
		b, _ := json.Marshal(x)
		w = append(w, string(b))
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("timeline\n got %s\nwant %s", gb, wb)
	}
}

// The full path: created to PROD, ordered by time then event id, each with
// its wait and what it waited on.
func TestFoldStepTimelineOrderWaitAndStages(t *testing.T) {
	t.Parallel()
	in := StepTimelineInput{
		Step: "p_step",
		Plan: []StepTimelineEvent{
			stepEv(1, "plan_node_created", 0, map[string]any{"node_kind": "step"}),
			stepEv(9, "plan_node_state_changed", 700, map[string]any{"from": "active", "to": "done", "cause": "done_rule"}),
			stepEv(2, "plan_node_state_changed", 10, map[string]any{"from": "planned", "to": "active", "cause": "mission_active"}),
		},
		Tasks: []StepTimelineEvent{
			stepEv(4, "attempt_submitted", 100, map[string]any{}),
			stepEv(3, "attempt_leased", 40, map[string]any{"executor": "x_exec", "host_id": "h_1"}),
			stepEv(5, "task_state_changed", 160, map[string]any{"from": "review", "to": "accepted", "reason": "checks green"}),
			stepEv(6, "task_state_changed", 160, map[string]any{"from": "submitted", "to": "review"}),
		},
		Landing: []StepLandingEntry{
			stepLand(10, 150, landing.Record{"kind": "claimed", "op": "o1", "task": "t_1", "attempt": "a_1"}),
			stepLand(11, 300, landing.Record{"kind": "landed", "op": "o1", "R": "rrr"}),
			stepLand(12, 310, landing.Record{"kind": "stg_pending", "op": "s1", "R": []any{"rrr"}}),
			stepLand(13, 400, landing.Record{"kind": "stg_verified", "op": "s1", "P": "ppp"}),
		},
		Deploys: []StepTimelineEvent{
			stepEv(20, "deploy_seen", 600, map[string]any{"env": "stg", "commit": "ppp"}),
			stepEv(21, "deploy_seen", 650, map[string]any{"env": "prod", "commit": "ppp"}),
		},
	}
	got := FoldStepTimeline(in)
	stepWant(t, got, [][]any{
		{"created", "agentrt.event", "", 0},
		{"active", "agentrt.event", "Owner", 10},
		{"task_leased", "agentrt.event", "executor lease", 30},
		{"submitted", "agentrt.event", "executor", 60},
		{"accepted", "agentrt.event", "review", 60},
		{"landed", "note.landing", "landing CI", 140},
		{"on_stg", "note.landing", "STG", 100},
		{"on_prod", "deploy_seen", "PROD release", 250},
		{"done_by_rule", "agentrt.event", "", 50},
	})
	if got[2].Actor != "x_exec" || got[4].Reason != "checks green" || got[5].Reason != "rrr" || got[5].TaskID != "t_1" ||
		got[6].Reason != "ppp" || got[8].Reason != "done_rule" || got[0].At != "2026-11-06T09:00:00.000000Z" {
		t.Fatalf("fields: %+v", got)
	}
}

// Events at the same time keep event id order, whatever the input order.
func TestFoldStepTimelineTieBreaksByEventID(t *testing.T) {
	t.Parallel()
	got := FoldStepTimeline(StepTimelineInput{Step: "p_step",
		Plan: []StepTimelineEvent{
			stepEv(8, "plan_node_state_changed", 5, map[string]any{"to": "active", "cause": "x"}),
			stepEv(7, "plan_node_created", 5, map[string]any{}),
		}})
	if len(got) != 2 || got[0].EventID != 7 || got[1].EventID != 8 || got[1].WaitS != 0 || got[1].WaitedOn != "Owner" {
		t.Fatalf("got %+v", got)
	}
}

// Only the rules pass makes done_by_rule; a done by any other cause is no stage.
func TestFoldStepTimelineDoneByRuleOnlyForTheRulesCause(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cause string
		want  int
	}{{"done_rule", 1}, {"done_rule_manual", 1}, {"owner", 0}, {"", 0}}
	for _, c := range cases {
		got := FoldStepTimeline(StepTimelineInput{Step: "p_step", Plan: []StepTimelineEvent{
			stepEv(1, "plan_node_state_changed", 1, map[string]any{"from": "active", "to": "done", "cause": c.cause})}})
		if len(got) != c.want || c.want == 1 && (got[0].Kind != "done_by_rule" || got[0].Reason != c.cause) {
			t.Fatalf("cause %q: %+v", c.cause, got)
		}
	}
}

// External work records and attempts on the external host are labelled as such.
func TestFoldStepTimelineExternalAndCoordinatorWork(t *testing.T) {
	t.Parallel()
	note := func(state string) externalNote {
		return externalNote{ID: "x_1", Actor: "thread:collector@h_ext", Node: "p_step", Task: "t_1", State: state}
	}
	got := FoldStepTimeline(StepTimelineInput{Step: "p_step",
		ExternalHosts: []string{"h_ext"},
		Tasks: []StepTimelineEvent{
			stepEv(1, "attempt_leased", 0, map[string]any{"executor": "x_coord", "host_id": "h_ext"}),
			stepEv(2, "attempt_submitted", 20, map[string]any{}),
		},
		External: []StepExternalRecord{
			{EventID: 4, At: stepAt(10), Note: note("Running")},
			{EventID: 3, At: stepAt(5), Note: note("Registered")},
			{EventID: 5, At: stepAt(12), Note: note("Running")},
			{EventID: 6, At: stepAt(30), Note: note("Succeeded")},
		},
	})
	stepWant(t, got, [][]any{
		{"coordinator_work", "coordinator_work", "", 0},
		{"external_work", "external_work", "executor", 5},
		{"external_work", "external_work", "executor", 5},
		{"coordinator_work", "coordinator_work", "executor", 10},
		{"external_work", "external_work", "executor", 10},
	})
	if got[0].Reason != "leased" || got[0].Actor != "x_coord" || got[1].Reason != "Registered" || got[4].Reason != "Succeeded" || got[4].Actor != "thread:collector@h_ext" {
		t.Fatalf("fields: %+v", got)
	}
}

// No on_prod without a deploy_seen in prod of a P carrying the step's R (or
// verified after it); a later verified P counts.
func TestFoldStepTimelineOnProdNeedsAMatchingDeploy(t *testing.T) {
	t.Parallel()
	base := []StepLandingEntry{
		stepLand(1, 0, landing.Record{"kind": "landed", "op": "o1", "R": "rrr"}),
		stepLand(2, 10, landing.Record{"kind": "stg_pending", "op": "s0", "R": []any{"other"}}),
		stepLand(3, 20, landing.Record{"kind": "stg_verified", "op": "s0", "P": "p_old"}),
		stepLand(4, 30, landing.Record{"kind": "stg_pending", "op": "s1", "R": []any{"rrr"}}),
		stepLand(5, 40, landing.Record{"kind": "stg_verified", "op": "s1", "P": "p_mine"}),
		stepLand(6, 50, landing.Record{"kind": "stg_pending", "op": "s2", "R": []any{"later"}}),
		stepLand(7, 60, landing.Record{"kind": "stg_verified", "op": "s2", "P": "p_later"}),
	}
	cases := []struct {
		name    string
		landing []StepLandingEntry
		deploy  map[string]any
		want    bool
	}{
		{"carrying P", base, map[string]any{"env": "prod", "commit": "p_mine"}, true},
		{"later P", base, map[string]any{"env": "prod", "commit": "p_later"}, true},
		{"earlier P", base, map[string]any{"env": "prod", "commit": "p_old"}, false},
		{"stg env", base, map[string]any{"env": "stg", "commit": "p_mine"}, false},
		{"unknown commit", base, map[string]any{"env": "prod", "commit": "zzz"}, false},
		{"never on STG", base[:1], map[string]any{"env": "prod", "commit": "p_mine"}, false},
	}
	for _, c := range cases {
		got := FoldStepTimeline(StepTimelineInput{Step: "p_step", Landing: c.landing,
			Deploys: []StepTimelineEvent{stepEv(9, "deploy_seen", 100, c.deploy)}})
		has := false
		for _, e := range got {
			has = has || e.Kind == "on_prod"
		}
		if has != c.want {
			t.Fatalf("%s: on_prod %v, want %v: %+v", c.name, has, c.want, got)
		}
	}
	// Without any deploy there is no on_prod, and nothing else is invented.
	got := FoldStepTimeline(StepTimelineInput{Step: "p_step", Landing: base})
	stepWant(t, got, [][]any{
		{"landed", "note.landing", "", 0},
		{"on_stg", "note.landing", "STG", 40},
	})
}

// No record gives an empty list, [] in JSON.
func TestFoldStepTimelineEmpty(t *testing.T) {
	t.Parallel()
	got := FoldStepTimeline(StepTimelineInput{Step: "p_step"})
	b, err := json.Marshal(got)
	if err != nil || string(b) != "[]" {
		t.Fatalf("got %s %v", b, err)
	}
}
