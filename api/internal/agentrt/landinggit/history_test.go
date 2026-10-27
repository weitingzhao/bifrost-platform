package landinggit

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func hc(sha, msg string, parents ...string) HistoryCommit {
	return HistoryCommit{SHA: sha, Message: msg, Parents: parents}
}

func TestVersionedResultHistory(t *testing.T) {
	a, b, r, rr := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	msg := "result\n\nAgentrt-Task: " + testTask + "\nChange-Id: I" + a
	picked := msg + "\n(cherry picked from commit " + a + ")"
	for _, tt := range []struct {
		name     string
		history  []HistoryCommit
		complete bool
		state    landing.History
	}{
		{"not landed", []HistoryCommit{hc(b, "unrelated")}, true, landing.Never},
		{"fast forward", []HistoryCommit{hc(a, msg)}, true, landing.Effective},
		{"cherry pick", []HistoryCommit{hc(b, picked)}, true, landing.Effective},
		{"revert", []HistoryCommit{hc(r, "Revert \"result\"\n\nThis reverts commit "+b+".", b), hc(b, picked)}, true, landing.Reverted},
		{"revert of revert", []HistoryCommit{hc(rr, "Revert \"revert\"\n\nThis reverts commit "+r+".", r), hc(r, "Revert \"result\"\n\nThis reverts commit "+b+".", b), hc(b, picked)}, true, landing.Effective},
		{"another attempt same task", []HistoryCommit{hc(b, "other\n\nAgentrt-Task: "+testTask+"\nChange-Id: I"+b)}, true, landing.Never},
		{"only Work", []HistoryCommit{hc(b, "unrelated\n\nWork: W-31")}, true, landing.Never},
		{"truncated", []HistoryCommit{hc(a, msg)}, false, landing.Unknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateHistory(testTask, a, msg, tt.history, tt.complete)
			if got.State != tt.state || (err != nil) != (tt.state == landing.Unknown) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	if _, err := EvaluateHistory("", a, "Work: W-31", []HistoryCommit{hc(a, "Work: W-31")}, true); err == nil {
		t.Fatal("Work counted as lineage")
	}
	if _, err := EvaluateHistory(testTask, a, msg+"\nChange-Id: I"+b, nil, true); err == nil {
		t.Fatal("ambiguous source counted")
	}
}

// PV2 review 2026-10-26: three histories the first version judged Effective.
func TestReviewPV2RevertsAndProvenance(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 40) }
	a, p, m, side, rv, rv2, x, y := sha("a"), sha("1"), sha("2"), sha("3"), sha("4"), sha("5"), sha("6"), sha("7")
	msg := "result\n\nAgentrt-Task: " + testTask + "\nChange-Id: I" + a
	picked := msg + "\n(cherry picked from commit " + a + ")"
	// main: p ← m (merge of side, which carries the replayed result) ← rv (git revert -m 1 m)
	merged := []HistoryCommit{
		hc(m, "Merge branch 'w31/x'", p, side),
		hc(side, picked, p),
		hc(p, "base"),
	}
	revertMerge := "Revert \"Merge branch 'w31/x'\"\n\nThis reverts commit " + m + ", reversing\nchanges made to " + p + "."
	for _, tt := range []struct {
		name    string
		history []HistoryCommit
		state   landing.History
	}{
		{"merged", merged, landing.Effective},
		{"merge reverted", append([]HistoryCommit{hc(rv, revertMerge, m)}, merged...), landing.Reverted},
		{"merge revert names a foreign parent", append([]HistoryCommit{hc(rv, "Revert \"Merge branch 'w31/x'\"\n\nThis reverts commit "+m+", reversing\nchanges made to "+x+".", m)}, merged...), landing.Unknown},
		{"merge revert reverted", append([]HistoryCommit{hc(rv2, "Reapply \"Merge branch 'w31/x'\"\n\nThis reverts commit "+rv+".", rv), hc(rv, revertMerge, m)}, merged...), landing.Effective},
		{"two reverts in one commit", []HistoryCommit{hc(rv, "Revert two\n\nThis reverts commit "+x+".\n\nThis reverts commit "+y+".", x), hc(x, picked, y), hc(y, "other", p), hc(p, "base")}, landing.Reverted},
		// attempt 2 amended from attempt 1 keeps the Change-Id; main has only
		// attempt 1's replay, whose cherry-pick line names attempt 1.
		{"older attempt shares the Change-Id", []HistoryCommit{hc(x, msg+"\n(cherry picked from commit "+y+")", p), hc(p, "base")}, landing.Unknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateHistory(testTask, a, msg, tt.history, true)
			if got.State != tt.state || (err != nil) != (tt.state == landing.Unknown) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	// A folded trailer line does not hide the trailer block.
	folded := "result\n\nAgentrt-Task: " + testTask + "\nChange-Id: I" + a + "\nCo-Authored-By: someone with a long\n name <x@example.com>"
	if got, err := EvaluateHistory(testTask, a, folded, []HistoryCommit{hc(a, folded)}, true); err != nil || got.State != landing.Effective {
		t.Fatalf("folded trailer: %+v %v", got, err)
	}
}
