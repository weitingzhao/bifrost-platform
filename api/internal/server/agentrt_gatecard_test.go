package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// TD-397 F-T33-rec (Gate A on STG, 2026-10-21: the catalog's thirteen gates
// came to the question store as card q_d58aaf5995349bf1 with no
// recommendation, STG's rule took "Do not apply it" and no gate was
// dispatched). On the real question store and a real database: the plan
// change the catalog command prints, every row given its first gates,
// recommends applying it, with why beside that option; STG's
// card-auto-answer rule takes that option and the decision applies it, so
// each row's done rule is the catalog's. A plan change the catalog does not
// give recommends nothing, as before: the rule takes the option that
// changes nothing and its row stays without a rule.
func TestGatePlanChangeRecommendsOnTheStore(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime) // platform_agentrt_dev: the catalog's rules are dev's
	specs, err := gaterun.Specs()
	if err != nil {
		t.Fatal(err)
	}
	const elsewhere = gaterun.ArchRowPrefix + "roles-chief-exam" // a row the catalog has no gates for
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		rows := []string{strings.TrimPrefix(elsewhere, gaterun.ArchRowPrefix)}
		for _, r := range gaterun.Catalog {
			rows = append(rows, r.Label)
		}
		for i, label := range rows {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: gaterun.ArchRowPrefix + label, Parent: "ARCH", Kind: "arch_row",
				Label: label, Title: label, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		for _, body := range specs {
			if _, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/plain", body); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The rule holds from a minute ago for a day, by the database's clock
	// (review td397-rec-1 B1: not a date the test would outlive).
	var now time.Time
	if err := conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	key := rptest.NewKey(t)
	text := "rule_id: " + rules.KindCardAuto + "\nrule_version: 1\ndirection: " + rules.Initial +
		"\neffective_at: " + now.Add(-time.Minute).UTC().Format(time.RFC3339) + "\nexpires_at: " + now.Add(24*time.Hour).UTC().Format(time.RFC3339) +
		"\nafter_seconds: 0\nkinds: [plan_change]\n"
	if _, err := store.SignRule(ctx, conn, store.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint); err != nil {
		t.Fatal(err)
	}

	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	rt.takeCardsByRule(true) // as STG's runtime is wired
	ask := rt.agentRuntimeAsk()
	thread := store.Writer{Actor: "thread:gate-a", Token: "operator"}
	set := func(node string, rule []byte) store.PlanChange {
		return store.PlanChange{Op: "set_done_rule", Node: node, Rule: json.RawMessage(rule)}
	}
	first, err := gaterun.Catalog[0].Rule("dev")
	if err != nil {
		t.Fatal(err)
	}
	propose := func(key string, changes ...store.PlanChange) string {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, conn, thread, key, "ARCH", "the architecture rows get their first gate tests from the reviewed catalog (TD-397)",
			changes, ask)
		id, _ := out["question_id"].(string)
		if err != nil || id == "" {
			t.Fatalf("%s: %v %v", key, out, err)
		}
		return id
	}
	other := propose("not the catalog's", set(elsewhere, first))
	var catalog []store.PlanChange
	want := map[string]string{}
	for _, r := range gaterun.Catalog {
		rule, err := r.Rule("dev")
		if err != nil {
			t.Fatal(err)
		}
		catalog = append(catalog, set(gaterun.ArchRowPrefix+r.Label, rule))
		want[gaterun.ArchRowPrefix+r.Label] = string(rule)
	}
	gates := propose("the catalog's", catalog...)

	q, ok, err := rt.svc.Lookup(gates)
	if err != nil || !ok || q.Recommended != store.CardApplyKey || len(q.Options) != 2 || q.Options[0].Label != store.CardApply ||
		!strings.Contains(q.Options[0].Detail, "the reviewed catalog's for dev") || q.Options[1].Detail != "" {
		t.Fatalf("the catalog's card: %+v %v %v", q, ok, err)
	}
	if q, ok, err = rt.svc.Lookup(other); err != nil || !ok || q.Recommended != "" || len(q.Options) != 2 || q.Options[0].Detail != "" {
		t.Fatalf("the other card: %+v %v %v", q, ok, err)
	}

	// STG's rule answers both; the database's clock may trail the store's.
	cr := store.CardRule{Cards: rt.agentRuntimeCards(), Answer: rt.agentRuntimeRuleAnswer(), Ack: rt.agentRuntimeAck()}
	answered := 0
	for deadline := time.Now().Add(10 * time.Second); answered < 2 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		n, err := store.AnswerByRule(ctx, conn, cr)
		if err != nil {
			t.Fatal(err)
		}
		answered += n
	}
	for id, option := range map[string]string{gates: store.CardApplyKey, other: store.CardRefuseKey} {
		if q, ok, err := rt.svc.Lookup(id); err != nil || !ok || q.Answer == nil || q.Answer.Option != option || q.Answer.Channel != "rule" {
			t.Fatalf("%s answered %+v (%d answered), want %s by the rule", id, q.Answer, answered, option)
		}
	}
	if n, err := store.DecidePlanChanges(ctx, conn, rt.agentRuntimeQuestion(), rt.agentRuntimeAck()); err != nil || n != 2 {
		t.Fatalf("decided %d: %v", n, err)
	}
	ruleOf := func(node string) string {
		t.Helper()
		var doc string
		if err := conn.QueryRow(ctx, `SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = $1`, node).Scan(&doc); err != nil {
			t.Fatal(err)
		}
		v, err := canon.Marshal([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		return string(v)
	}
	for node, rule := range want {
		if got := ruleOf(node); got != rule {
			t.Errorf("%s: %s, want the catalog's %s", node, got, rule)
		}
	}
	if got := ruleOf(elsewhere); got != "{}" {
		t.Errorf("%s: %s, want no rule", elsewhere, got)
	}
}

// Review td397-rec-1 A1: a plan change's card made by a try whose
// transaction then failed is found again by a retry under the same key when
// only what the runtime recommends has changed since (the plan moved), and
// keeps what it said; a retry under that key asking something else is
// refused, as before.
func TestPlanChangeCardRetryFindsItsCard(t *testing.T) {
	ctx := context.Background()
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ask := rt.agentRuntimeAsk()
	recommended := store.Card{Title: "Plan change to ARCH: the catalog's gates", Body: "A thread proposes to change plan ARCH.\n\n- a change",
		Recommended: 1, RecommendedWhy: "it gives architecture rows their first gates, exactly the reviewed catalog's for stg"}
	plain := recommended
	plain.Recommended, plain.RecommendedWhy = 0, ""
	for i, tries := range [][2]store.Card{{recommended, plain}, {plain, recommended}} {
		first, retry := tries[0], tries[1]
		first.Key = strings.Repeat(string(rune('a'+i)), 32)
		retry.Key = first.Key
		id, err := ask(ctx, first)
		if err != nil {
			t.Fatal(err)
		}
		if again, err := ask(ctx, retry); err != nil || again != id {
			t.Fatalf("try %d: the retry got %q (%v), want card %s", i, again, err, id)
		}
		q, ok, err := rt.svc.Lookup(id)
		if err != nil || !ok || (q.Recommended == store.CardApplyKey) != (first.Recommended == 1) {
			t.Fatalf("try %d: the card %+v %v %v", i, q, ok, err)
		}
		other := retry
		other.Body += "\n- another change"
		if got, err := ask(ctx, other); err == nil || !strings.Contains(err.Error(), "is not this one") {
			t.Fatalf("try %d: another proposal under the key got %q (%v)", i, got, err)
		}
	}
}
