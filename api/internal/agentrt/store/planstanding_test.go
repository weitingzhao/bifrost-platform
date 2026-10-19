package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan-standing rule (CUT-plan-import-2026-10-19.md §7): what it grants
// happens only while it is in force, and only as far as it grants.

// putPlanStanding puts a plan-standing rule in force, as if the Owner had
// signed it: the row's text and body_json agree.
func putPlanStanding(t *testing.T, w *world, doneRules, activeOnLease bool) {
	t.Helper()
	text := fmt.Sprintf("rule_id: plan-standing\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\n"+
		"expires_at: 2100-01-01T00:00:00Z\ndone_rules: %v\nactive_on_lease: %v\n", doneRules, activeOnLease)
	r, err := rules.Parse([]byte(text))
	ok(t, err)
	body, err := r.BodyJSON()
	ok(t, err)
	_, err = w.rt.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ('plan-standing', 1, $1, $2, 'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`,
		string(body), text)
	ok(t, err)
}

func TestPlanStandingDoneRules(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := &asker{}
	setRule := func(key, step, rule string) map[string]any {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []store.PlanChange{{Op: "set_done_rule", Node: step,
			Rule: []byte(rule), Bind: []store.Binding{{Kind: "approval", Ref: "16"}}}}, a.ask)
		ok(t, err)
		return out
	}
	// No rule in force: the Owner's card.
	if out := setRule("k0", s2, `{"approval_executed":16}`); out["decision"] != "" {
		t.Fatalf("without the rule: %v", out)
	}
	w2 := newWorld(t)
	putPlanStanding(t, w2, true, false)
	w = w2
	cards := len(a.cards)
	out := setRule("k1", s2, `{"approval_executed":16}`)
	if out["decision"] != "applied" || len(a.cards) != cards {
		t.Fatalf("a first rule on a step not done: %v", out)
	}
	var by string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided' ORDER BY event_id DESC LIMIT 1`).Scan(&by))
	if by != "standing authorization (plan-standing v1)" {
		t.Fatalf("decided by %q", by)
	}
	// Not within it: a rule on a step that has one, a manual atom, a step
	// done.
	if out := setRule("k2", s2, `{"any":[{"approval_executed":16}]}`); out["decision"] != "" {
		t.Fatalf("a second rule: %v", out)
	}
	if out := setRule("k3", s3, `{"manual":"by hand"}`); out["decision"] != "" {
		t.Fatalf("a manual rule: %v", out)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, s1, "done", "test")
		return err
	}))
	if out := setRule("k4", s1, `{"approval_executed":16}`); out["decision"] != "" {
		t.Fatalf("a rule on a done step: %v", out)
	}
}

func TestActivateLeasedSteps(t *testing.T) {
	t.Parallel()
	for _, granted := range []bool{false, true} {
		t.Run(fmt.Sprint("granted=", granted), func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			putPlanStanding(t, w, false, granted)
			t1, err := w.task(t, "M-1", s1, "", "implement")
			ok(t, err)
			_, err = w.task(t, "M-1", s3, "", "implement") // a task never leased
			ok(t, err)
			var leased int64
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				if _, _, lerr := store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, 2*time.Minute); lerr != nil {
					return lerr
				}
				return tx.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event WHERE event_type = 'attempt_leased'`).Scan(&leased)
			}))
			n, err := store.ActivateLeasedSteps(ctx, w.rt)
			ok(t, err)
			states := map[string]string{}
			for _, s := range []string{s1, s3} {
				var st string
				ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, s).Scan(&st))
				states[s] = st
			}
			if !granted {
				if n != 0 || states[s1] != "planned" {
					t.Fatalf("without the grant: %d moved, %v", n, states)
				}
				return
			}
			if n != 1 || states[s1] != "active" || states[s3] != "planned" {
				t.Fatalf("with the grant: %d moved, %v", n, states)
			}
			var cause string
			var causedBy int64
			ok(t, w.rt.QueryRow(ctx, `SELECT e.body_json->>'cause', e.cause_event_id FROM agentrt.plan_node n
			  JOIN agentrt.event e ON e.event_id = n.state_event_id WHERE n.plan_node_id = $1`, s1).Scan(&cause, &causedBy))
			if cause != "task_leased" || causedBy != leased {
				t.Fatalf("cause %s caused by %d, the lease %d", cause, causedBy, leased)
			}
			if n, err := store.ActivateLeasedSteps(ctx, w.rt); err != nil || n != 0 {
				t.Fatalf("a second pass moved %d: %v", n, err)
			}
		})
	}
}
