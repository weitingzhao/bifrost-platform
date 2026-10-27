package landinggit

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Work 632: the review631 quoted-provenance probe, with both standard -x
// layouts as controls and malformed/ambiguous trailer areas as adversaries.
func TestPV2StrictProvenance(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	msg := "result\n\nChange-Id: I" + a
	line := "(cherry picked from commit " + a + ")"
	for _, tt := range []struct {
		name, message string
		want          landing.History
	}{
		{"inline body example", "amended\n\nDo not use the old marker: " + line + "\n\nChange-Id: I" + a, landing.Unknown},
		{"standalone body example", "amended\n\n" + line + "\nThis is an example.\n\nChange-Id: I" + a, landing.Unknown},
		{"quoted", "amended\n\n> " + line + "\n\nChange-Id: I" + a, landing.Unknown},
		{"indented", msg + "\n " + line, landing.Unknown},
		{"code fence", "amended\n\n```\n" + line + "\n```\n\nChange-Id: I" + a, landing.Unknown},
		{"duplicate", msg + "\n" + line + "\n" + line, landing.Unknown},
		{"duplicate paragraphs", msg + "\n\n" + line + "\n\n" + line, landing.Unknown},
		{"conflicting", msg + "\n" + line + "\n(cherry picked from commit " + b + ")", landing.Unknown},
		{"trailing prose", "amended\n\n" + line + " trailing prose\n\nChange-Id: I" + a, landing.Unknown},
		{"same paragraph", msg + "\n" + line, landing.Effective},
		{"own final paragraph", msg + "\n\n" + line, landing.Effective},
		{"before trailers", "result\n\n" + line + "\n\nChange-Id: I" + a, landing.Effective},
		{"crlf", strings.ReplaceAll(msg+"\n"+line+"\n", "\n", "\r\n"), landing.Effective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, err := EvaluateHistory("", a, msg, []HistoryCommit{hc(b, tt.message)}, true)
			if h.State != tt.want || (err != nil) != (tt.want == landing.Unknown) {
				t.Fatalf("state=%s error=%v; want %s", h.State, err, tt.want)
			}
		})
	}
}

// Work 632: review631's edited merge-revert message cannot imply -m 1.
func TestPV2MergeRevertRequiresKeptParent(t *testing.T) {
	t.Parallel()
	a, p, m, r, base := strings.Repeat("a", 40), strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40), strings.Repeat("0", 40)
	msg := "result\n\nChange-Id: I" + a
	kept := func(parent string) string {
		return "This reverts commit " + m + ", reversing\nchanges made to " + parent + "."
	}
	for _, tt := range []struct {
		name, body string
		want       landing.History
	}{
		{"missing", "This reverts commit " + m + ".", landing.Unknown},
		{"conflicting", kept(p) + "\n\n" + kept(a), landing.Unknown},
		{"foreign", kept(base), landing.Unknown},
		{"first parent", kept(p), landing.Reverted},
		{"second parent", kept(a), landing.Effective},
		{"repeated same parent", kept(a) + "\n\n" + kept(a), landing.Effective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, err := EvaluateHistory("", a, msg, []HistoryCommit{hc(r, "Revert merge\n\n"+tt.body, m), hc(m, "merge", p, a), hc(a, msg, base), hc(p, "main work", base), hc(base, "base")}, true)
			if h.State != tt.want || (err != nil) != (tt.want == landing.Unknown) {
				t.Fatalf("state=%s error=%v; want %s", h.State, err, tt.want)
			}
		})
	}
}

// The runtime's own replay (replay.go) puts its provenance paragraph and the
// cherry-pick line before the original trailer block and appends Landed-By:
// the strict parser must still find the source and the task there.
func TestPV2ProvenanceOfRuntimeReplayLayout(t *testing.T) {
	a, landed := strings.Repeat("a", 40), strings.Repeat("b", 40)
	result := "feat: thing\n\nBody of the change.\n\nWork: W-31\nChange-Id: I" + a + "\nAgentrt-Task: " + testTask
	split := strings.LastIndex(result, "\n\n")
	msg := result[:split] + "\n\nLanded by the agent runtime (STG rehearsal) under landing:dev v1: mission M-1, task " + testTask + ", attempt a_1, reviewed by r, accepted 2026-10-26T00:00:00Z, replayed onto " + strings.Repeat("c", 40) + " as land:" + testTask + ":1.\n\n(cherry picked from commit " + a + ")" + result[split:] + "\nLanded-By: agentrt-stg/land:" + testTask + ":1\n"
	if got := CherryPickSource(msg); got != a {
		t.Fatalf("source = %q", got)
	}
	if !HasTaskTrailer(msg, testTask) {
		t.Fatal("task trailer not found in the replay layout")
	}
	h, err := EvaluateHistory(testTask, a, result, []HistoryCommit{{SHA: landed, Message: msg}}, true)
	if err != nil || h.State != landing.Effective || h.Commit != landed {
		t.Fatalf("%+v %v", h, err)
	}
}
