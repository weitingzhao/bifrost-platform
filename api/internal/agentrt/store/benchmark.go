package store

import (
	"context"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Benchmark runs (design v5 §5.1.2 「能力怎么量」 ③; W-31 2.4 slice ⑤; the
// Owner 2026-10-18: run on the agent host, judge by the fix's own tests, the
// host hands the result in). A host's agentd runs the question set with one
// executor (agentd bench): for each question it first checks the question
// stands on this host (the fix passes its tests, the code before it with the
// fix's tests does not), then has the executor make the change the fix's
// commit message describes on the code before it, puts the fix's tests over
// what the executor wrote and runs them. The platform runs nothing again: it
// takes the result on the host's identity, as it takes a usage report.
//
// A run is a capability_scored event on its own stream with each question's
// outcome and the samples by task class, and one capability_score row per
// class (source benchmark, job implementer, not core). Routing's competence
// step adds, to the in-job samples, those of the latest run of each executor,
// provider and model in the last BenchmarkWindow (competence.go). A run is
// recorded once: handing it in again returns the first record.

// BenchmarksStream is the stream of benchmark runs.
const BenchmarksStream = "capability_benchmarks"

// BenchmarkWindow is how long a run's samples count.
const BenchmarkWindow = 90 * 24 * time.Hour

// BenchmarkResult is one question's outcome: pass, fail, or unrunnable (the
// question does not stand on this host; it counts for nobody).
type BenchmarkResult struct {
	Question string `json:"question"`
	Class    string `json:"class"`
	Outcome  string `json:"outcome"`
	WallS    int    `json:"wall_s"`
	Reason   string `json:"reason,omitempty"`
}

// BenchmarkRun is a run as agentd hands it in.
type BenchmarkRun struct {
	RunID     string `json:"run_id"`
	Executor  string `json:"executor"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Questions string `json:"questions_sha256"`
	// QuestionIDs are the questions the run answers: the results are one
	// for each, no more (a partial run is never recorded, review 287 B10).
	QuestionIDs []string          `json:"question_ids"`
	StartedAt   time.Time         `json:"started_at"`
	Results     []BenchmarkResult `json:"results"`
}

var (
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// latestBenchmarks is, for each executor, provider and model, the samples of
// the run that started last within the last BenchmarkWindow: a run handed in
// late does not stand for a newer one (review 287 B12). A run is recorded
// after it started, so the stream read back one window has every run that
// started within it.
func latestBenchmarks(ctx context.Context, tx pgx.Tx) ([]scoreLine, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 AND at >= $2 ORDER BY seq`,
		BenchmarksStream, now.Add(-BenchmarkWindow))
	if err != nil {
		return nil, err
	}
	bodies, err := pgx.CollectRows(rows, pgx.RowTo[map[string]any])
	if err != nil {
		return nil, err
	}
	type pick struct {
		started time.Time
		body    map[string]any
	}
	latest := map[string]pick{}
	for _, b := range bodies {
		s, _ := b["started_at"].(string)
		started, perr := time.Parse(time.RFC3339Nano, s)
		if perr != nil || started.Before(now.Add(-BenchmarkWindow)) {
			continue
		}
		str := func(k string) string { v, _ := b[k].(string); return v }
		k := str("executor") + "|" + str("provider") + "|" + str("model")
		if p, ok := latest[k]; !ok || started.After(p.started) {
			latest[k] = pick{started, b}
		}
	}
	keys := make([]string, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []scoreLine
	for _, k := range keys {
		out = append(out, samplesOf(latest[k].body)...)
	}
	return out, nil
}
