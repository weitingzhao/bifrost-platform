package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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

// benchmarkJob is the job the questions are work of: a change to make.
const benchmarkJob = "implementer"

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
	runIDRE      = regexp.MustCompile(`^[0-9A-Za-z_-]{8,64}$`)
	questionIDRE = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)
	sha256RE     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	modelRE      = regexp.MustCompile(`^[0-9A-Za-z._:/-]{0,100}$`)
)

// benchmarkClasses are the task classes a question can be of (the classes
// of v5 §5.1 that have tests).
var benchmarkClasses = map[string]bool{"go": true, "python": true, "ts": true, "shell": true}

var benchmarkOutcomes = map[string]bool{"pass": true, "fail": true, "unrunnable": true}

// checkBenchmark refuses a run that does not read as one.
func checkBenchmark(run BenchmarkRun, now time.Time) error {
	switch {
	case !runIDRE.MatchString(run.RunID):
		return invalid("run_id is 8 to 64 letters, digits, _ or -")
	case !strings.HasPrefix(run.Executor, "exec:") || run.Executor == "exec:deterministic":
		return invalid("executor %q is not an agent executor", run.Executor)
	case run.Provider == "" || !modelRE.MatchString(run.Provider) || !modelRE.MatchString(run.Model):
		return invalid("provider and model are short names")
	case !sha256RE.MatchString(run.Questions):
		return invalid("questions_sha256 is the hex sha256 of the question set")
	case run.StartedAt.IsZero() || run.StartedAt.After(now) || now.Sub(run.StartedAt) > 7*24*time.Hour:
		return invalid("started_at is a time in the last seven days")
	case len(run.Results) == 0 || len(run.Results) > 100:
		return invalid("a run has 1 to 100 results")
	case len(run.QuestionIDs) != len(run.Results):
		return invalid("a run has one result for each of its %d questions, not %d", len(run.QuestionIDs), len(run.Results))
	}
	asked := map[string]bool{}
	for _, q := range run.QuestionIDs {
		asked[q] = true
	}
	seen := map[string]bool{}
	for _, r := range run.Results {
		switch {
		case !questionIDRE.MatchString(r.Question) || seen[r.Question] || !asked[r.Question]:
			return invalid("question %q is not an id, comes twice or is not one of the run's", r.Question)
		case !benchmarkClasses[r.Class]:
			return invalid("question %s: class %q is not go, python, ts or shell", r.Question, r.Class)
		case !benchmarkOutcomes[r.Outcome]:
			return invalid("question %s: outcome %q is not pass, fail or unrunnable", r.Question, r.Outcome)
		case r.WallS < 0 || r.WallS > 86400:
			return invalid("question %s: wall_s is 0 to 86400", r.Question)
		case len(r.Reason) > 300 || strings.ContainsAny(r.Reason, "\n\r"):
			return invalid("question %s: the reason is one line of at most 300 bytes", r.Question)
		}
		seen[r.Question] = true
	}
	return nil
}

// RecordBenchmark records a run host handed in, or returns the record of the
// same run handed in before.
func RecordBenchmark(ctx context.Context, db DB, host string, run BenchmarkRun) (map[string]any, error) {
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if err := checkBenchmark(run, now); err != nil {
			return err
		}
		var tool string
		err := tx.QueryRow(ctx, `SELECT tool FROM agentrt.actor WHERE actor_id = $1 AND actor_kind = 'executor' AND retired_at IS NULL`,
			run.Executor).Scan(&tool)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("executor %s is not registered", run.Executor)
		}
		if err != nil {
			return err
		}
		digest, err := run.digest()
		if err != nil {
			return err
		}
		// One record per run of a host: looked for under this lock; the same
		// run again is the record made, another one under its id is refused
		// (review 287 B11).
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742112)`); err != nil {
			return err
		}
		var id int64
		var had string
		err = tx.QueryRow(ctx, `SELECT event_id, COALESCE(body_json->>'digest', '') FROM agentrt.event
		  WHERE event_stream_id = $1 AND body_json->>'run_id' = $2 AND body_json->>'host' = $3`,
			BenchmarksStream, run.RunID, host).Scan(&id, &had)
		if err == nil {
			if had != digest {
				return invalid("run %s of host %s was handed in before with other results", run.RunID, host)
			}
			out = map[string]any{"event_id": id, "duplicate": true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		type count struct {
			attempts, passes int
			walls            []int
		}
		byClass := map[string]*count{}
		results := []any{}
		for _, r := range run.Results {
			m := map[string]any{"question": r.Question, "class": r.Class, "outcome": r.Outcome, "wall_s": r.WallS}
			if r.Reason != "" {
				m["reason"] = r.Reason
			}
			results = append(results, m)
			if r.Outcome == "unrunnable" {
				continue
			}
			c := byClass[r.Class]
			if c == nil {
				c = &count{}
				byClass[r.Class] = c
			}
			c.attempts++
			if r.Outcome == "pass" {
				c.passes++
			}
			c.walls = append(c.walls, r.WallS)
		}
		classes := make([]string, 0, len(byClass))
		for k := range byClass {
			classes = append(classes, k)
		}
		sort.Strings(classes)
		samples := []any{}
		for _, k := range classes {
			c := byClass[k]
			samples = append(samples, map[string]any{"executor": run.Executor, "provider": run.Provider, "model": run.Model,
				"job": benchmarkJob, "class": k, "core": false, "attempts": c.attempts, "first_pass": c.passes})
		}
		body := map[string]any{"source": "benchmark", "rows": len(classes), "samples": samples, "run_id": run.RunID, "host": host,
			"executor": run.Executor, "provider": run.Provider, "questions": run.Questions, "results": results,
			"started_at": run.StartedAt.UTC().Format(time.RFC3339Nano), "digest": digest}
		if run.Model != "" {
			body["model"] = run.Model
		}
		ev, err := Append(ctx, tx, Event{Stream: BenchmarksStream, Type: "capability_scored", Actor: hostActor(host), Body: body})
		if err != nil {
			return err
		}
		start := run.StartedAt
		if !start.Before(now) {
			start = now.Add(-time.Second)
		}
		for _, k := range classes {
			c := byClass[k]
			// TD-372: the table's identity has no provider.
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
			  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework,
			  median_wall_s, score, computed_from_event_id)
			  VALUES ($1,$2,$3,$4,'',$5,$6,false,'benchmark',$7,$8,$9,$10,$10,0,$11,$12,$13) ON CONFLICT DO NOTHING`,
				"s_"+newULID(now), run.Executor, run.Provider, run.Model, benchmarkJob, k, start, now,
				c.attempts, c.passes, median(c.walls), Wilson(c.passes, c.attempts), ev.EventID); err != nil {
				return err
			}
		}
		out = map[string]any{"event_id": ev.EventID, "duplicate": false, "rows": len(classes)}
		return nil
	})
	return out, err
}

// digest is the sha256 of what the run says, for telling the same run from
// another under its id.
func (run BenchmarkRun) digest() (string, error) {
	results := []any{}
	for _, r := range run.Results {
		results = append(results, map[string]any{"q": r.Question, "c": r.Class, "o": r.Outcome, "w": r.WallS, "r": r.Reason})
	}
	doc, err := canon.Encode(map[string]any{"executor": run.Executor, "provider": run.Provider, "model": run.Model,
		"questions": run.Questions, "started_at": run.StartedAt.UTC().Format(time.RFC3339Nano), "results": results})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:]), nil
}

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
