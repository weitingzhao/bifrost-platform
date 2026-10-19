package store

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// In-job scores (design v5 §5.1.2 「能力怎么量」 ④; W-31 2.4 slice ②). A
// task that ended counts for the executor of its last started attempt:
// accepted, rejected, or blocked as failed work (its checks failed with no
// round left, or it handed in nothing; failWork). One attempt each; a first
// pass when it was accepted in its first round at its first attempt, rework
// when it took more than one round. Grouped by executor, provider, model,
// job, task class and whether the work is core, over the last ScoreWindow;
// the score is the Wilson lower bound of the first-pass rate, so few samples
// score low. Never an LLM's judgement.
//
// Each computation is a capability_scored event on its own stream (the
// latest is the stream's last, read by its index) carrying the samples
// routing reads (competence.go), and one capability_score row per group (the
// append-only record). A computation is made when an outcome may be missing
// from the last one, or when a sample it counted has left the window
// (expires_at). Outcomes are told by transaction id, not event id: a
// transaction can take an event id and commit after a later one. Everything
// from before the last computation's snapshot xmin was visible to it; of the
// outcome events of later transactions it lists those it saw (late); any
// other is new (review 286 B3, B4, B7).

// ScoreWindow is how far back an in-job score looks.
const ScoreWindow = 30 * 24 * time.Hour

// ScoresStream is the stream of in-job computations.
const ScoresStream = "capability_scores"

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

// outcomeEvents are the task moves that can change what a computation counts.
const outcomeEvents = `event_type = 'task_state_changed' AND (body_json->>'to' IN ('accepted','rejected','blocked')
	OR body_json->>'from' IN ('accepted','rejected','blocked'))`

// computation is an in-job computation as its event records it.
type computation struct {
	xmin    int64
	late    []int64
	expires *time.Time
	samples []scoreLine
}

