package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sha40 is a commit id made of one hex digit.
func sha40(c string) string { return strings.Repeat(c, 40) }

// TD-383 (review 436 A1, A2): main's results by task: the commit a merge was
// cherry-picked from, or the commit itself, to the newest main commit that
// still stands as it. A git revert undoes a landing, a revert of that revert
// restores it; a trailer in prose, or one that is no task id, names nothing;
// a commit before since ends the read.
func TestMainResults(t *testing.T) {
	now := time.Now().UTC()
	const t1, t2, t3 = "t_01M5B8BF2EM3SJQDKZ9E9TY2H8", "t_01M5B8BF2EM3SJQDKZ9E9TY2H9", "t_01M5B8BF2EM3SJQDKZ9E9TY2HA"
	acc1, acc2, other, r1, r2, l3 := sha40("a"), sha40("b"), sha40("c"), sha40("1"), sha40("2"), sha40("3")
	picks := func(subject, from, task string) string {
		return subject + "\n\nWhy.\n\n(cherry picked from commit " + from + ")\n\nAgentrt-Task: " + task + "\nChange-Id: I1"
	}
	commits := []struct {
		sha, msg string
		at       time.Time
	}{
		// t3 landed, was reverted, and the revert was reverted: it stands.
		{sha40("6"), "Reapply \"agentrt: three\"\n\nThis reverts commit " + sha40("5") + ".", now.Add(-time.Hour)},
		{sha40("5"), "Revert \"agentrt: three\"\n\nThis reverts commit " + l3 + ".", now.Add(-2 * time.Hour)},
		{l3, picks("agentrt: three", sha40("d"), t3), now.Add(-3 * time.Hour)},
		// t2's landing is reverted; a hand revert written otherwise is not read as one.
		{sha40("4"), "Revert \"agentrt: two\"\n\nThis reverts commit " + r2 + ".", now.Add(-4 * time.Hour)},
		{sha40("7"), "agentrt: undo one by hand\n\nThis reverts commit " + r1 + ".", now.Add(-5 * time.Hour)},
		{r2, picks("agentrt: two", acc2, t2), now.Add(-6 * time.Hour)},
		// t1: a later merge of another commit of the same task, the accepted
		// one picked, and a fast-forward of a commit of the task's own.
		{sha40("8"), picks("agentrt: one, again", other, t1), now.Add(-7 * time.Hour)},
		{r1, picks("agentrt: one", acc1, t1), now.Add(-8 * time.Hour)},
		{sha40("9"), "agentrt: one, its own\n\nAgentrt-Task: " + t1, now.Add(-9 * time.Hour)},
		{sha40("e"), "docs: a note\n\nThe body names Agentrt-Task: " + t2 + " in prose only.\n(cherry picked from commit " + acc2 + ")", now.Add(-10 * time.Hour)},
		{sha40("f"), "agentrt: other\n\nAgentrt-Task: not-a-task", now.Add(-11 * time.Hour)},
		{sha40("0"), picks("agentrt: before", acc2, t2), now.Add(-48 * time.Hour)},
	}
	pages := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/bifrost/r1/commits", func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Query().Get("sha") != "main" || r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		var out []map[string]any
		for _, c := range commits {
			out = append(out, map[string]any{"sha": c.sha, "commit": map[string]any{"message": c.msg, "committer": map[string]any{"date": c.at}}})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	got, err := MainResults(context.Background(), Access{Base: srv.URL, Org: "bifrost"}, srv.Client(), "r1", now.Add(-24*time.Hour), 5)
	if err != nil || pages != 1 {
		t.Fatalf("main results: %v, %d pages", err, pages)
	}
	if got[t1][acc1] != r1 || got[t1][other] != sha40("8") || got[t1][sha40("9")] != sha40("9") {
		t.Errorf("t1: %v; want acc1 as %s, the other commit as its own merge, the fast-forward as itself", got[t1], r1[:8])
	}
	if _, ok := got[t2]; ok {
		t.Errorf("t2: %v; its landing is reverted", got[t2])
	}
	if got[t3][sha40("d")] != l3 {
		t.Errorf("t3: %v; a revert of the revert restores it", got[t3])
	}
	if len(got) != 2 {
		t.Errorf("tasks: %v", got)
	}
}
