package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type capabilitiesDoc struct {
	Scores []struct {
		ID    string  `json:"capability_score_id"`
		Class string  `json:"task_class"`
		Score float64 `json:"score"`
		End   string  `json:"window_end"`
	} `json:"scores"`
	Candidates []struct {
		Job    string `json:"job_id"`
		Status string `json:"status"`
	} `json:"candidates"`
	Facts []struct {
		ID      string `json:"capability_fact_id"`
		Kind    string `json:"fact_kind"`
		Subject string `json:"subject"`
	} `json:"facts"`
	Truncated bool `json:"truncated"`
}

// legacyRows writes rows under the import event, which may write any table.
func legacyRows(t *testing.T, pool *pgxpool.Pool, stmts ...string) {
	t.Helper()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: capabilities"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

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
	// The same window computed twice at the same time: the larger id (review 350 B1).
	_, err := pool.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
	  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework, score,
	  computed_from_event_id, computed_at) SELECT id, 'exec:cursor', 'cursor-mix', 'auto', v, 'implementer', 'ts', false, 'in_job',
	  '2026-10-10', '2026-10-17', 4, 2, 3, 1, sc, $1, '2026-10-17T01:00:00Z' FROM (VALUES ('s4', '1', 0.3), ('s5', '2', 0.4)) x(id, v, sc)`, ev)
	must(t, err)
	// A live fact and a retired one.
	legacyRows(t, pool, `INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source)
	  VALUES ('f_live', 'exec:cursor', 'cannot', 'live subject', 'measured', 'test')`,
		`INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source, retired_at)
	  VALUES ('f_retired', 'exec:cursor', 'cannot', 'retired subject', 'measured', 'test', now())`)
	var out capabilitiesDoc
	must(t, json.Unmarshal(get(t, s, "capabilities"), &out))
	if len(out.Scores) != 3 || out.Scores[0].Class != "go" || out.Scores[0].Score != 0.8 || out.Scores[1].Class != "python" ||
		out.Scores[2].ID != "s5" {
		t.Fatalf("scores = %+v; want the latest go window, the python one and s5 of the tie", out.Scores)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Job != "implementer" || out.Candidates[0].Status != "active" {
		t.Fatalf("candidates = %+v", out.Candidates)
	}
	facts := map[string]bool{}
	for _, f := range out.Facts {
		facts[f.ID] = true
	}
	if !facts["f_live"] || facts["f_retired"] || out.Truncated {
		t.Fatalf("facts = %+v, truncated %v; want f_live and not f_retired", out.Facts, out.Truncated)
	}
	if code, _ := call(t, s, "", "GET", "capabilities", nil); code != http.StatusUnauthorized {
		t.Fatalf("an anonymous read = %d", code)
	}
}

// Each list is capped at 1000 and says when it was cut: at 1000 rows it is
// whole, at 1001 truncated (review 350 B1).
func TestCapabilitiesTruncated(t *testing.T) {
	for _, list := range []string{"scores", "candidates", "facts"} {
		t.Run(list, func(t *testing.T) {
			pool, _ := world(t)
			s := server(t, pool)
			var ev int64
			must(t, pool.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event`).Scan(&ev))
			var have int
			fill := func(from, to int) {}
			switch list {
			case "scores":
				must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.capability_score`).Scan(&have))
				fill = func(from, to int) {
					_, err := pool.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
					  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework, score,
					  computed_from_event_id) SELECT 'sc' || i, 'exec:cursor', 'cursor-mix', 'auto', '1', 'implementer', 'c' || i, false, 'in_job',
					  '2026-10-10', '2026-10-17', 1, 1, 1, 0, 0.5, $3 FROM generate_series($1::int, $2::int) i`, from, to, ev)
					must(t, err)
				}
			case "candidates":
				must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.job_candidate`).Scan(&have))
				fill = func(from, to int) {
					legacyRows(t, pool, fmt.Sprintf(`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider,
					  model_pattern, status) SELECT 'jc' || i, 'implementer', 'exec:cursor', 'cursor-mix', 'p' || i, 'qualifying'
					  FROM generate_series(%d, %d) i`, from, to))
				}
			case "facts":
				must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.capability_fact WHERE retired_at IS NULL`).Scan(&have))
				fill = func(from, to int) {
					legacyRows(t, pool, fmt.Sprintf(`INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject,
					  source_kind, source) SELECT 'fc' || i, 'exec:cursor', 'prefer', 's' || i, 'measured', 'test' FROM generate_series(%d, %d) i`, from, to))
				}
			}
			listed := func() (int, bool) {
				t.Helper()
				var out capabilitiesDoc
				must(t, json.Unmarshal(get(t, s, "capabilities"), &out))
				return map[string]int{"scores": len(out.Scores), "candidates": len(out.Candidates), "facts": len(out.Facts)}[list], out.Truncated
			}
			fill(1, 1000-have)
			if n, cut := listed(); n != 1000 || cut {
				t.Fatalf("at 1000: %d listed, truncated %v", n, cut)
			}
			fill(1001-have, 1001-have)
			if n, cut := listed(); n != 1000 || !cut {
				t.Fatalf("at 1001: %d listed, truncated %v", n, cut)
			}
		})
	}
}