// lastComputation reads the latest in-job computation, or nil.
func lastComputation(ctx context.Context, tx pgx.Tx) (*computation, error) {
	var body map[string]any
	err := tx.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`,
		ScoresStream).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &computation{xmin: int64(numberOf(body["xmin"]))}
	l, _ := body["late"].([]any)
	for _, v := range l {
		c.late = append(c.late, int64(numberOf(v)))
	}
	if s, ok := body["expires_at"].(string); ok {
		if t, perr := time.Parse(time.RFC3339Nano, s); perr == nil {
			c.expires = &t
		}
	}
	c.samples = samplesOf(body)
	return c, nil
}

// samplesOf reads the samples a capability_scored body carries.
func samplesOf(body map[string]any) []scoreLine {
	var out []scoreLine
	l, _ := body["samples"].([]any)
	for _, v := range l {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		str := func(k string) string { s, _ := m[k].(string); return s }
		core, _ := m["core"].(bool)
		out = append(out, scoreLine{source: sourceOf(body), executor: str("executor"), provider: str("provider"), model: str("model"),
			job: str("job"), class: str("class"), core: core, attempts: int(numberOf(m["attempts"])), firstPass: int(numberOf(m["first_pass"]))})
	}
	return out
}

func sourceOf(body map[string]any) string { s, _ := body["source"].(string); return s }

func numberOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// due says whether a computation is needed after last at now: there is none
// yet, a sample it counted has left the window, or an outcome event it did
// not see.
func due(ctx context.Context, tx pgx.Tx, last *computation, now time.Time) (bool, error) {
	if last == nil || (last.expires != nil && !now.Before(*last.expires)) {
		return true, nil
	}
	ids, err := outcomesSince(ctx, tx, last.xmin)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if !slices.Contains(last.late, id) {
			return true, nil
		}
	}
	return false, nil
}

// outcomesSince lists the outcome events of transactions from xmin on.
func outcomesSince(ctx context.Context, tx pgx.Tx, xmin int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT event_id FROM agentrt.event WHERE txid >= $1 AND `+outcomeEvents+` ORDER BY event_id`, xmin)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// ComputeScores appends this window's in-job scores when they may have
// changed since the last computation. It returns how many rows it wrote.
func ComputeScores(ctx context.Context, db DB) (int, error) {
	var n int
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		// One computation at a time: two would count the same outcomes.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742111)`); err != nil {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		last, err := lastComputation(ctx, tx)
		if err != nil {
			return err
		}
		if ok, derr := due(ctx, tx, last, now); derr != nil || !ok {
			return derr
		}
		// In this order: every transaction below xmin has ended, so the
		// count sees its outcomes; the late outcomes listed were committed
		// before the count reads, so it sees those too.
		var xmin int64
		if err = tx.QueryRow(ctx, `SELECT txid_snapshot_xmin(txid_current_snapshot())`).Scan(&xmin); err != nil {
			return err
		}
		late, err := outcomesSince(ctx, tx, xmin)
		if err != nil {
			return err
		}
		since := now.Add(-ScoreWindow)
		groups, oldest, err := countOutcomes(ctx, tx, since)
		if err != nil {
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
		keys := make([]scoreKey, 0, len(groups))
		for k := range groups {
			if slices.Contains(jobs, k.job) {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keyString(keys[i]) < keyString(keys[j]) })
		samples := []any{}
		for _, k := range keys {
			g := groups[k]
			samples = append(samples, map[string]any{"executor": k.executor, "provider": k.provider, "model": k.model, "job": k.job,
				"class": k.class, "core": k.core, "attempts": g.attempts, "first_pass": g.firstPass})
		}
		lateIDs := make([]any, 0, len(late))
		for _, id := range late {
			lateIDs = append(lateIDs, id)
		}
		body := map[string]any{"source": "in_job", "rows": len(keys), "xmin": xmin, "late": lateIDs, "samples": samples}
		if oldest != nil {
			body["expires_at"] = oldest.Add(ScoreWindow).UTC().Format(time.RFC3339Nano)
		}
		ev, err := Append(ctx, tx, Event{Stream: ScoresStream, Type: "capability_scored", Actor: "controller", Body: body})
		if err != nil {
			return err
		}
		for _, k := range keys {
			g := groups[k]
			// The table's identity has no provider (TD-372): a second
			// provider of one executor and model in one computation keeps
			// its sample in the event, which routing reads, and no row.
			tag, err := tx.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
			  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework,
			  median_wall_s, score, computed_from_event_id)
			  VALUES ($1,$2,$3,$4,'',$5,$6,$7,'in_job',$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`,
				"s_"+newULID(now), k.executor, k.provider, k.model, k.job, k.class, k.core, since, now,
				g.attempts, g.firstPass, g.accepted, g.rework, median(g.walls), Wilson(g.firstPass, g.attempts), ev.EventID)
			if err != nil {
				return err
			}
			n += int(tag.RowsAffected())
		}
		return nil
	})
	return n, err
}

// countOutcomes groups the outcomes of tasks that ended since then, and
// returns when the earliest of them ended.
func countOutcomes(ctx context.Context, tx pgx.Tx, since time.Time) (map[scoreKey]*scoreRow, *time.Time, error) {
	rows, err := tx.Query(ctx, `SELECT t.role, t.task_class, t.core OR EXISTS (SELECT 1 FROM agentrt.task s
	      WHERE s.task_id = t.subject_task_id AND s.core),
	    t.state, t.round_no, t.updated_at, a.attempt_no, a.executor_actor_id, COALESCE(a.model_provider, ''), COALESCE(a.model, ''),
	    GREATEST(0, EXTRACT(EPOCH FROM COALESCE(a.computed_at, a.ended_at) - a.started_at))::int
	  FROM agentrt.task t
	  JOIN LATERAL (SELECT * FROM agentrt.attempt x WHERE x.task_id = t.task_id AND x.started_at IS NOT NULL
	    ORDER BY x.attempt_no DESC LIMIT 1) a ON true
	  WHERE t.updated_at >= $1 AND (t.state IN ('accepted','rejected') OR (t.state = 'blocked' AND
	    (SELECT (e.body_json->>'failed')::boolean FROM agentrt.event e WHERE e.task_id = t.task_id
	      AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1) IS TRUE))`, since)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	groups := map[scoreKey]*scoreRow{}
	var oldest *time.Time
	for rows.Next() {
		var role, class, state, executor, provider, model string
		var core bool
		var round, attemptNo, wall int
		var ended time.Time
		if err = rows.Scan(&role, &class, &core, &state, &round, &ended, &attemptNo, &executor, &provider, &model, &wall); err != nil {
			return nil, nil, err
		}
		job, ok := jobOfRole[role]
		if !ok {
			continue
		}
		if oldest == nil || ended.Before(*oldest) {
			e := ended
			oldest = &e
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
	return groups, oldest, rows.Err()
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
