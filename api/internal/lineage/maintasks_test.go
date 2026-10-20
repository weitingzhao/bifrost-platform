package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TD-383: main's agent work by task, the newest commit naming each, back to
// since; a commit before since ends the read, and a trailer that is not a
// task id names nothing.
func TestMainTasks(t *testing.T) {
	now := time.Now().UTC()
	const t1, t2 = "t_01M5B8BF2EM3SJQDKZ9E9TY2H8", "t_01M5B8BF2EM3SJQDKZ9E9TY2H9"
	commits := []struct {
		sha, msg string
		at       time.Time
	}{
		{"n1", "agentrt: again\n\nAgentrt-Task: " + t1 + "\nChange-Id: I1", now.Add(-time.Hour)},
		{"x1", "docs: a note\n\nThe body names Agentrt-Task: " + t2 + " in prose only.", now.Add(-2 * time.Hour)},
		{"o1", "agentrt: first\n\nAgentrt-Task: " + t1 + "\nChange-Id: I1", now.Add(-3 * time.Hour)},
		{"w2", "agentrt: other\n\nAgentrt-Task: not-a-task", now.Add(-4 * time.Hour)},
		{"p2", "agentrt: before\n\nAgentrt-Task: " + t2, now.Add(-48 * time.Hour)},
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
	got, err := MainTasks(context.Background(), Access{Base: srv.URL, Org: "bifrost"}, srv.Client(), "r1", now.Add(-24*time.Hour), 5)
	if err != nil || len(got) != 1 || got[t1] != "n1" || pages != 1 {
		t.Fatalf("main tasks = %v (%v), %d pages; want %s at its newest commit only", got, err, pages, t1)
	}
}
