package eventlog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"
)

// The vocabulary: the closed set of event types and what each may carry.
// This package holds the rules of checking, and no type: each domain
// declares its own (Declare, from its init), and Append asks the sum. A
// domain package can so bring its events with it when it leaves the store.

// Kind is a body field's kind. Values are what canon.Parse or a Go caller
// produce.
type Kind int

const (
	Str  Kind = iota // a non-empty string
	ID               // a non-empty string naming a row (some of a fixed shape: Values)
	Int              // an integer
	IDs              // a non-empty list of row ids
	Any              // any canonical JSON value
	Bool             // true or false
	Ints             // a non-empty list of integers (row ids of an identity column)
)

// Field is one field of an event type's body.
type Field struct {
	Name     string
	Kind     Kind
	Optional bool
}

// Value checks one string of a field; it returns "" or why it is refused.
type Value func(s string) string

// Vocabulary is what one domain declares.
type Vocabulary struct {
	// Types are the domain's event types, with or without a body: the
	// database's event_type_closed constraint lists them all.
	Types []string
	// Bodies is the closed body of each type the domain writes: every
	// required field present with a value of its kind, no other field. A
	// type with no body is refused rather than written with a guessed shape.
	Bodies map[string][]Field
	// Values are the checked fields, by event type and field: what a field
	// may hold beyond its kind. A list is checked element by element.
	Values map[string]map[string]Value
	// WholeValues check a field's value as it is, whatever its kind; one
	// named here is not asked of Values.
	WholeValues map[string]map[string]func(v any) string
	// Before and After are a type's own checks of its body, run before and
	// after its fields are checked.
	Before, After map[string]func(body map[string]any) error
	// Events are asked of every event, of any type, before its body.
	Events []func(Event) error
}

var (
	vocabularyMu sync.RWMutex
	declared     bool
	types        = map[string]bool{}
	bodies       = map[string][]Field{}
	values       = map[string]map[string]Value{}
	wholeValues  = map[string]map[string]func(any) string{}
	before       = map[string]func(map[string]any) error{}
	after        = map[string]func(map[string]any) error{}
	events       []func(Event) error
)

// Declare adds a domain's vocabulary. It is called from the init of the
// package that owns the types. Two declarations of one type's body, values or
// checks are a programming error: the process does not start.
func Declare(v Vocabulary) {
	vocabularyMu.Lock()
	defer vocabularyMu.Unlock()
	declared = true
	for _, t := range v.Types {
		types[t] = true
	}
	for t, spec := range v.Bodies {
		if _, twice := bodies[t]; twice {
			panic("eventlog: the body of " + t + " is declared twice")
		}
		bodies[t] = spec
		types[t] = true
	}
	for t, fields := range v.Values {
		if _, twice := values[t]; twice {
			panic("eventlog: the values of " + t + " are declared twice")
		}
		values[t] = fields
	}
	for t, fields := range v.WholeValues {
		if _, twice := wholeValues[t]; twice {
			panic("eventlog: the whole values of " + t + " are declared twice")
		}
		wholeValues[t] = fields
	}
	for t, f := range v.Before {
		if _, twice := before[t]; twice {
			panic("eventlog: the check before " + t + " is declared twice")
		}
		before[t] = f
	}
	for t, f := range v.After {
		if _, twice := after[t]; twice {
			panic("eventlog: the check after " + t + " is declared twice")
		}
		after[t] = f
	}
	events = append(events, v.Events...)
}

// IsEventType says whether t is an event type of the closed vocabulary.
func IsEventType(t string) bool {
	vocabularyMu.RLock()
	defer vocabularyMu.RUnlock()
	return types[t]
}

// EventTypes is the closed vocabulary and, of it, the types with a body.
func EventTypes() (vocabulary, written []string) {
	vocabularyMu.RLock()
	defer vocabularyMu.RUnlock()
	for t := range types {
		vocabulary = append(vocabulary, t)
	}
	for t := range bodies {
		written = append(written, t)
	}
	slices.Sort(vocabulary)
	slices.Sort(written)
	return vocabulary, written
}

// BodyOf is the declared body of an event type, and whether it has one.
func BodyOf(eventType string) ([]Field, bool) {
	vocabularyMu.RLock()
	defer vocabularyMu.RUnlock()
	spec, ok := bodies[eventType]
	return spec, ok
}

