package questions

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// document is the statefile value at key "questions".
// asOf is the clock for this mutate. It is not stored.
type document struct {
	LastNumber int          `json:"last_number,omitempty"`
	Questions  []Question   `json:"questions"`
	Hourly     []hourlyMark `json:"hourly,omitempty"`
	asOf       time.Time    `json:"-"`
}

type hourlyMark struct {
	At            time.Time `json:"at"`
	RequesterHash string    `json:"requester_hash"`
}

type store struct {
	path         string
	docLimit     int
	keep         int
	keepOverride bool
}

func (s *store) limit() int {
	if s.docLimit > 0 {
		return s.docLimit
	}
	return maxDocBytes
}

func (s *store) keepCount() int {
	if s.keepOverride {
		return s.keep
	}
	return keepTerminal
}

var (
	errNoChange = errors.New("no change")
	errTooBig   = errors.New("question store over capacity")
)

// update is the only write path. mutate is re-run from scratch when the
// statefile write conflicts. A document over the byte budget drops ordinary
// terminal records; if it is still over, the write is not stored.
func (s *store) update(mutate func(*document) error) error {
	pruned := 0
	limit := s.limit()
	keep := s.keepCount()
	run := func(old []byte) ([]byte, error) {
		doc, err := parse(old)
		if err != nil {
			return nil, err
		}
		if err := mutate(doc); err != nil {
			return nil, err
		}
		before := len(doc.Questions)
		doc.prune(keep)
		raw, err := doc.marshal()
		if err != nil {
			return nil, err
		}
		for len(raw) > limit && doc.dropOldestOrdinary() {
			raw, err = doc.marshal()
			if err != nil {
				return nil, err
			}
		}
		if len(raw) > limit {
			return nil, errTooBig
		}
		pruned = before - len(doc.Questions)
		return raw, nil
	}
	if s.path == "" {
		var doc document
		raw, err := marshalJSON(&doc)
		if err != nil {
			return err
		}
		_, err = run(raw)
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	err := statefile.Update(s.path, run)
	if errors.Is(err, errNoChange) {
		return nil
	}
	if err == nil && pruned > 0 {
		slog.Info("question business records pruned; conclusions follow retained records", "count", pruned)
	}
	return err
}

func parse(data []byte) (*document, error) {
	doc := &document{}
	if bytesTrim(data) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(data, doc); err != nil {
		return nil, err
	}
	if doc.Questions == nil {
		doc.Questions = []Question{}
	}
	return doc, nil
}

func bytesTrim(data []byte) int {
	n := len(data)
	for n > 0 && (data[n-1] == '\n' || data[n-1] == ' ' || data[n-1] == '\t') {
		n--
	}
	return n
}

func (doc *document) marshal() ([]byte, error) {
	if doc.Questions == nil {
		doc.Questions = []Question{}
	}
	return marshalJSON(doc)
}

func (doc *document) prune(keep int) {
	asOf := doc.asOf
	var protected, ordinary []Question
	for _, q := range doc.Questions {
		if q.protected(asOf) {
			protected = append(protected, q)
			continue
		}
		ordinary = append(ordinary, q)
	}
	sort.SliceStable(ordinary, func(i, j int) bool {
		return ordinary[i].terminalAt().After(ordinary[j].terminalAt())
	})
	if len(ordinary) > keep {
		ordinary = ordinary[:keep]
	}
	doc.Questions = append(protected, ordinary...)
}

func (doc *document) dropOldestOrdinary() bool {
	asOf := doc.asOf
	oldest := -1
	var at time.Time
	for i, q := range doc.Questions {
		if q.protected(asOf) {
			continue
		}
		t := q.terminalAt()
		if oldest < 0 || t.Before(at) {
			oldest = i
			at = t
		}
	}
	if oldest < 0 {
		return false
	}
	doc.Questions = append(doc.Questions[:oldest], doc.Questions[oldest+1:]...)
	return true
}

func (doc *document) find(ref string) (int, bool) {
	if isQuestionID(ref) {
		for i := range doc.Questions {
			if doc.Questions[i].ID == ref {
				return i, true
			}
		}
		return 0, false
	}
	if n, ok := parseNumber(ref); ok {
		for i := range doc.Questions {
			if doc.Questions[i].Number == n {
				return i, true
			}
		}
	}
	return 0, false
}

func (doc *document) nextNumber() int {
	n := doc.LastNumber
	for _, q := range doc.Questions {
		if q.Number > n {
			n = q.Number
		}
	}
	doc.LastNumber = n + 1
	return doc.LastNumber
}

func (s *store) readBytes() ([]byte, error) {
	return s.readBytesContext(context.Background())
}

func (s *store) readBytesContext(ctx context.Context) ([]byte, error) {
	if s.path == "" {
		return nil, nil
	}
	data, err := statefile.ReadFileContext(ctx, s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func isQuestionID(ref string) bool {
	if len(ref) != 18 || !strings.HasPrefix(ref, "q_") {
		return false
	}
	for _, c := range ref[2:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func parseNumber(ref string) (int, bool) {
	if ref == "" {
		return 0, false
	}
	if ref[0] == 'Q' || ref[0] == 'q' {
		ref = ref[1:]
	}
	if ref == "" || len(ref) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range ref {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}
