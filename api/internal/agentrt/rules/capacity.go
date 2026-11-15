package rules

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// CapacityChange is one capacity field a version changes: its path in the
// rule's doc (a landing repo's fields under repos[<repo>]), and its value
// before and after; nil where the version before or after does not state it.
type CapacityChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// capacityField says whether a flattened doc path is a capacity field of a
// rule of kind; nil for a kind with none.
func capacityField(kind string) func(path string) bool {
	switch kind {
	case KindTuning:
		return func(p string) bool { return strings.HasPrefix(p, "values.") }
	case KindLanding:
		return func(p string) bool {
			switch p {
			case "limits.max_files", "limits.max_changed_lines", "limits.max_landings_per_day", "limits.max_in_flight_per_repo",
				// Legacy, bounds nothing: stating, changing or dropping it
				// needs no signature (cleanup wave 1).
				"limits.max_batch":
				return true
			}
			// Repository capacities.
			return strings.HasPrefix(p, "repos[") &&
				(strings.HasSuffix(p, "].limits.max_files") || strings.HasSuffix(p, "].limits.max_changed_lines") || strings.HasSuffix(p, "].limits.max_in_flight"))
		}
	}
	return nil
}

// headerOnly are the doc keys a version always or may change whatever it
// says: its number and the direction it declares.
var headerOnly = []string{"rule_version", "direction"}

// flatten writes v's leaves into out by path: an object's keys joined with
// ".", a list of landing repos by each repo's name, any other list whole.
func flatten(prefix string, v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, e, out)
		}
	case []any:
		if prefix == "repos" {
			byRepo := map[string]any{}
			for _, e := range t {
				row, ok := e.(map[string]any)
				name, named := row["repo"].(string)
				if !ok || !named || byRepo[name] != nil {
					out[prefix] = v
					return
				}
				byRepo[name] = row
			}
			for name, row := range byRepo {
				flatten("repos["+name+"]", row, out)
			}
			return
		}
		out[prefix] = v
	default:
		out[prefix] = v
	}
}

// CapacityChanges lists the capacity fields next changes from prev, sorted by
// field, and says which other fields it changes (besides the version and the
// direction), also sorted: those are what keep it off a card.
func CapacityChanges(prev, next Rule) (changes []CapacityChange, others []string, err error) {
	if prev.ID != next.ID || prev.Kind != next.Kind {
		return nil, nil, errors.New("a different rule")
	}
	capacity := capacityField(next.Kind)
	if capacity == nil {
		return nil, nil, fmt.Errorf("a %s rule has no capacity fields: it needs the Owner's signature", next.Kind)
	}
	a, b := map[string]any{}, map[string]any{}
	flatten("", prev.Doc(), a)
	flatten("", next.Doc(), b)
	paths := map[string]bool{}
	for p := range a {
		paths[p] = true
	}
	for p := range b {
		paths[p] = true
	}
	for p := range paths {
		if slices.Contains(headerOnly, p) {
			continue
		}
		o, inA := a[p]
		n, inB := b[p]
		same := inA == inB
		if same && inA {
			if same, err = equalJSON(o, n); err != nil {
				return nil, nil, err
			}
		}
		switch {
		case same:
		case capacity(p):
			changes = append(changes, CapacityChange{Field: p, Old: o, New: n})
		default:
			others = append(others, p)
		}
	}
	slices.SortFunc(changes, func(x, y CapacityChange) int { return strings.Compare(x.Field, y.Field) })
	slices.Sort(others)
	return changes, others, nil
}

func equalJSON(a, b any) (bool, error) {
	x, err := canon.Encode(a)
	if err != nil {
		return false, err
	}
	y, err := canon.Encode(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(x, y), nil
}

// CapacityOnly says whether next changes only capacity fields of prev (Q5 A),
// so may take effect on a card the Owner approves instead of his signature:
// the same rule and kind, the two docs equal once the capacity fields, the
// version and the direction are removed (so the same validity), at least one
// capacity field changed, and every changed value within its ceiling. It
// never trusts what next declares; it compares the two. nil means it may go
// on a card.
func CapacityOnly(prev, next Rule) error {
	if next.Direction == Initial {
		return errors.New("an initial rule is signed")
	}
	changes, others, err := CapacityChanges(prev, next)
	if err != nil {
		return err
	}
	if len(others) > 0 {
		return fmt.Errorf("it changes %s, which only the Owner's signature may change", strings.Join(others, ", "))
	}
	if len(changes) == 0 {
		return errors.New("it changes no capacity field")
	}
	for _, c := range changes {
		if c.New == nil {
			continue
		}
		n, ok := c.New.(int)
		if !ok {
			// Non-integer fields are bounded by their parser.
			continue
		}
		if next.Kind == KindLanding && (c.Field == "limits.max_in_flight_per_repo" || strings.HasSuffix(c.Field, "].limits.max_in_flight")) && (n < 1 || n > LandingInFlightCeiling) {
			return fmt.Errorf("%s must be between 1 and %d", c.Field, LandingInFlightCeiling)
		}
		if n < 0 {
			return fmt.Errorf("%s is %d, below zero", c.Field, n)
		}
	}
	return nil
}
