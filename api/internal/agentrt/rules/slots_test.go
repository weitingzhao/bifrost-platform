package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

var elastic = text("concurrency", `per_host: 3
slots:
  policy: aimd
  min: 1
  start: 2
`)

// A concurrency rule's slots block reads with its defaults filled in, and the
// doc carries it whole; a rule without one keeps the doc it had.
func TestConcurrencySlotsBlock(t *testing.T) {
	r := parse(t, elastic)
	want := rules.Slots{Policy: "aimd", Min: 1, Start: 2, IntervalS: 300, Step: 1,
		Thresholds: rules.SlotThresholds{LoadHighPercent: 100, LoadLowPercent: 70, MemLowPercent: 10}}
	if r.Slots == nil || *r.Slots != want || *r.PerHost != 3 {
		t.Fatalf("slots %+v per_host %d, want %+v and 3", r.Slots, *r.PerHost, want)
	}
	body, err := r.BodyJSON()
	if err != nil || !strings.Contains(string(body), `"slots":{"interval_s":300,"min":1,"policy":"aimd","start":2,"step":1,`+
		`"thresholds":{"load_high_percent":100,"load_low_percent":70,"mem_low_percent":10}}`) {
		t.Fatalf("body %s %v", body, err)
	}
	full := parse(t, text("concurrency", `per_host: 2
slots:
  policy: fixed
  min: 0
  start: 2
  interval_s: 600
  step: 2
  thresholds: {load_high_percent: 150, mem_low_percent: 5}
`))
	if s := full.Slots; s.Policy != "fixed" || s.IntervalS != 600 || s.Step != 2 ||
		s.Thresholds != (rules.SlotThresholds{LoadHighPercent: 150, LoadLowPercent: 70, MemLowPercent: 5}) {
		t.Fatalf("stated block %+v", s)
	}
	if plain := parse(t, concurrency); plain.Slots != nil {
		t.Fatalf("a rule without the block has %+v", plain.Slots)
	} else if body, _ := plain.BodyJSON(); strings.Contains(string(body), "slots") {
		t.Fatalf("a rule without the block has a doc with one: %s", body)
	}
	for name, s := range map[string]string{
		"an unknown policy":       strings.Replace(elastic, "policy: aimd", "policy: pid", 1),
		"no policy":               strings.Replace(elastic, "  policy: aimd\n", "", 1),
		"no start":                strings.Replace(elastic, "  start: 2\n", "", 1),
		"min above per_host":      strings.Replace(elastic, "min: 1", "min: 4", 1),
		"start below min":         strings.Replace(elastic, "start: 2", "start: 0", 1),
		"start above per_host":    strings.Replace(elastic, "start: 2", "start: 4", 1),
		"an interval too short":   elastic + "  interval_s: 10\n",
		"a step of none":          elastic + "  step: 0\n",
		"an unknown field":        elastic + "  ceiling: 5\n",
		"an unknown threshold":    elastic + "  thresholds: {cpu: 50}\n",
		"low not below high":      elastic + "  thresholds: {load_high_percent: 70, load_low_percent: 70}\n",
		"memory past a hundred":   elastic + "  thresholds: {mem_low_percent: 101}\n",
		"a block on another kind": budgetWith("slots: {policy: fixed, min: 1, start: 1}\n"),
		"not a mapping":           text("concurrency", "per_host: 1\nslots: aimd\n"),
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func budgetWith(extra string) string { return budget + extra }

// SlotRange is what a rule may give a host; adding or narrowing a block
// within per_host tightens, anything that reaches higher or holds a host at
// more loosens.
func TestSlotsTighten(t *testing.T) {
	next := func(s string) string {
		return strings.Replace(s, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1)
	}
	three := text("concurrency", "per_host: 3\n")
	for name, c := range map[string]struct {
		prev, next string
		lo, hi     int
		tighter    bool
	}{
		"aimd added within per_host": {three, next(elastic), 1, 3, true},
		"the block dropped":          {elastic, next(three), 3, 3, false},
		"min raised":                 {elastic, next(strings.Replace(elastic, "min: 1", "min: 2", 1)), 2, 3, false},
		"min lowered":                {elastic, next(strings.Replace(elastic, "min: 1", "min: 0", 1)), 0, 3, true},
		"start moved":                {elastic, next(strings.Replace(elastic, "start: 2", "start: 3", 1)), 1, 3, true},
		"fixed below aimd's max":     {elastic, next(strings.Replace(elastic, "policy: aimd", "policy: fixed", 1)), 2, 2, false},
		"thresholds changed":         {elastic, next(elastic + "  thresholds: {load_high_percent: 200}\n"), 1, 3, true},
		"per_host up with the block": {strings.Replace(elastic, "per_host: 3", "per_host: 2", 1), next(elastic), 1, 3, false},
		"fixed to aimd reaches more": {strings.Replace(elastic, "policy: aimd", "policy: fixed", 1), next(elastic), 1, 3, false},
	} {
		n := parse(t, c.next)
		if lo, hi := rules.SlotRange(n); lo != c.lo || hi != c.hi {
			t.Errorf("%s: range %d..%d, want %d..%d", name, lo, hi, c.lo, c.hi)
		}
		err := rules.Tightens(parse(t, c.prev), n)
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}
