// Package hostops defines the S1 trust and authorization protocol shared by
// the platform, executor and helper. It performs no host I/O or execution.
package hostops

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const MaxDocument = 1 << 20

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func Digest(b []byte) string    { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
func validDigest(s string) bool { return digestPattern.MatchString(s) }

// Decode accepts a closed YAML (including JSON) schema. Every field without
// omitempty is required, including zero-valued limits and empty collections.
// There is no coercion of strings, timestamps, booleans or numbers.
func Decode(raw []byte, dst any) error {
	if len(raw) == 0 || len(raw) > MaxDocument {
		return errors.New("invalid document size")
	}
	d := yaml.NewDecoder(bytes.NewReader(raw))
	var n, extra yaml.Node
	if err := d.Decode(&n); err != nil {
		return err
	}
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("exactly one document required")
	}
	if len(n.Content) != 1 {
		return errors.New("empty document")
	}
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer {
		return errors.New("destination must be a pointer")
	}
	v, err := closed(n.Content[0], t.Elem())
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func closed(n *yaml.Node, t reflect.Type) (any, error) {
	if n.Anchor != "" || n.Kind == yaml.AliasNode || n.Style&yaml.TaggedStyle != 0 {
		return nil, errors.New("anchors, aliases and explicit tags forbidden")
	}
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		if n.Kind != yaml.MappingNode {
			return nil, errors.New("mapping required")
		}
		fields := map[string]reflect.StructField{}
		if t.Kind() == reflect.Struct {
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f
			}
		}
		out := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" || k.Style&yaml.TaggedStyle != 0 {
				return nil, errors.New("plain string keys required")
			}
			if _, ok := out[k.Value]; ok {
				return nil, fmt.Errorf("duplicate key %s", k.Value)
			}
			var ft reflect.Type
			if t.Kind() == reflect.Map {
				ft = t.Elem()
			} else {
				f, ok := fields[k.Value]
				if !ok {
					return nil, fmt.Errorf("unknown field %s", k.Value)
				}
				ft = f.Type
			}
			value := n.Content[i+1]
			if ft.Kind() == reflect.String && contains([]string{"effective_at", "expires_at", "deadline"}, k.Value) && value.Tag == "!!timestamp" {
				copy := *value
				copy.Tag = "!!str"
				value = &copy
			}
			v, err := closed(value, ft)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k.Value, err)
			}
			out[k.Value] = v
		}
		for k, f := range fields {
			if _, ok := out[k]; !ok && !strings.Contains(f.Tag.Get("json"), ",omitempty") {
				return nil, fmt.Errorf("missing field %s", k)
			}
		}
		return out, nil
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return nil, errors.New("sequence required")
		}
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			v, err := closed(child, t.Elem())
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case reflect.String:
		if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
			return n.Value, nil
		}
	case reflect.Bool:
		if n.Kind == yaml.ScalarNode && n.Tag == "!!bool" && (n.Value == "true" || n.Value == "false") {
			return n.Value == "true", nil
		}
	case reflect.Uint64, reflect.Int:
		if n.Kind == yaml.ScalarNode && n.Tag == "!!int" && decimal.MatchString(n.Value) {
			return json.Number(n.Value), nil
		}
	}
	return nil, fmt.Errorf("invalid %s value", t.Kind())
}

func canonical(v any) []byte { b, _ := json.Marshal(v); return b }

func times(effective, expires string) (time.Time, time.Time, error) {
	a, err := time.Parse(time.RFC3339, effective)
	if err != nil {
		return a, a, err
	}
	b, err := time.Parse(time.RFC3339, expires)
	if err != nil || a.Nanosecond() != 0 || b.Nanosecond() != 0 || !b.After(a) || b.Sub(a) > 90*24*time.Hour {
		return a, b, errors.New("invalid validity (maximum 90 days, whole seconds with zone)")
	}
	return a, b, nil
}
func current(effective, expires string, now time.Time) bool {
	a, b, e := times(effective, expires)
	return e == nil && !now.Before(a) && now.Before(b)
}
func setOK(xs []string, empty bool) bool {
	seen := map[string]bool{}
	if !empty && len(xs) == 0 {
		return false
	}
	for _, x := range xs {
		if strings.TrimSpace(x) == "" || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}
func subset(a, b []string) bool {
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}
