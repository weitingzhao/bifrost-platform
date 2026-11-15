package store

import (
	"regexp"
	"time"
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
