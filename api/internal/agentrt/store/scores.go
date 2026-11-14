package store

import (
	"context"
	"errors"
	"math"
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
