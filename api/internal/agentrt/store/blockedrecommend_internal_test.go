package store

import (
	"strings"
	"testing"
	"time"
)

// The rules of a blocked card's recommendation, one case each and at their
// bounds (the Owner 2026-10-20): the first rule that applies wins, its why is
// one line of at most RecommendWhyMax bytes with no local path, and with no
// rule the card says why it recommends nothing.
func TestRecommendRules(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	ago := func(h float64) time.Time { return now.Add(-time.Duration(h * float64(time.Hour))) }
	base := func(kind string) recFacts {
		return recFacts{mission: "M-3", role: "implement", kind: kind, created: ago(5), now: now, budgetHours: 8, lastWork: ago(1),
			round: 2, maxRounds: 2}
	}
	with := func(f recFacts, edit func(*recFacts)) recFacts { edit(&f); return f }
	for _, c := range []struct {
		name   string
		f      recFacts
		option int
		rule   string
		why    string
	}{
		// R1: over a day old and at least 3× its hours, whatever blocked it.
		{"R1 old and far over", with(base(blockFailed), func(f *recFacts) {
			f.created, f.lastWork = ago(115), ago(100)
			f.check, f.verdict = "deterministic", "fail"
		}),
			recClose, "R1", "started 4 days ago, used 14× its 8 hours, nothing ran since 10-16"},
		{"R1 nothing ever ran", with(base(blockBudget), func(f *recFacts) { f.created, f.budgetHours, f.lastWork = ago(30), 2, time.Time{} }),
			recClose, "R1", "started 30 hours ago, used 15× its 2 hours, nothing has run"},
		{"R1 work ran lately", with(base(blockChain), func(f *recFacts) { f.created, f.budgetHours, f.lastWork = ago(30), 4, ago(2) }),
			recClose, "R1", "started 30 hours ago, used 7× its 4 hours, ran 2 hours ago"},
		{"R1 exactly 3×", with(base(blockBudget), func(f *recFacts) { f.created, f.budgetHours = ago(27), 9 }),
			recClose, "R1", "started 27 hours ago, used 3× its 9 hours, ran 1 hour ago"},
		{"not R1 under 3×", with(base(blockBudget), func(f *recFacts) { f.created, f.budgetHours = ago(26.9), 9 }),
			recNone, "", "only the budget stopped it, but M-3 started 26 hours ago and is under 3× its hours"},
		{"not R1 a day old exactly", with(base(blockFailed), func(f *recFacts) {
			f.created, f.budgetHours = ago(24), 2
			f.check, f.verdict = "deterministic", "fail"
		}), recDrop, "R5", "its checks failed in its last round (2 of 2)"},
		{"not R1 with no budget", with(base(blockChain), func(f *recFacts) { f.created, f.budgetHours, f.lastWork = ago(100), 0, ago(2) }),
			recDrop, "R2", "its only check was dropped on its card; other work in M-3 ran 2 hours ago, so drop only this task"},

		// R2: its only check was dropped on its card.
		{"R2 quiet", with(base(blockChain), func(f *recFacts) { f.lastWork = ago(25) }),
			recClose, "R2", "its only check was dropped on its card, and nothing in M-3 ran since 10-19"},
		{"R2 nothing ran", with(base(blockChain), func(f *recFacts) { f.lastWork = time.Time{} }),
			recClose, "R2", "its only check was dropped on its card, and nothing in M-3 has run"},
		{"R2 a day exactly", with(base(blockChain), func(f *recFacts) { f.lastWork = ago(24) }),
			recClose, "R2", "its only check was dropped on its card, and nothing in M-3 ran since 10-19"},
		{"R2 busy", with(base(blockChain), func(f *recFacts) { f.lastWork = ago(23.9) }),
			recDrop, "R2", "its only check was dropped on its card; other work in M-3 ran 23 hours ago, so drop only this task"},

		// R3: only the budget, a young mission, nothing failed this round.
		{"R3 not tried this round", base(blockBudget), recRetry, "R3", "only the budget stopped it"},
		{"R3 its check passed", with(base(blockBudget), func(f *recFacts) { f.check, f.verdict, f.checkedThisRound = "deterministic", "pass", true }),
			recRetry, "R3", "only the budget stopped it"},
		{"R3 a failed check of an earlier round", with(base(blockBudget), func(f *recFacts) { f.check, f.verdict = "deterministic", "fail" }),
			recRetry, "R3", "only the budget stopped it"},
		{"not R3 a failed check this round", with(base(blockBudget), func(f *recFacts) { f.check, f.verdict, f.checkedThisRound = "review", "fail", true }),
			recNone, "", "the budget stopped it after a check of this round that did not pass"},
		{"not R3 a day old", with(base(blockBudget), func(f *recFacts) { f.created = ago(24) }),
			recNone, "", "only the budget stopped it, but M-3 started 24 hours ago and is under 3× its hours"},

		// R4, R5: the last round's checks.
		{"R4", with(base(blockFailed), func(f *recFacts) { f.check, f.verdict, f.determined = "review", "fail", "pass" }),
			recNone, "R4", "needs your judgment: the checks pass, the reviewer failed it"},
		{"R5", with(base(blockFailed), func(f *recFacts) { f.check, f.verdict, f.determined = "deterministic", "fail", "fail" }),
			recDrop, "R5", "its checks failed in its last round (2 of 2)"},

		// No rule.
		{"a plan refused", with(base(blockFailed), func(f *recFacts) { f.role = "plan" }), recNone, "", "its plan was not used in its last round"},
		{"nothing handed in", base(blockFailed), recNone, "", "its last round handed in nothing the checks could use"},
		{"a gate", with(base(blockFailed), func(f *recFacts) { f.check, f.verdict = "gate", "none" }), recNone, "", "no rule covers its last check (gate, none)"},
		{"a dependency", base(blockDependency), recNone, "", "a task it depends on was rejected or cancelled"},
		{"an external attempt", base(blockLost), recNone, "", "its attempt ended on its host without a result"},
		{"anything else", base(blockOther), recNone, "", "no rule covers why it is blocked"},
	} {
		got := recommend(c.f)
		if got.option != c.option || got.rule != c.rule || got.why != c.why {
			t.Errorf("%s: got option %d, rule %q, why %q; want %d, %q, %q", c.name, got.option, got.rule, got.why, c.option, c.rule, c.why)
		}
		if len(got.why) > RecommendWhyMax || strings.ContainsAny(got.why, "\n/\\") {
			t.Errorf("%s: why %q is not one line of at most %d bytes with no path", c.name, got.why, RecommendWhyMax)
		}
	}
}

