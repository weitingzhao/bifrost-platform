package actuation

import (
	"net/http"
	"sync"
	"time"
)

type Job struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type JobStore struct {
	mu   sync.Mutex
	jobs []Job
}

func NewJobStore() *JobStore {
	return &JobStore{}
}

func (s *JobStore) Add(action, target, status, detail string) Job {
	now := time.Now().UTC()
	job := Job{
		ID:        now.Format("20060102T150405.000000000Z"),
		Action:    action,
		Target:    target,
		Status:    status,
		Detail:    detail,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append([]Job{job}, s.jobs...)
	if len(s.jobs) > 100 {
		s.jobs = s.jobs[:100]
	}
	return job
}

func (s *JobStore) HandleList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"jobs": append([]Job(nil), s.jobs...)})
}
