package landinggit

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"strings"
	"testing"
)

func TestVersionedResultHistory(t *testing.T) {
	a, b, r, rr := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	msg := "result\n\nAgentrt-Task: " + testTask + "\nChange-Id: I" + a
	for _, tt := range []struct {
		name     string
		history  []HistoryCommit
		complete bool
		state    landing.History
	}{
		{"not landed", []HistoryCommit{{b, "unrelated"}}, true, landing.Never},
		{"fast forward", []HistoryCommit{{a, msg}}, true, landing.Effective},
		{"cherry pick", []HistoryCommit{{b, msg + "\n(cherry picked from commit " + a + ")"}}, true, landing.Effective},
		{"revert", []HistoryCommit{{r, "Revert \"result\"\n\nThis reverts commit " + b + "."}, {b, msg}}, true, landing.Reverted},
		{"revert of revert", []HistoryCommit{{rr, "Revert \"revert\"\n\nThis reverts commit " + r + "."}, {r, "Revert \"result\"\n\nThis reverts commit " + b + "."}, {b, msg}}, true, landing.Effective},
		{"another attempt same task", []HistoryCommit{{b, "other\n\nAgentrt-Task: " + testTask + "\nChange-Id: I" + b}}, true, landing.Never},
		{"only Work", []HistoryCommit{{b, "unrelated\n\nWork: W-31"}}, true, landing.Never},
		{"truncated", []HistoryCommit{{a, msg}}, false, landing.Unknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateHistory(testTask, a, msg, tt.history, tt.complete)
			if got.State != tt.state || (err != nil) != (tt.state == landing.Unknown) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	if _, err := EvaluateHistory("", a, "Work: W-31", []HistoryCommit{{a, "Work: W-31"}}, true); err == nil {
		t.Fatal("Work counted as lineage")
	}
	if _, err := EvaluateHistory(testTask, a, msg+"\nChange-Id: I"+b, nil, true); err == nil {
		t.Fatal("ambiguous source counted")
	}
}
