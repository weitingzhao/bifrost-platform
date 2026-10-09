package cluster

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

type sharingBackend struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *sharingBackend) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), d...), nil
}

func (m *sharingBackend) Write(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
	return nil
}

func (m *sharingBackend) Update(_ context.Context, key string, mutate func(old []byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next, err := mutate(append([]byte(nil), m.data[key]...))
	if err != nil {
		return err
	}
	m.data[key] = append([]byte(nil), next...)
	return nil
}

func useCloneSharing(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	statefile.Use(&sharingBackend{data: map[string][]byte{}}, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULE", filepath.Join(root, "data-clone-schedule.json"))
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(root, "data-clone-last.json"))
}

func TestDataCloneScheduleShare(t *testing.T) {
	useCloneSharing(t)
	api := NewDataCloneScheduleStore()
	workers := NewDataCloneScheduleStore()
	cfg := api.Get()
	cfg.Enabled = true
	cfg.Interval = "weekly"
	api.Put(cfg)
	if got := workers.Get(); !got.Enabled || got.Interval != "weekly" {
		t.Fatalf("workers schedule %+v, want the schedule api wrote", got)
	}
}

func TestDataCloneLastReload(t *testing.T) {
	useCloneSharing(t)
	api := NewDataCloneLastStore()
	workers := NewDataCloneLastStore()
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	api.Record("job-1", []string{"bifrost_dev"}, at)
	got := workers.Get()
	if got.LastCloneJobID != "job-1" {
		t.Fatalf("workers last clone %+v, want job-1", got)
	}
}

func TestDataCloneRecordRunSurvives(t *testing.T) {
	useCloneSharing(t)
	api := NewDataCloneScheduleStore()
	workers := NewDataCloneScheduleStore()
	cfg := api.Get()
	cfg.Enabled = true
	cfg.Interval = "daily"
	api.Put(cfg)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			cur := api.Get()
			cur.Enabled = true
			cur.Interval = "daily"
			api.Put(cur)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			workers.RecordRun(fmt.Sprintf("job-%d", i), "started")
		}
	}()
	wg.Wait()
	got := api.Get()
	if !got.Enabled || got.Interval != "daily" {
		t.Fatalf("schedule lost after interleaved writes: %+v", got)
	}
	if got.LastAutoRunID == "" {
		t.Fatal("record run was wiped by a schedule write")
	}
}
