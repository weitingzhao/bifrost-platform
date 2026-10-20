package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func cardAutoText(version int, direction, after, kinds string) string {
	return "rule_id: card-auto-answer\nrule_version: " + map[int]string{1: "1", 2: "2"}[version] + "\ndirection: " + direction +
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
	if r.Kind != rules.KindCardAuto || r.CardAuto == nil || r.CardAuto.AfterMinutes != 30 ||
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
		"a plan change":     cardAutoText(1, "initial", "30", "[blocked, plan_change]"),
		"a mission's card":  cardAutoText(1, "initial", "30", "[mission]"),
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
