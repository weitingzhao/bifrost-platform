// Package opsfacts projects authoritative reads without storing a second ledger.
package opsfacts

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

type State string

const (
	OK        State = "ok"
	Empty     State = "empty"
	Unknown   State = "unknown"
	Error     State = "error"
	Forbidden State = "forbidden"
	Stale     State = "stale"
)

type Source struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Coverage struct {
	Window    *Window `json:"window"`
	Complete  *bool   `json:"complete"`
	Paginated *bool   `json:"paginated"`
	Truncated *bool   `json:"truncated"`
	Limit     *int    `json:"limit"`
	Reason    string  `json:"reason,omitempty"`
}

type Fact[T any] struct {
	AsOf          time.Time  `json:"as_of"`
	Source        Source     `json:"source"`
	Watermark     *time.Time `json:"watermark"`
	Coverage      Coverage   `json:"coverage"`
	State         State      `json:"state"`
	Reason        string     `json:"reason,omitempty"`
	MergedSources []Source   `json:"merged_sources"`
	Value         *T         `json:"value"`
}

// ReadResult contains transport status, never a user-visible error message.
// A zero status means no measurement was attempted. Err is intentionally opaque.
type ReadResult struct {
	Status int
	Body   json.RawMessage
	Err    error
}
type Reader interface {
	Read(context.Context, Source) ReadResult
}

func Read[T any](ctx context.Context, reader Reader, source Source, now func() time.Time) Fact[T] {
	result := reader.Read(ctx, source)
	f := Fact[T]{AsOf: now().UTC(), Source: source, State: Unknown, MergedSources: []Source{}}
	switch {
	case result.Status == 401 || result.Status == 403:
		f.State, f.Reason = Forbidden, "source access denied"
	case result.Err != nil:
		f.State, f.Reason = Error, "source read failed"
	case result.Status == 0:
		f.Reason = "source not measured"
	case result.Status < 200 || result.Status >= 300:
		f.State, f.Reason = Error, "source returned an unsuccessful status"
	default:
		var value *T
		if json.Unmarshal(result.Body, &value) != nil || value == nil {
			f.State, f.Reason = Error, "source returned an invalid or empty document"
		} else {
			f.State, f.Value = OK, value
		}
	}
	return f
}

// Freshness uses the source observation timestamp, not a lease expiry, alert
// onset, or query evaluation time. Missing timestamps are not made up.
func (f *Fact[T]) Freshness(watermark *time.Time, maxAge time.Duration) {
	f.Watermark = watermark
	if f.State != OK && f.State != Empty {
		return
	}
	if watermark == nil {
		return
	}
	if watermark.After(f.AsOf.Add(time.Minute)) {
		f.State, f.Reason = Unknown, "source timestamp is in the future"
		return
	}
	if maxAge > 0 && f.AsOf.Sub(*watermark) > maxAge {
		f.State, f.Reason = Stale, "source freshness threshold exceeded"
	}
}

// Candidate priorities are explicit per projection; lower numbers win. State
// never changes precedence: a failed authoritative read cannot be hidden by a
// less authoritative success. Equal priorities retain input order.
type Candidate[T any] struct {
	Key      string
	Priority int
	Fact     Fact[T]
}

func Deduplicate[T any](candidates []Candidate[T]) []Candidate[T] {
	ordered := append([]Candidate[T](nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	out := []Candidate[T]{}
	seen := map[string]int{}
	for _, c := range ordered {
		if i, ok := seen[c.Key]; ok {
			out[i].Fact.MergedSources = append(out[i].Fact.MergedSources, c.Fact.Source)
			out[i].Fact.MergedSources = append(out[i].Fact.MergedSources, c.Fact.MergedSources...)
		} else {
			seen[c.Key] = len(out)
			c.Fact.MergedSources = append([]Source{}, c.Fact.MergedSources...)
			out = append(out, c)
		}
	}
	return out
}

func snapshotCoverage() Coverage {
	yes, no := true, false
	return Coverage{Complete: &yes, Paginated: &no, Truncated: &no}
}
func invalid[T any](f *Fact[T]) {
	f.State, f.Reason, f.Value = Error, "source document is missing required fields or contains invalid values", nil
}
func timestamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
