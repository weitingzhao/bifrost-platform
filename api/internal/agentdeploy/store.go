package agentdeploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	raw, err := os.ReadFile(s.lastPath)
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

func (s *Store) persistLastLocked(j *Job) {
	if s.lastPath == "" || j == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.lastPath), 0o755); err != nil {
		return
	}
	// Trim log before persist — checklist only needs status/timestamps.
	toSave := *j
	if len(toSave.Log) > 16*1024 {
		toSave.Log = toSave.Log[len(toSave.Log)-16*1024:]
	}
	raw, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.lastPath, raw, 0o644)
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

func (s *Store) Start(id, remote, role string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && s.current.Status == "running" {
		return nil, false
	}
	now := time.Now().UTC()
	job := &Job{
		ID:        id,
		Status:    "running",
		Remote:    remote,
		Role:      role,
		StartedAt: now,
	}
	s.current = job
	return cloneJob(job), true
}

func (s *Store) AppendLog(delta string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return
	}
	const maxLog = 96 * 1024
	s.current.Log += delta
	if len(s.current.Log) > maxLog {
		s.current.Log = s.current.Log[len(s.current.Log)-maxLog:]
	}
}

func (s *Store) Finish(exitCode int, errMsg string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	now := time.Now().UTC()
	s.current.FinishedAt = &now
	s.current.ExitCode = &exitCode
	if exitCode == 0 {
		s.current.Status = "done"
	} else {
		s.current.Status = "failed"
		if errMsg != "" {
			s.current.Error = errMsg
		}
	}
	s.last = cloneJob(s.current)
	done := cloneJob(s.current)
	s.persistLastLocked(s.last)
	s.current = nil
	return done
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
