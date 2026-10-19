package agentrtapi

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /capabilities: who can do what, as the runtime measured it (2.5 G9,
// CUT-round2-2.5-interface-2026-10-19.md §2; Owner 2026-10-19 「2.5 八条照建议」):
// for each executor, provider, model, job, task class, core or not and source
// (exam, benchmark, in job), the score of its latest window; each job's
// candidates and their status; the capability facts not retired, each with
// the rule it is bound to (an Owner fact counts only while that rule is in
// force). A read, for the System entry beside the vendor pools.

// capabilitiesMax bounds each list; more says truncated.
const capabilitiesMax = 1000

func (a *API) capabilities(w http.ResponseWriter, r *http.Request) {
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `WITH scores AS (
		    SELECT DISTINCT ON (executor_actor_id, model_provider, model, job_id, task_class, core, source)
		      executor_actor_id, model_provider, model, tool_version, job_id, task_class, core, source, window_start, window_end,
		      attempts, first_pass, accepted, rework, review_misses, review_false_alarms, median_wall_s, score, computed_at,
		      computed_from_event_id
		    FROM agentrt.capability_score
		    ORDER BY executor_actor_id, model_provider, model, job_id, task_class, core, source, window_end DESC, computed_at DESC
		    LIMIT $1 + 1),
		  candidates AS (
		    SELECT job_candidate_id, job_id, executor_actor_id, model_provider, model_pattern, status, rule_id, rule_version, qualified_at
		    FROM agentrt.job_candidate ORDER BY job_id, executor_actor_id, model_provider, model_pattern LIMIT $1 + 1),
		  facts AS (
		    SELECT capability_fact_id, executor_actor_id, model_provider, fact_kind, subject, scope_json, source_kind, source,
		      rule_id, rule_version, recorded_at
		    FROM agentrt.capability_fact WHERE retired_at IS NULL ORDER BY executor_actor_id, subject, capability_fact_id LIMIT $1 + 1)
		  SELECT jsonb_build_object(
		    'scores', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.job_id, s.task_class, s.executor_actor_id, s.model_provider, s.model,
		      s.core, s.source) FROM (SELECT * FROM scores LIMIT $1) s), '[]'::jsonb),
		    'candidates', COALESCE((SELECT jsonb_agg(to_jsonb(c) ORDER BY c.job_id, c.executor_actor_id, c.model_provider, c.model_pattern)
		      FROM (SELECT * FROM candidates LIMIT $1) c), '[]'::jsonb),
		    'facts', COALESCE((SELECT jsonb_agg(to_jsonb(f) ORDER BY f.executor_actor_id, f.subject, f.capability_fact_id)
		      FROM (SELECT * FROM facts LIMIT $1) f), '[]'::jsonb),
		    'truncated', (SELECT count(*) FROM scores) > $1 OR (SELECT count(*) FROM candidates) > $1 OR (SELECT count(*) FROM facts) > $1)`,
			capabilitiesMax).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
