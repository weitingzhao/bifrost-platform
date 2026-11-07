package agentdeploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

type Job struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Remote     string     `json:"remote"`
	Role       string     `json:"role,omitempty"` // primary | standby | custom
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Log        string     `json:"log"`
	Error      string     `json:"error,omitempty"`
}

type Store struct {
	mu       sync.RWMutex
	current  *Job
	last     *Job
	lastPath string
}

func NewStore() *Store {
	s := &Store{lastPath: resolveLastJobPath()}
	if j := s.loadLastLocked(); j != nil {
		s.last = j
	}
	return s
}

func resolveLastJobPath() string {
	if p := strings.TrimSpace(os.Getenv("PLATFORM_AGENT_DEPLOY_LAST")); p != "" {
		return p
	}
	data := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR"))
	if data == "" {
		if root := strings.TrimSpace(os.Getenv("PLATFORM_PROJECT_ROOT")); root != "" {
			data = filepath.Join(root, "data")
		} else {
			data = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data")
		}
	}
	return filepath.Join(data, "agent-deploy", "last.json")
}

func (s *Store) loadLastLocked() *Job {
	if s.lastPath == "" {
		return nil
	}
	raw, err := statefile.ReadFile(s.lastPath)
	if err != nil {
		return nil
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil
	}
	if job.Status == "" {
		return nil
	}
	return cloneJob(&job)
}

func (s *Store) Current() *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil
	}
	return cloneJob(s.current)
}

func (s *Store) Last() *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.last == nil {
		return nil
	}
	return cloneJob(s.last)
}

func (s *Store) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current != nil && s.current.Status == "running"
}

// ErrRunning is Begin's answer while another job runs.
var ErrRunning = errors.New("agentdeploy: another agent deploy is running")

// Begin makes job the current job, running; it is refused while another
// job runs.
func (s *Store) Begin(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && s.current.Status == "running" {
		return ErrRunning
	}
	job.Status = "running"
	s.current = cloneJob(job)
	return nil
}

// Logf appends one line to the current job's log.
func (s *Store) Logf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return
	}
	s.current.Log += time.Now().UTC().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...) + "\n"
}

// Finish ends the current job, done when err is nil and failed with it
// otherwise, and keeps it as the last job, on disk too.
func (s *Store) Finish(err error) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	now := time.Now().UTC()
	j := s.current
	j.FinishedAt = &now
	j.Status = "done"
	if err != nil {
		j.Status, j.Error = "failed", err.Error()
	}
	s.current, s.last = nil, j
	if s.lastPath != "" {
		if raw, merr := json.Marshal(j); merr == nil {
			_ = statefile.WriteFile(s.lastPath, raw, 0o644)
		}
	}
	return cloneJob(j)
}

func cloneJob(j *Job) *Job {
	if j == nil {
		return nil
	}
	out := *j
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		out.FinishedAt = &t
	}
	if j.ExitCode != nil {
		c := *j.ExitCode
		out.ExitCode = &c
	}
	return &out
}
