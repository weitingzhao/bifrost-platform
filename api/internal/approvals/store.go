package approvals

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// file is the on-disk document. The statefile key is the path relative to
// PLATFORM_DATA_DIR, which is "approvals" (PROD: ConfigMap platform-state-approvals).
type file struct {
	Approvals []Approval `json:"approvals"`
}

type store struct {
	path  string
	items []Approval
}

func newStore(path string) *store {
	s := &store{path: path}
	_ = s.load()
	return s
}

func (s *store) load() error {
	if s.path == "" {
		s.items = nil
		return nil
	}
	data, err := statefile.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			s.items = nil
			return nil
		}
		return err
	}
	if len(data) == 0 {
		s.items = nil
		return nil
	}
	var doc file
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	s.items = doc.Approvals
	return nil
}

func (s *store) save() error {
	if s.path == "" {
		return nil
	}
	s.prune()
	doc := file{Approvals: s.items}
	if doc.Approvals == nil {
		doc.Approvals = []Approval{}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil && !statefileActive() {
		return err
	}
	return statefile.WriteFile(s.path, raw, 0o644)
}

func statefileActive() bool {
	on, _ := statefile.Active()
	return on
}

// prune keeps every open approval and the most recent keepClosed terminal ones.
func (s *store) prune() {
	open := make([]Approval, 0)
	closed := make([]Approval, 0)
	for _, a := range s.items {
		if a.open() {
			open = append(open, a)
			continue
		}
		closed = append(closed, a)
	}
	sort.SliceStable(closed, func(i, j int) bool {
		return closed[i].closedAt().After(closed[j].closedAt())
	})
	if len(closed) > keepClosed {
		closed = closed[:keepClosed]
	}
	s.items = append(open, closed...)
}

func (s *store) put(a Approval) {
	for i := range s.items {
		if s.items[i].ID == a.ID {
			s.items[i] = a
			return
		}
	}
	s.items = append(s.items, a)
}

func (s *store) get(id string) (Approval, bool) {
	for _, a := range s.items {
		if a.ID == id {
			return a, true
		}
	}
	return Approval{}, false
}
