package agentdeploy

import (
	"sync"
	"time"
)

type Job struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Remote     string     `json:"remote"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Log        string     `json:"log"`
	Error      string     `json:"error,omitempty"`
}

type Store struct {
	mu      sync.RWMutex
	current *Job
	last    *Job
}

func NewStore() *Store {
	return &Store{}
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

func (s *Store) Start(id, remote string) (*Job, bool) {
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
