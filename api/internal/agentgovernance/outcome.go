package agentgovernance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Outcome is one finished skill run. The trust matrix reads these; nothing
// here starts an agent. Files written by the retired remediation runner still
// decode: extra JSON fields are ignored, and status values match.
type OutcomeStatus string

const (
	OutcomeDone   OutcomeStatus = "done"
	OutcomeFailed OutcomeStatus = "failed"
)

type Outcome struct {
	ID        string        `json:"id"`
	Status    OutcomeStatus `json:"status"`
	Summary   string        `json:"summary,omitempty"`
	Error     string        `json:"error,omitempty"`
	Actor     string        `json:"actor,omitempty"`
	Scope     string        `json:"scope,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// OutcomeStore persists skill outcomes on the platform host.
type OutcomeStore struct {
	mu  sync.Mutex
	dir string
}

func NewOutcomeStore() *OutcomeStore {
	dir := os.Getenv("PLATFORM_REMEDIATION_JOBS_DIR")
	if dir == "" {
		if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
			dir = filepath.Join(root, "agent", "remediation-jobs")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "remediation-jobs")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return &OutcomeStore{dir: dir}
}

func (s *OutcomeStore) Put(job Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = job.CreatedAt
	}
	raw, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.dir, job.ID+".json"), raw, 0o644)
}

func (s *OutcomeStore) List() []Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]Outcome, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var job Outcome
		if json.Unmarshal(raw, &job) == nil {
			out = append(out, job)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}
