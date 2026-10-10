package approvals

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// file is the on-disk document. The statefile key is the path relative to
// PLATFORM_DATA_DIR, which is "approvals" (PROD: ConfigMap platform-state-approvals).
type file struct {
	// LastNumber is the #n counter. It survives pruning and only grows.
	LastNumber int        `json:"last_number,omitempty"`
	Approvals  []Approval `json:"approvals"`
}

// maxDocBytes stays under the ConfigMap budget (k8sstate.MaxBytes, 900 KiB):
// past it the oldest terminal records are dropped rather than failing a write.
const maxDocBytes = 800 * 1024

type store struct {
	path string
}

func newStore(path string) *store {
	return &store{path: path}
}

func (s *store) read() (file, error) {
	if s.path == "" {
		return file{}, nil
	}
	data, err := statefile.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return file{}, nil
		}
		return file{}, err
	}
	return parse(data)
}

func parse(data []byte) (file, error) {
	var doc file
	if len(data) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return file{}, err
	}
	return doc, nil
}

// errNoChange makes update skip the write.
var errNoChange = errors.New("no change")

// update is the only write path. statefile.Update re-runs mutate on the newer
// document when the ConfigMap write conflicts (two platform-api pods during a
// rollout), so a counter bump or a status transition decided in mutate holds.
func (s *store) update(mutate func(doc *file) error) error {
	if s.path == "" {
		var doc file
		err := mutate(&doc)
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	err := statefile.Update(s.path, func(old []byte) ([]byte, error) {
		doc, err := parse(old)
		if err != nil {
			return nil, err
		}
		if err := mutate(&doc); err != nil {
			return nil, err
		}
		doc.prune()
		return doc.marshal()
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	return err
}

func (doc *file) marshal() ([]byte, error) {
	if doc.Approvals == nil {
		doc.Approvals = []Approval{}
	}
	for {
		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		if len(raw) <= maxDocBytes || !doc.dropOldestClosed() {
			return raw, nil
		}
	}
}

// prune keeps every open approval and the most recent keepClosed terminal
// ones; only the newest keepTails terminal ones keep their output tail.
func (doc *file) prune() {
	open := make([]Approval, 0)
	closed := make([]Approval, 0)
	for _, a := range doc.Approvals {
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
	for i := keepTails; i < len(closed); i++ {
		if e := closed[i].Execution; e != nil && e.OutputTail != "" {
			c := *e
			c.OutputTail = ""
			closed[i].Execution = &c
		}
	}
	doc.Approvals = append(open, closed...)
}

// dropOldestClosed removes the last terminal record (prune sorted them newest
// first, after the open ones). false when none is left.
func (doc *file) dropOldestClosed() bool {
	for i := len(doc.Approvals) - 1; i >= 0; i-- {
		if !doc.Approvals[i].open() {
			doc.Approvals = append(doc.Approvals[:i], doc.Approvals[i+1:]...)
			return true
		}
	}
	return false
}

func (doc *file) put(a Approval) {
	for i := range doc.Approvals {
		if doc.Approvals[i].ID == a.ID {
			doc.Approvals[i] = a
			return
		}
	}
	doc.Approvals = append(doc.Approvals, a)
}

// find resolves an approval id or a number ("57" or "#57").
func (doc *file) find(ref string) (Approval, bool) {
	if n, ok := parseNumber(ref); ok {
		for _, a := range doc.Approvals {
			if a.Number == n {
				return a, true
			}
		}
		return Approval{}, false
	}
	for _, a := range doc.Approvals {
		if a.ID == ref {
			return a, true
		}
	}
	return Approval{}, false
}

func (doc *file) nextNumber() int {
	n := doc.LastNumber
	for _, a := range doc.Approvals {
		if a.Number > n {
			n = a.Number
		}
	}
	doc.LastNumber = n + 1
	return doc.LastNumber
}
