package driftproposal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore() *Store {
	dir := os.Getenv("PLATFORM_DRIFT_PROPOSALS_DIR")
	if dir == "" {
		if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
			dir = filepath.Join(root, "agent", "drift-proposals")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "drift-proposals")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return &Store{dir: dir}
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) Put(p Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(p.ID), raw, 0o644)
}

func (s *Store) Get(id string) (*Proposal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, false
	}
	var p Proposal
	if json.Unmarshal(raw, &p) != nil {
		return nil, false
	}
	return &p, true
}

func (s *Store) List() []Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]Proposal, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var p Proposal
		if json.Unmarshal(raw, &p) == nil {
			out = append(out, p)
		}
	}
	sortProposals(out)
	return out
}

func sortProposals(list []Proposal) {
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].CreatedAt.After(list[i].CreatedAt) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
}

func newProposalID() string {
	return fmt.Sprintf("drift-%d", time.Now().UTC().UnixNano())
}
