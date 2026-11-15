package store

import (
	"regexp"
	"time"
)

// Historical benchmark event shapes and validators. Host-driven benchmarks
// and competence-based dispatch are retired; stored events retain these names.

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
