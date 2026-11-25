package registry

import (
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The ledger's vocabulary for the types whose body is written in the
// register (ISSUE-34). The register is compiled in and parsed at start; a
// type with a body here is declared to the ledger here, so a new topic is a
// register entry, not Go. The tests hold that no type is declared both here
// and in Go (eventlog panics on a second declaration).

var kindOf = map[string]eventlog.Kind{"str": eventlog.Str, "id": eventlog.ID, "int": eventlog.Int, "bool": eventlog.Bool, "any": eventlog.Any}

// Vocabulary is the ledger vocabulary of a channel's registered body; ok is
// false for a channel with no body here.
func Vocabulary(c Channel) (eventlog.Vocabulary, bool) {
	if len(c.Body) == 0 {
		return eventlog.Vocabulary{}, false
	}
	fields := make([]eventlog.Field, 0, len(c.Body))
	values := map[string]eventlog.Value{}
	for _, f := range c.Body {
		fields = append(fields, eventlog.Field{Name: f.Name, Kind: kindOf[f.Kind], Optional: f.Optional})
		if f.Pattern != "" {
			values[f.Name] = eventlog.Shaped(f.Name+" of "+c.Type, regexp.MustCompile(f.Pattern))
		}
	}
	v := eventlog.Vocabulary{Bodies: map[string][]eventlog.Field{c.Type: fields}}
	if len(values) > 0 {
		v.Values = map[string]map[string]eventlog.Value{c.Type: values}
	}
	hasRef := false
	for _, f := range c.Body {
		hasRef = hasRef || f.Name == "ref"
	}
	ref, subjectFrom, changeFrom, typ := c.Ref, c.SubjectFrom, c.ChangeFrom, c.Type
	if subjectFrom != "" || len(changeFrom) > 0 || hasRef {
		v.After = map[string]func(map[string]any) error{typ: func(b map[string]any) error {
			if subjectFrom != "" && b["subject"] != b[subjectFrom] {
				return eventlog.Invalid("%s: subject is the %s field", typ, subjectFrom)
			}
			if hasRef && b["ref"] != ref {
				return eventlog.Invalid("%s: ref is %s", typ, ref)
			}
			if len(changeFrom) > 0 {
				parts := make([]string, len(changeFrom))
				for i, n := range changeFrom {
					s, _ := b[n].(string)
					parts[i] = s
				}
				if b["change"] != strings.Join(parts, "-") {
					return eventlog.Invalid("%s: change is %s joined by -", typ, strings.Join(changeFrom, ", "))
				}
			}
			return nil
		}}
	}
	return v, true
}

func init() {
	all, err := Load()
	if err != nil {
		// The register's own tests refuse a broken file; a process built from
		// one declares nothing here and refuses those events at Append.
		return
	}
	for _, t := range all {
		for _, c := range t.Channels {
			if v, ok := Vocabulary(c); ok {
				eventlog.Declare(v)
			}
		}
	}
}
