package remediation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// JobStore persists remediation jobs on platform-api host (survives runner / API restart).
type JobStore struct {
	mu  sync.Mutex
	dir string
}

func NewJobStore() *JobStore {
	dir := os.Getenv("PLATFORM_REMEDIATION_JOBS_DIR")
	if dir == "" {
		if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
			dir = filepath.Join(root, "agent", "remediation-jobs")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "remediation-jobs")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return &JobStore{dir: dir}
}

func (s *JobStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *JobStore) Put(job Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path(job.ID), raw, 0o644)
}

func (s *JobStore) Get(id string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, false
	}
	var job Job
	if json.Unmarshal(raw, &job) != nil {
		return nil, false
	}
	return &job, true
}

func (s *JobStore) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]Job, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var job Job
		if json.Unmarshal(raw, &job) == nil {
			out = append(out, job)
		}
	}
	sortJobsByUpdated(out)
	return out
}

func sortJobsByUpdated(list []Job) {
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].UpdatedAt.After(list[i].UpdatedAt) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
}

func mergeJobs(runner []Job, stored []Job) []Job {
	byID := make(map[string]Job, len(runner)+len(stored))
	for _, j := range stored {
		byID[j.ID] = j
	}
	for _, j := range runner {
		prev, ok := byID[j.ID]
		if !ok || j.UpdatedAt.After(prev.UpdatedAt) {
			byID[j.ID] = j
		}
	}
	out := make([]Job, 0, len(byID))
	for _, j := range byID {
		out = append(out, j)
	}
	sortJobsByUpdated(out)
	return out
}
