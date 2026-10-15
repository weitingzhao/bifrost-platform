package rules

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// The closed reader under the parser: one YAML document of mappings,
// sequences and plain or quoted scalars, nothing else. An anchor, an alias, a
// merge key, an explicit tag, a second document or a duplicate key is an
// error, as is a value whose resolved type is not the one the field takes:
// the text the Owner signed means one thing.

// MaxText is the largest rule text read.
const MaxText = 64 << 10

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// document reads text into its one top-level mapping.
func document(text []byte) (*object, error) {
	if len(text) > MaxText {
		return nil, fmt.Errorf("the rule is %d bytes; at most %d", len(text), MaxText)
	}
	dec := yaml.NewDecoder(bytes.NewReader(text))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the rule is empty")
		}
		return nil, fmt.Errorf("not YAML: %w", err)
	}
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return nil, errors.New("the rule is one YAML document; this text has more")
	}
	if len(doc.Content) != 1 {
		return nil, errors.New("the rule is one mapping")
	}
	return mapping(doc.Content[0], "the rule")
}

// plain refuses what the closed reader never takes from a node.
func plain(n *yaml.Node, where string) error {
	switch {
	case n.Kind == yaml.AliasNode:
		return fmt.Errorf("%s: aliases are not allowed", where)
	case n.Anchor != "":
		return fmt.Errorf("%s: anchors are not allowed", where)
	case n.Style&yaml.TaggedStyle != 0:
		return fmt.Errorf("%s: explicit tags are not allowed", where)
	}
	return nil
}

// object is a mapping read so far: each field is taken once, and done says
// whether any was left over.
type object struct {
	where  string
	fields map[string]*yaml.Node
	order  []string
	taken  map[string]bool
}

func mapping(n *yaml.Node, where string) (*object, error) {
	if err := plain(n, where); err != nil {
		return nil, err
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is a mapping", where)
	}
	o := &object{where: where, fields: map[string]*yaml.Node{}, taken: map[string]bool{}}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if err := plain(k, where); err != nil {
			return nil, err
		}
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			if k.Tag == "!!merge" {
				return nil, fmt.Errorf("%s: merge keys are not allowed", where)
			}
			return nil, fmt.Errorf("%s: every key is a plain string", where)
		}
		if _, dup := o.fields[k.Value]; dup {
			return nil, fmt.Errorf("%s: %q is given twice", where, k.Value)
		}
		o.fields[k.Value] = v
		o.order = append(o.order, k.Value)
	}
	return o, nil
}

func (o *object) node(key string, optional bool) (*yaml.Node, error) {
	n, ok := o.fields[key]
	if !ok {
		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("%s needs %q", o.where, key)
	}
	o.taken[key] = true
	if err := plain(n, o.where+"."+key); err != nil {
		return nil, err
	}
	return n, nil
}

// done refuses the fields nobody took.
func (o *object) done() error {
	for _, k := range o.order {
		if !o.taken[k] {
			return fmt.Errorf("%s: %q is not a field of it", o.where, k)
		}
	}
	return nil
}

func scalar(n *yaml.Node, where, tag string) (string, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != tag {
		return "", fmt.Errorf("%s is %s", where, map[string]string{"!!str": "a string", "!!int": "a whole number",
			"!!float": "a number", "!!timestamp": "a time"}[tag])
	}
	return n.Value, nil
}

func (o *object) str(key string, optional bool) (string, error) {
	n, err := o.node(key, optional)
	if n == nil || err != nil {
		return "", err
	}
	return scalar(n, o.where+"."+key, "!!str")
}

func (o *object) integer(key string, lo, hi int) (int, error) {
	n, err := o.node(key, false)
	if err != nil {
		return 0, err
	}
	s, err := scalar(n, o.where+"."+key, "!!int")
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(s)
	if !decimal.MatchString(s) || err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("%s.%s is a whole number from %d to %d, written in decimal", o.where, key, lo, hi)
	}
	return v, nil
}

// flag reads a string or true / false (written so, nothing else).
func (o *object) flag(key string) (any, error) {
	n, err := o.node(key, false)
	if err != nil {
		return nil, err
	}
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag == "!!str":
		return n.Value, nil
	case n.Kind == yaml.ScalarNode && n.Tag == "!!bool" && (n.Value == "true" || n.Value == "false"):
		return n.Value == "true", nil
	}
	return nil, fmt.Errorf("%s.%s is a string, true or false", o.where, key)
}

// at reads a time written as RFC 3339 with its offset (2026-10-15T00:00:00Z).
func (o *object) at(key string) (time.Time, error) {
	n, err := o.node(key, false)
	if err != nil {
		return time.Time{}, err
	}
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!timestamp" && n.Tag != "!!str") {
		return time.Time{}, fmt.Errorf("%s.%s is a time", o.where, key)
	}
	t, err := time.Parse(time.RFC3339, n.Value)
	if err != nil || t.Nanosecond() != 0 {
		return time.Time{}, fmt.Errorf("%s.%s is a time in whole seconds like 2026-10-15T00:00:00Z", o.where, key)
	}
	return t.UTC(), nil
}

func (o *object) object(key string) (*object, error) {
	n, err := o.node(key, false)
	if err != nil {
		return nil, err
	}
	return mapping(n, o.where+"."+key)
}

// list reads a sequence; each item goes to f.
func (o *object) list(key string, optional bool, f func(n *yaml.Node, where string) error) error {
	n, err := o.node(key, optional)
	if n == nil || err != nil {
		return err
	}
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s.%s is a list", o.where, key)
	}
	for i, item := range n.Content {
		where := fmt.Sprintf("%s.%s[%d]", o.where, key, i)
		if err := plain(item, where); err != nil {
			return err
		}
		if err := f(item, where); err != nil {
			return err
		}
	}
	return nil
}

// strings reads a list of strings, each once.
func (o *object) strings(key string, optional bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	err := o.list(key, optional, func(n *yaml.Node, where string) error {
		s, err := scalar(n, where, "!!str")
		if err != nil {
			return err
		}
		if s == "" || seen[s] {
			return fmt.Errorf("%s: %q is empty or listed twice", where, s)
		}
		seen[s] = true
		out = append(out, s)
		return nil
	})
	return out, err
}
