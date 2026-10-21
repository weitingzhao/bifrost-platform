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
	a := &asker{}
	setRule := func(w *world, key, step, rule string, bind ...store.Binding) map[string]any {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []store.PlanChange{{Op: "set_done_rule", Node: step,
			Rule: []byte(rule), Bind: bind}}, a.ask)
		ok(t, err)
		return out
	}
	// No rule in force: the Owner's card.
	w := newWorld(t)
	t2, err := w.task(t, "M-2", s2, "", "implement")
	ok(t, err)
	if out := setRule(w, "k0", s2, `{"task_accepted":"`+t2+`"}`); out["decision"] != "" {
		t.Fatalf("without the rule: %v", out)
	}

	w = newWorld(t)
	putPlanStanding(t, w, true, false)
	t2, err = w.task(t, "M-2", s2, "", "implement")
	ok(t, err)
	// Approvals 16 and 18 and question Q1 bound to s3 by a change the Owner
	// applied, with no rule yet.
	out := setRule(w, "b0", s3, `{}`, store.Binding{Kind: "approval", Ref: "16"}, store.Binding{Kind: "approval", Ref: "18"},
		store.Binding{Kind: "question", Ref: "Q1"})
	if out["decision"] != "" {
		t.Fatalf("a rule binding something new within the standing authorization: %v", out)
	}
	a.answer(out["question_id"].(string), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)

	// Within it: the first rule of a step not done, naming its own task.
	cards := len(a.cards)
	if out := setRule(w, "k1", s2, `{"task_accepted":"`+t2+`"}`); out["decision"] != "applied" || len(a.cards) != cards {
		t.Fatalf("a first rule naming the step's own task: %v", out)
	}
	var by string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided' ORDER BY event_id DESC LIMIT 1`).Scan(&by))
	if by != "standing authorization (plan-standing v1)" {
		t.Fatalf("decided by %q", by)
	}
	// Not within it (review 313 A2): binding something new, even an
	// approval long executed; a CI run, a gate, a manual atom; a rule that
	// holds already; a step with a rule; a step done. A gate's spec is stored
	// before a rule names it (TD-397).
	_, gateBody, gateSum := gateOf(t, "T19", "TestGrammarIsClosed")
	w.artifact(t, "task_spec", string(gateBody))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, aerr := store.Append(ctx, tx, store.Event{Stream: "approvals", Type: "approval_seen", Actor: "controller",
			Body: map[string]any{"approval": 16, "status": store.ApprovalExecuted}}); aerr != nil {
			return aerr
		}
		return store.SignOff(ctx, tx, "owner", s1, "TD-1")
	}))
	for name, c := range map[string]struct {
		step, rule string
		bind       []store.Binding
	}{
		"a new binding":   {s1, `{"approval_executed":16}`, []store.Binding{{Kind: "approval", Ref: "16"}}},
		"a CI run":        {s1, `{"ci_succeeded":{"repo":"bifrost-platform","commit":"2333e00753f91c74c8b2f4d693a803f8e89e7892"}}`, nil},
		"a gate":          {s1, `{"gates_passed":[{"test":"T19","spec_sha256":"` + gateSum + `"}],"env":"prod"}`, nil},
		"a manual atom":   {s1, `{"manual":"by hand"}`, nil},
		"already holding": {s1, `{"owner_signoff":"TD-1"}`, nil},
		// Bound already but outside records the runtime may not have read:
		// that they do not hold cannot be shown (review 314 A2).
		"an approval bound, not yet seen": {s3, `{"approval_executed":18}`, nil},
		"a question bound, not yet seen":  {s3, `{"question_answered":{"question":"Q1","option":"a"}}`, nil},
		"a second rule":                   {s2, `{"any":[{"task_accepted":"` + t2 + `"}]}`, nil},
	} {
		if out := setRule(w, "n-"+name, c.step, c.rule, c.bind...); out["decision"] != "" {
			t.Errorf("%s: decided at once: %v", name, out)
		}
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, s1, "done", "test")
		return err
	}))
	if out := setRule(w, "k4", s1, `{"owner_signoff":"TD-1"}`); out["decision"] != "" {
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

// The rule is read again once the plan stream's lock is held: a rule that
// expires while the write waits for that lock grants nothing (review 313 A4).
//
// Serial: a window of four seconds: the write must be waiting for the lock
// before the rule expires, which parallel neighbours could delay.
func TestPlanStandingExpiresWhileWaiting(t *testing.T) {
	for _, path := range []string{"activate", "standing"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			var now time.Time
			ok(t, w.rt.QueryRow(ctx, `SELECT date_trunc('second', clock_timestamp())`).Scan(&now))
			expires := now.Add(4 * time.Second).UTC()
			text := fmt.Sprintf("rule_id: plan-standing\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\n"+
				"expires_at: %s\ndone_rules: true\nactive_on_lease: true\n", expires.Format(time.RFC3339))
			r, err := rules.Parse([]byte(text))
			ok(t, err)
			body, err := r.BodyJSON()
			ok(t, err)
			_, err = w.rt.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
			  effective_at, expires_at) VALUES ('plan-standing', 1, $1, $2, 'initial', 'test', 'test', '2026-01-01T00:00:00Z', $3)`,
				string(body), text, expires)
			ok(t, err)
			t1, err := w.task(t, "M-1", s1, "", "implement")
			ok(t, err)
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				_, _, lerr := store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, 2*time.Minute)
				return lerr
			}))
			t2, err := w.task(t, "M-2", s2, "", "implement")
			ok(t, err)
			// Separate connections: the write waits on one, the test polls
			// on another, and another transaction holds the plan stream's
			// head.
			pool := w.db.Pool(t, w.db.Runtime)
			holder := w.db.Connect(t, w.db.Runtime)
			tx, err := holder.Begin(ctx)
			ok(t, err)
			_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id = $1 FOR UPDATE`, store.PlanStream(root))
			ok(t, err)
			done := make(chan map[string]any, 1)
			a := &asker{}
			go func() {
				if path == "activate" {
					n, aerr := store.ActivateLeasedSteps(ctx, pool)
					done <- map[string]any{"n": n, "err": aerr}
					return
				}
				out, perr := store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "test", []store.PlanChange{{Op: "set_done_rule",
					Node: s2, Rule: []byte(`{"task_accepted":"` + t2 + `"}`)}}, a.ask)
				if perr != nil {
					out = map[string]any{"err": perr}
				}
				done <- out
			}()
			waiting := false
			for i := 0; i < 200 && !waiting; i++ {
				ok(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database()
				  AND wait_event_type = 'Lock')`).Scan(&waiting))
				time.Sleep(20 * time.Millisecond)
			}
			if !waiting || !time.Now().Before(expires) {
				t.Fatal("the write was not waiting for the stream's lock before the rule expired")
			}
			time.Sleep(time.Until(expires) + 500*time.Millisecond)
			ok(t, tx.Commit(ctx))
			out := <-done
			switch path {
			case "activate":
				var state string
				ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, s1).Scan(&state))
				if out["err"] != nil || state != "planned" {
					t.Fatalf("an expired rule activated %s: %v, state %s", s1, out, state)
				}
			case "standing":
				if out["decision"] != "" || out["question_id"] == nil {
					t.Fatalf("an expired rule decided at once: %v", out)
				}
			}
		})
	}
}
