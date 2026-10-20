package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func cardAutoText(version int, direction, after, kinds string) string {
	return "rule_id: card-auto-answer\nrule_version: " + map[int]string{1: "1", 2: "2", 3: "3"}[version] + "\ndirection: " + direction +
		"\neffective_at: 2026-10-20T00:00:00Z\nexpires_at: 2027-01-20T00:00:00Z\nafter_minutes: " + after + "\nkinds: " + kinds + "\n"
}

// TD-390: the rule that lets the platform answer a card with its own
// recommendation: how long a card waits first, and which kinds of card; read
// closed, written back as the text says, kinds sorted.
func TestCardAutoRule(t *testing.T) {
	r, err := rules.Parse([]byte(cardAutoText(1, "initial", "30", "[plan_question, blocked]")))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != rules.KindCardAuto || r.CardAuto == nil || r.CardAuto.AfterSeconds != 30*60 ||
		strings.Join(r.CardAuto.Kinds, ",") != "blocked,plan_question" {
		t.Fatalf("parsed = %+v %+v", r, r.CardAuto)
	}
	body, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"after_minutes":30`) || !strings.Contains(string(body), `"kinds":["blocked","plan_question"]`) {
		t.Fatalf("body = %s", body)
	}
	for name, text := range map[string]string{
		"too soon":          cardAutoText(1, "initial", "4", "[blocked]"),
		"too late":          cardAutoText(1, "initial", "10081", "[blocked]"),
		"a string":          cardAutoText(1, "initial", `"30"`, "[blocked]"),
		"no kind":           cardAutoText(1, "initial", "30", "[]"),
		"an unknown kind":   cardAutoText(1, "initial", "30", "[blocked, approval]"),
		"twice":             cardAutoText(1, "initial", "30", "[blocked, blocked]"),
		"kinds missing":     strings.Replace(cardAutoText(1, "initial", "30", "[blocked]"), "kinds: [blocked]\n", "", 1),
		"another field":     cardAutoText(1, "initial", "30", "[blocked]") + "prod: true\n",
		"an argument":       strings.Replace(cardAutoText(1, "initial", "30", "[blocked]"), "rule_id: card-auto-answer", "rule_id: card-auto-answer:dev", 1),
		"a kinds of scalar": cardAutoText(1, "initial", "30", "blocked"),
	} {
		if _, err := rules.Parse([]byte(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// Waiting longer or answering fewer kinds tightens; waiting less or a new
// kind loosens and needs the Owner's signature.
func TestCardAutoTightens(t *testing.T) {
	parse := func(text string) rules.Rule {
		t.Helper()
		r, err := rules.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	both := parse(cardAutoText(1, "initial", "30", "[blocked, plan_question]"))
	for _, next := range []string{cardAutoText(2, "tighten", "60", "[blocked, plan_question]"), cardAutoText(2, "tighten", "30", "[blocked]"),
		cardAutoText(2, "tighten", "30", "[blocked, plan_question]")} {
		if err := rules.Tightens(both, parse(next)); err != nil {
			t.Errorf("%s: %v", next, err)
		}
	}
	blocked := parse(cardAutoText(1, "initial", "30", "[blocked]"))
	for _, next := range []string{cardAutoText(2, "loosen", "29", "[blocked]"), cardAutoText(2, "loosen", "30", "[blocked, plan_question]")} {
		if err := rules.Tightens(blocked, parse(next)); err == nil {
			t.Errorf("counted as tightening: %s", next)
		}
	}
}

func cardAutoSeconds(version int, direction, wait, kinds string) string {
	return strings.Replace(cardAutoText(version, direction, "5", kinds), "after_minutes: 5", "after_seconds: "+wait, 1)
}

// TD-393 (the Owner 2026-10-20 「STG 的规则等待按秒、设 5 秒…所有种类的运行时卡都
// 归规则管」): the wait in seconds, from 0; every kind of card the runtime
// asks; a rule says after_seconds or after_minutes, never both, and its body
// says it as its text does, so versions 1 and 2 read as they were signed.
func TestCardAutoRuleInSeconds(t *testing.T) {
	all := "[blocked, plan_question, plan_change, plan_import, recorder_import, mission]"
	r, err := rules.Parse([]byte(cardAutoSeconds(3, "loosen", "5", all)))
	if err != nil {
		t.Fatal(err)
	}
	if r.CardAuto.AfterSeconds != 5 || strings.Join(r.CardAuto.Kinds, ",") != "blocked,mission,plan_change,plan_import,plan_question,recorder_import" {
		t.Fatalf("parsed = %+v", r.CardAuto)
	}
	body, err := r.BodyJSON()
	if err != nil || !strings.Contains(string(body), `"after_seconds":5`) || strings.Contains(string(body), "after_minutes") {
		t.Fatalf("body = %s (%v)", body, err)
	}
	if r, err = rules.Parse([]byte(cardAutoSeconds(3, "loosen", "0", "[mission]"))); err != nil || r.CardAuto.AfterSeconds != 0 {
		t.Fatalf("no wait: %+v %v", r.CardAuto, err)
	}
	for name, text := range map[string]string{
		"both":      strings.Replace(cardAutoSeconds(3, "loosen", "5", "[blocked]"), "kinds:", "after_minutes: 5\nkinds:", 1),
		"negative":  cardAutoSeconds(3, "loosen", "-1", "[blocked]"),
		"too long":  cardAutoSeconds(3, "loosen", "604801", "[blocked]"),
		"a float":   cardAutoSeconds(3, "loosen", "5.0", "[blocked]"),
		"no wait":   strings.Replace(cardAutoSeconds(3, "loosen", "5", "[blocked]"), "after_seconds: 5\n", "", 1),
		"duplicate": cardAutoSeconds(3, "loosen", "5", "[mission, mission]"),
	} {
		if _, err := rules.Parse([]byte(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	// From minutes to seconds the wait is compared in seconds: five
	// minutes to five seconds loosens, five seconds to ten tightens.
	parse := func(text string) rules.Rule {
		t.Helper()
		r, err := rules.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	v2 := parse(cardAutoText(2, "loosen", "5", "[blocked, plan_question]"))
	if err := rules.Tightens(v2, parse(cardAutoSeconds(3, "tighten", "5", "[blocked, plan_question]"))); err == nil {
		t.Error("five minutes to five seconds counted as tightening")
	}
	if err := rules.Tightens(v2, parse(cardAutoSeconds(3, "tighten", "300", "[blocked]"))); err != nil {
		t.Errorf("the same wait, fewer kinds: %v", err)
	}
	five := parse(cardAutoSeconds(3, "loosen", "5", all))
	if err := rules.Tightens(five, parse(cardAutoSeconds(3, "tighten", "10", all))); err != nil {
		t.Errorf("five seconds to ten: %v", err)
	}
	if err := rules.Tightens(five, parse(cardAutoSeconds(3, "tighten", "5", "[blocked, mission]"))); err != nil {
		t.Errorf("fewer kinds: %v", err)
	}
}
