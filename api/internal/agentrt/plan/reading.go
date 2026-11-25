package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The reading atom (2026-11-25, the map line asked the plan: the map's
// criteria are numbers the platform already keeps, and no atom could read
// one). A step is done when a number or a flag in a kept document compares
// true with a threshold:
//
//	{"reading": {"source": "catalog.status", "path": "inventory.*.gap", "op": "==", "value": 0}}
//
// source names a document the platform keeps and serves read-only (the
// sources the store wires, ReadingFacts); path walks it by object keys, with
// * for every element of a list. Every value the path reaches must compare
// true, and it must reach at least one: a path that reaches nothing, a value
// that is not a number (or a flag for == and !=) and a source that cannot be
// read are all Unknown, never done (LAW-8: only numbers the system produces
// count; what is not there is not measured).

// ReadingFacts is optional, like StepFacts: a facts reader without it reads
// every reading Unknown, so it can never make a step done.
type ReadingFacts interface {
	// ReadingDoc is the source's document as the platform keeps it now;
	// ok is false when it cannot be read.
	ReadingDoc(source string) (doc []byte, ok bool)
}

type reading struct {
	source string
	path   []string
	op     string
	num    float64
	flag   *bool
}

var (
	readingSource = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)*$`)
	readingKey    = regexp.MustCompile(`^(\*|[A-Za-z0-9_-]+)$`)
	readingOps    = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}
)

func parseReading(v any) (*node, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 4 {
		return nil, fmt.Errorf("%w: reading needs source, path, op and value", ErrRule)
	}
	src, ok1 := m["source"].(string)
	path, ok2 := m["path"].(string)
	op, ok3 := m["op"].(string)
	if !ok1 || !ok2 || !ok3 || !readingSource.MatchString(src) || !readingOps[op] {
		return nil, fmt.Errorf("%w: reading needs a source name, a path and op one of == != < <= > >=", ErrRule)
	}
	keys := strings.Split(path, ".")
	for _, k := range keys {
		if !readingKey.MatchString(k) {
			return nil, fmt.Errorf("%w: reading path %q: keys joined by dots, * for every element", ErrRule, path)
		}
	}
	r := &reading{source: src, path: keys, op: op}
	switch val := m["value"].(type) {
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return nil, fmt.Errorf("%w: reading value %q", ErrRule, val)
		}
		r.num = f
	case bool:
		if op != "==" && op != "!=" {
			return nil, fmt.Errorf("%w: a flag compares with == or != only", ErrRule)
		}
		b := val
		r.flag = &b
	default:
		return nil, fmt.Errorf("%w: reading value is a number or a flag", ErrRule)
	}
	return &node{kind: kReading, s1: src, reading: r}, nil
}

func evalReading(r *reading, f Facts) Tri {
	rf, ok := f.(ReadingFacts)
	if !ok {
		return Unknown
	}
	raw, ok := rf.ReadingDoc(r.source)
	if !ok || len(raw) == 0 {
		return Unknown
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var doc any
	if err := d.Decode(&doc); err != nil {
		return Unknown
	}
	values := walkReading(doc, r.path)
	if len(values) == 0 {
		return Unknown
	}
	out := True
	for _, v := range values {
		switch r.compare(v) {
		case False:
			out = False
		case Unknown:
			return Unknown
		}
	}
	return out
}

func walkReading(v any, path []string) []any {
	if len(path) == 0 {
		return []any{v}
	}
	var out []any
	if path[0] == "*" {
		list, ok := v.([]any)
		if !ok {
			return nil
		}
		for _, e := range list {
			out = append(out, walkReading(e, path[1:])...)
		}
		return out
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	next, ok := obj[path[0]]
	if !ok {
		return nil
	}
	return walkReading(next, path[1:])
}

func (r *reading) compare(v any) Tri {
	if r.flag != nil {
		b, ok := v.(bool)
		if !ok {
			return Unknown
		}
		eq := b == *r.flag
		if r.op == "!=" {
			eq = !eq
		}
		return triOf(eq)
	}
	n, ok := v.(json.Number)
	if !ok {
		return Unknown
	}
	x, err := n.Float64()
	if err != nil {
		return Unknown
	}
	switch r.op {
	case "==":
		return triOf(x == r.num)
	case "!=":
		return triOf(x != r.num)
	case "<":
		return triOf(x < r.num)
	case "<=":
		return triOf(x <= r.num)
	case ">":
		return triOf(x > r.num)
	}
	return triOf(x >= r.num)
}

func triOf(b bool) Tri {
	if b {
		return True
	}
	return False
}

// ReadingDoc reads each source once per evaluation, so every reading of one
// rule sees the same document.
func (o *onceFacts) ReadingDoc(source string) ([]byte, bool) {
	if o.docs == nil {
		o.docs = map[string]docRead{}
	}
	if d, ok := o.docs[source]; ok {
		return d.raw, d.ok
	}
	var d docRead
	if f, ok := o.Facts.(ReadingFacts); ok {
		d.raw, d.ok = f.ReadingDoc(source)
	}
	o.docs[source] = d
	return d.raw, d.ok
}

type docRead struct {
	raw []byte
	ok  bool
}

// Readings lists the sources the rule reads, so the store can refuse a rule
// that names a source it does not serve.
func (r Rule) Readings() []string {
	var out []string
	seen := map[string]bool{}
	var walk func(*node)
	walk = func(n *node) {
		if n.kind == kReading && !seen[n.s1] {
			seen[n.s1] = true
			out = append(out, n.s1)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if r.node != nil {
		walk(r.node)
	}
	return out
}