// CheckedFields are the fields of each event type that have a Value check.
func CheckedFields() map[string][]string {
	vocabularyMu.RLock()
	defer vocabularyMu.RUnlock()
	out := map[string][]string{}
	for t, fields := range values {
		for name := range fields {
			out[t] = append(out[t], name)
		}
		slices.Sort(out[t])
	}
	return out
}

// Invalid is an ErrInvalid with its reason.
func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// checkEvent is what Append asks before it writes: the checks every event
// passes, then its body.
func checkEvent(ev Event) error {
	vocabularyMu.RLock()
	if !declared {
		vocabularyMu.RUnlock()
		return fmt.Errorf("eventlog: no vocabulary is declared; nothing is appended unchecked")
	}
	all := events
	vocabularyMu.RUnlock()
	for _, check := range all {
		if err := check(ev); err != nil {
			return err
		}
	}
	return CheckBody(ev.Type, ev.Body)
}

// CheckBody holds body to the declared body of eventType.
func CheckBody(eventType string, body map[string]any) error {
	vocabularyMu.RLock()
	pre, post := before[eventType], after[eventType]
	spec, ok := bodies[eventType]
	vocabularyMu.RUnlock()
	if pre != nil {
		if err := pre(body); err != nil {
			return err
		}
	}
	if !ok {
		return fmt.Errorf("store: event type %q has no body defined yet", eventType)
	}
	allowed := map[string]bool{}
	for _, f := range spec {
		allowed[f.Name] = true
		v, present := body[f.Name]
		if !present {
			if f.Optional {
				continue
			}
			return fmt.Errorf("store: %s needs %q", eventType, f.Name)
		}
		if !OfKind(v, f.Kind) {
			return fmt.Errorf("store: %s field %q has the wrong kind of value", eventType, f.Name)
		}
		if why := BadValue(eventType, f.Name, v); why != "" {
			return Invalid("%s field %q %s", eventType, f.Name, why)
		}
	}
	var extra []string
	for k := range body {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("store: %s does not take %v", eventType, extra)
	}
	if post != nil {
		return post(body)
	}
	return nil
}

// OfKind says whether v is a value of kind k.
func OfKind(v any, k Kind) bool {
	switch k {
	case Str, ID:
		s, ok := v.(string)
		return ok && s != ""
	case Int:
		switch n := v.(type) {
		case int, int32, int64, uint32:
			return true
		case json.Number:
			_, err := n.Int64()
			return err == nil
		}
		return false
	case IDs:
		switch l := v.(type) {
		case []any:
			if len(l) == 0 {
				return false
			}
			for _, e := range l {
				if s, ok := e.(string); !ok || s == "" {
					return false
				}
			}
			return true
		case []string:
			if len(l) == 0 {
				return false
			}
			for _, s := range l {
				if s == "" {
					return false
				}
			}
			return true
		}
		return false
	case Bool:
		_, ok := v.(bool)
		return ok
	case Ints:
		switch l := v.(type) {
		case []any:
			if len(l) == 0 {
				return false
			}
			for _, e := range l {
				if !OfKind(e, Int) {
					return false
				}
			}
			return true
		case []int64:
			return len(l) > 0
		}
		return false
	}
	return true
}

// BadValue is why v, of its field's kind already, is refused, or "". A list
// is checked element by element.
func BadValue(eventType, name string, v any) string {
	vocabularyMu.RLock()
	whole := wholeValues[eventType][name]
	check, ok := values[eventType][name]
	vocabularyMu.RUnlock()
	if whole != nil {
		return whole(v)
	}
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return check(t)
	case []string:
		for _, s := range t {
			if why := check(s); why != "" {
				return why
			}
		}
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				if why := check(s); why != "" {
					return why
				}
			}
		}
	}
	return ""
}

// OneOf is an enum.
func OneOf(vals ...string) Value {
	return func(s string) string {
		if slices.Contains(vals, s) {
			return ""
		}
		return fmt.Sprintf("is %q, not one of %v", s, vals)
	}
}

// Shaped is an id or a reference of a fixed shape.
func Shaped(name string, re *regexp.Regexp) Value {
	return func(s string) string {
		if re.MatchString(s) {
			return ""
		}
		return fmt.Sprintf("is %q, not %s", s, name)
	}
}

// Instant is an RFC 3339 instant.
func Instant(s string) string {
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return fmt.Sprintf("is %q, not an RFC 3339 instant", s)
	}
	return ""
}
