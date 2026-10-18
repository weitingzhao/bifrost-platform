package store

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// In-job scores (design v5 §5.1.2 「能力怎么量」 ④; W-31 2.4 slice ②). A
// task that ends (accepted or rejected) counts for the executor of its last
// attempt: one attempt, accepted or not, first pass when it was accepted in
// its first round at its first attempt, rework when it took more than one
// round. Grouped by executor, provider, model, job, task class and whether
// the work is core, over the last ScoreWindow; the score is the Wilson lower
// bound of the first-pass rate, so few samples score low. Never an LLM's
// judgement. Each computation appends a capability_scored event and its rows
// (capability_score is append only) and is made only when a task has ended
// since the last one; the event is written even when no row is (every task
// that ended is of a job no rule made), so the same verdicts are not counted
// again on every pass.

// ScoreWindow is how far back an in-job score looks.
const ScoreWindow = 30 * 24 * time.Hour

// wilsonZ is the normal quantile of the Wilson bound (95 %).
const wilsonZ = 1.96

// Wilson is the lower bound of the Wilson interval of k successes in n.
func Wilson(k, n int) float64 {
	if n <= 0 {
		return 0
	}
	p, z, nf := float64(k)/float64(n), wilsonZ, float64(n)
	d := 1 + z*z/nf
	c := p + z*z/(2*nf)
	r := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf))
	return math.Max(0, (c-r)/d)
}

type scoreKey struct {
	executor, provider, model, job, class string
	core                                  bool
}

type scoreRow struct {
	attempts, firstPass, accepted, rework int
	walls                                 []int
}

// ComputeScores appends this window's in-job scores when a task has ended
// since the last computation. It returns how many rows it wrote.
func ComputeScores(ctx context.Context, db DB) (int, error) {
	var n int
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		// One computation at a time: two would write the same window end.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742111)`); err != nil {
			return err
		}
		var through int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(event_id), 0) FROM agentrt.event
		  WHERE event_type = 'task_state_changed' AND body_json->>'to' IN ('accepted','rejected')`).Scan(&through); err != nil || through == 0 {
			return err
		}
		var last int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max((body_json->>'through_event_id')::bigint), 0) FROM agentrt.event
		  WHERE event_type = 'capability_scored' AND body_json->>'source' = 'in_job'`).Scan(&last); err != nil || last >= through {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		since := now.Add(-ScoreWindow)
		rows, err := tx.Query(ctx, `SELECT t.role, t.task_class, t.core OR EXISTS (SELECT 1 FROM agentrt.task s
		      WHERE s.task_id = t.subject_task_id AND s.core),
		    t.state, t.round_no, a.attempt_no, a.executor_actor_id, COALESCE(a.model_provider, ''), COALESCE(a.model, ''),
		    GREATEST(0, EXTRACT(EPOCH FROM COALESCE(a.computed_at, a.ended_at) - a.started_at))::int
		  FROM agentrt.task t
		  JOIN LATERAL (SELECT * FROM agentrt.attempt x WHERE x.task_id = t.task_id AND x.started_at IS NOT NULL
		    ORDER BY x.attempt_no DESC LIMIT 1) a ON true
		  WHERE t.state IN ('accepted','rejected') AND t.updated_at >= $1`, since)
		if err != nil {
			return err
		}
		groups := map[scoreKey]*scoreRow{}
		for rows.Next() {
			var role, class, state, executor, provider, model string
			var core bool
			var round, attemptNo, wall int
			if err = rows.Scan(&role, &class, &core, &state, &round, &attemptNo, &executor, &provider, &model, &wall); err != nil {
				rows.Close()
				return err
			}
			job, ok := jobOfRole[role]
			if !ok {
				continue
			}
			k := scoreKey{executor: executor, provider: provider, model: model, job: job, class: class, core: core}
			g := groups[k]
			if g == nil {
				g = &scoreRow{}
				groups[k] = g
			}
			g.attempts++
			if state == "accepted" {
				g.accepted++
				if round == 1 && attemptNo == 1 {
					g.firstPass++
				}
			}
			if round > 1 {
				g.rework++
			}
			g.walls = append(g.walls, wall)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		// a job no rule made has no row to score against
		jobRows, err := tx.Query(ctx, `SELECT job_id FROM agentrt.job`)
		if err != nil {
			return err
		}
		jobs, err := pgx.CollectRows(jobRows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, j := range jobs {
			known[j] = true
		}
		keys := make([]scoreKey, 0, len(groups))
		for k := range groups {
			if known[k.job] {
				keys = append(keys, k)
			}
		}
		if _, err = Append(ctx, tx, Event{Stream: "system", Type: "capability_scored", Actor: "controller",
			Body: map[string]any{"source": "in_job", "rows": len(keys), "through_event_id": through}}); err != nil {
			return err
		}
		sort.Slice(keys, func(i, j int) bool { return keyString(keys[i]) < keyString(keys[j]) })
		for _, k := range keys {
			g := groups[k]
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
			  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework,
			  median_wall_s, score, computed_from_event_id)
			  VALUES ($1,$2,$3,$4,'',$5,$6,$7,'in_job',$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
				"s_"+newULID(now), k.executor, k.provider, k.model, k.job, k.class, k.core, since, now,
				g.attempts, g.firstPass, g.accepted, g.rework, median(g.walls), Wilson(g.firstPass, g.attempts), through); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func keyString(k scoreKey) string {
	core := "0"
	if k.core {
		core = "1"
	}
	return k.executor + "|" + k.provider + "|" + k.model + "|" + k.job + "|" + k.class + "|" + core
}

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	return s[len(s)/2]
}
