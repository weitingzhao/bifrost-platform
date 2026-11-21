package registry

import (
	"regexp"
	"slices"
)

// Kinds a registered body field may have.
var bodyKinds = map[string]bool{"str": true, "id": true, "int": true, "bool": true, "any": true}

var fieldRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// checkBody holds a channel's registered body to its own rules: known kinds,
// patterns that compile, no field twice, and subject_from and change_from
// naming fields of the body (which then carries subject and change).
func checkBody(c Channel, where func(string, ...any)) {
	if len(c.Body) == 0 {
		if c.SubjectFrom != "" || len(c.ChangeFrom) > 0 {
			where("subject_from or change_from without a body")
		}
		return
	}
	seen := map[string]bool{}
	for _, f := range c.Body {
		if !fieldRE.MatchString(f.Name) {
			where("body field %q is not a lower-case name", f.Name)
		}
		if seen[f.Name] {
			where("body field %q is listed twice", f.Name)
		}
		seen[f.Name] = true
		if !bodyKinds[f.Kind] {
			where("body field %q: kind %q is not str, id, int, bool or any", f.Name, f.Kind)
		}
		if f.Pattern != "" {
			if f.Kind != "str" && f.Kind != "id" {
				where("body field %q: a pattern is only for str or id", f.Name)
			}
			if _, err := regexp.Compile(f.Pattern); err != nil {
				where("body field %q: pattern: %v", f.Name, err)
			}
		}
	}
	if c.SubjectFrom != "" && (!seen[c.SubjectFrom] || !seen["subject"]) {
		where("subject_from %q needs that field and a subject field in the body", c.SubjectFrom)
	}
	for _, n := range c.ChangeFrom {
		if !seen[n] {
			where("change_from names %q, which is not in the body", n)
		}
	}
	if len(c.ChangeFrom) > 0 && !seen["change"] {
		where("change_from needs a change field in the body")
	}
	if seen["ref"] && slices.ContainsFunc(c.Body, func(f BodyField) bool { return f.Name == "ref" && f.Optional }) {
		where("the body's ref is the register's ref and is never optional")
	}
}
