package agentrtapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET /capabilities (2.5 G9): the latest window of each score group, the
// candidates, the facts not retired.
func TestCapabilities(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	var ev int64
	must(t, pool.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event`).Scan(&ev))
	for _, row := range []struct {
		id, class, end string
		score          float64
	}{{"s1", "go", "2026-10-10T00:00:00Z", 0.5}, {"s2", "go", "2026-10-17T00:00:00Z", 0.8}, {"s3", "python", "2026-10-17T00:00:00Z", 0.6}} {
		_, err := pool.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
		  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework, score,
		  computed_from_event_id) VALUES ($1, 'exec:cursor', 'cursor-mix', 'auto', '1', 'implementer', $2, false, 'in_job',
		  $3::timestamptz - interval '7 days', $3::timestamptz, 4, 2, 3, 1, $4, $5)`, row.id, row.class, row.end, row.score, ev)
		must(t, err)
	}
	var out struct {
		Scores []struct {
			Class string  `json:"task_class"`
			Score float64 `json:"score"`
			End   string  `json:"window_end"`
		} `json:"scores"`
		Candidates []struct {
			Job    string `json:"job_id"`
			Status string `json:"status"`
		} `json:"candidates"`
		Facts []struct {
			Kind    string `json:"fact_kind"`
			Subject string `json:"subject"`
		} `json:"facts"`
		Truncated bool `json:"truncated"`
	}
	must(t, json.Unmarshal(get(t, s, "capabilities"), &out))
	if len(out.Scores) != 2 || out.Scores[0].Class != "go" || out.Scores[0].Score != 0.8 || out.Scores[1].Class != "python" {
		t.Fatalf("scores = %+v; want the latest go window and the python one", out.Scores)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Job != "implementer" || out.Candidates[0].Status != "active" {
		t.Fatalf("candidates = %+v", out.Candidates)
	}
	if len(out.Facts) == 0 || out.Truncated {
		t.Fatalf("facts = %+v, truncated %v", out.Facts, out.Truncated)
	}
	if code, _ := call(t, s, "", "GET", "capabilities", nil); code != http.StatusUnauthorized {
		t.Fatalf("an anonymous read = %d", code)
	}
}
