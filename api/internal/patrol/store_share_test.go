package patrol

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// sharingBackend is the two-process stand-in: platform-api and platform-workers
// each hold a Store, and both talk to this one backend.
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

func useSharing(t *testing.T) (api, workers *Store) {
	t.Helper()
	root := t.TempDir()
	statefile.Use(&sharingBackend{data: map[string][]byte{}}, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	dir := filepath.Join(root, "patrol")
	var err error
	api, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	workers, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return api, workers
}

func TestOtherStoreSeesAppendedRun(t *testing.T) {
	api, workers := useSharing(t)
	if err := api.AppendRun(PatrolRun{ID: "r1", SkillID: "fleet"}); err != nil {
		t.Fatal(err)
	}
	runs, total := workers.ListRuns(10)
	if total != 1 || len(runs) != 1 || runs[0].ID != "r1" {
		t.Fatalf("workers list %+v total %d, want the run api appended", runs, total)
	}
	if last := workers.LastRun("fleet"); last == nil || last.ID != "r1" {
		t.Fatalf("workers LastRun = %+v", last)
	}
}

func TestOtherStoreSeesEnableFlag(t *testing.T) {
	api, workers := useSharing(t)
	if workers.Enabled("fleet", true) != true {
		t.Fatal("unset skill should keep the yaml default")
	}
	if err := api.SetEnabled("fleet", false); err != nil {
		t.Fatal(err)
	}
	if workers.Enabled("fleet", true) {
		t.Fatal("workers still see the skill enabled after api turned it off")
	}
}

func TestInterleavedWritesLoseNothing(t *testing.T) {
	api, workers := useSharing(t)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			if err := api.SetEnabled("skill", i%2 == 0); err != nil {
				t.Errorf("enable: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			if err := workers.AppendRun(PatrolRun{ID: runID(i), SkillID: "skill"}); err != nil {
				t.Errorf("append: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	runs, total := api.ListRuns(MaxRuns)
	if total != 40 {
		t.Fatalf("runs=%d, want 40 (an enable must not wipe a run)", total)
	}
	seen := map[string]bool{}
	for _, r := range runs {
		seen[r.ID] = true
	}
	for i := 0; i < 40; i++ {
		if !seen[runID(i)] {
			t.Fatalf("missing run %s", runID(i))
		}
	}
	if _, ok := mustEnabled(t, workers); !ok {
		t.Fatal("enable flag missing after the interleaved writes")
	}
}

func runID(i int) string {
	return "run-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}

func mustEnabled(t *testing.T, s *Store) (bool, bool) {
	t.Helper()
	rec, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := rec.Enabled["skill"]
	return v, ok
}