// The longest why the rules can write still fits, cut at a rune.
func TestRecommendWhyFits(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	long := "M-" + strings.Repeat("9", 40)
	for _, f := range []recFacts{
		{mission: long, kind: blockChain, now: now, created: now.Add(-time.Hour), lastWork: now.Add(-time.Hour)},
		{mission: long, kind: blockBudget, now: now, created: now.Add(-1e5 * time.Hour), budgetHours: 1 << 40, lastWork: now},
		{mission: long, kind: blockFailed, check: "deterministic", verdict: "fail", round: 1 << 30, maxRounds: 1 << 30, now: now, created: now},
	} {
		if why := recommend(f).why; len(why) > RecommendWhyMax || why == "" {
			t.Errorf("why %q is %d bytes", why, len(why))
		}
	}
	if got := clipBytes("ab×", 3); got != "ab" {
		t.Errorf("a cut inside a rune: %q", got)
	}
}

// Which block a reason is.
func TestBlockKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		reason string
		failed bool
		effect string
		want   string
	}{
		{"deterministic failed in round 2 of 2", true, "branch", blockFailed},
		{"the Owner chose not to do its review t_01M50GC0F41WYWCVCTS006SSCD (question q_dec5d32cdb81efc1)", false, "branch", blockChain},
		{"mission M-3 is past its 8 hours (it started 115.3 hours ago)" + budgetBlockSuffix, false, "branch", blockBudget},
		{"mission M-11 has used 2 of its 2 attempts" + budgetBlockSuffix, false, "none", blockBudget},
		{"dependency t_01M50GC0F41WYWCVCTS006SSCD is cancelled", false, "branch", blockDependency},
		{"attempt lost", false, "external", blockLost},
		{"test", false, "branch", blockOther},
	} {
		if got := blockKind(c.reason, c.failed, c.effect); got != c.want {
			t.Errorf("%q: %s, want %s", c.reason, got, c.want)
		}
	}
}

// The card's first line.
func TestRecommendationLine(t *testing.T) {
	t.Parallel()
	opts := blockedOptions("M-3")
	if got := (recommendation{option: recClose, why: "x"}).line(opts); got != "Recommended: Close mission M-3 — x" {
		t.Errorf("%q", got)
	}
	if got := (recommendation{why: "y"}).line(opts); got != "No recommendation: y" {
		t.Errorf("%q", got)
	}
}
