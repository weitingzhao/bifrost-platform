package codehealth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// historyLimit caps retained reports. Enough to render a trend; small enough
// that the directory stays readable by hand.
const historyLimit = 30

type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore() *Store {
	dir := os.Getenv("PLATFORM_CODE_HEALTH_DIR")
	if dir == "" {
		if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
			dir = filepath.Join(root, "agent", "code-health")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "code-health")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return &Store{dir: dir}
}

// Put stores a report and prunes the oldest beyond historyLimit.
func (s *Store) Put(r Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("report-%d.json", r.ReceivedAt.UTC().UnixNano())
	if err := os.WriteFile(filepath.Join(s.dir, name), raw, 0o644); err != nil {
		return err
	}
	s.prune()
	return nil
}

// prune drops the oldest files past historyLimit. Caller holds the lock.
func (s *Store) prune() {
	names := s.reportFiles()
	if len(names) <= historyLimit {
		return
	}
	for _, n := range names[historyLimit:] {
		_ = os.Remove(filepath.Join(s.dir, n))
	}
}

// reportFiles returns report file names, newest first. Caller holds the lock.
func (s *Store) reportFiles() []string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "report-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	// Filenames embed a fixed-width-enough unix-nano stamp; reverse lexical
	// order is newest-first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

// List returns up to limit reports, newest first.
func (s *Store) List(limit int) []Report {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := s.reportFiles()
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	out := make([]Report, 0, len(names))
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(s.dir, n))
		if err != nil {
			continue
		}
		var r Report
		if json.Unmarshal(raw, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Latest returns the newest report, or false when none has ever been stored.
// The bool is the caller's only honest way to distinguish "never measured"
// from "measured clean".
func (s *Store) Latest() (*Report, bool) {
	list := s.List(1)
	if len(list) == 0 {
		return nil, false
	}
	return &list[0], true
}
