// Package metrics defines versioned delivery metrics; it does not compute readings.
package metrics

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

//go:embed definitions.yaml
var definitionsYAML []byte

// Definition is the configuration hashed for a metric version. Canonical JSON is
// encoding/json's compact encoding of this struct (in field order), with map
// keys sorted recursively. SHA256 itself is excluded from that encoding.
type Definition struct {
	ID          string         `yaml:"id" json:"id"`
	Version     int            `yaml:"version" json:"version"`
	Title       string         `yaml:"title" json:"title"`
	Unit        string         `yaml:"unit" json:"unit"`
	Extractor   string         `yaml:"extractor" json:"extractor"`
	Params      map[string]any `yaml:"params" json:"params"`
	Headline    string         `yaml:"headline" json:"headline"`
	MinSamples  int            `yaml:"min_samples" json:"min_samples"`
	WindowKinds []string       `yaml:"window_kinds" json:"window_kinds"`
	SettleS     int            `yaml:"settle_s" json:"settle_s"`
	SHA256      string         `yaml:"-" json:"-"`
}

// DefinitionKey identifies one computed reading, including its definition version.
type DefinitionKey struct {
	ID      string
	Version int
	Label   string
}

type Registry struct{ definitions []Definition }

// Load parses and validates the embedded configuration.
func Load() (*Registry, error) { return parse(definitionsYAML) }

var metricID = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func parse(data []byte) (*Registry, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var defs []Definition
	if err := dec.Decode(&defs); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one YAML document")
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("definitions are empty")
	}
	seen := map[string]bool{}
	for i := range defs {
		d := &defs[i]
		if !metricID.MatchString(d.ID) || seen[d.ID] {
			return nil, fmt.Errorf("invalid or duplicate metric id %q", d.ID)
		}
		seen[d.ID] = true
		switch d.Extractor {
		case "task_lead_time", "owner_wait_share", "cards_opened", "ci_runs_per_commit", "release_window_occupancy", "macro_wall_time", "tasks_accepted":
		default:
			return nil, fmt.Errorf("%s: unknown extractor %q", d.ID, d.Extractor)
		}
		if d.Version < 1 || d.Title == "" || d.MinSamples < 1 || d.SettleS < 0 || d.Params == nil {
			return nil, fmt.Errorf("%s: invalid version, title, min_samples, settle_s or params", d.ID)
		}
		if !oneOf(d.Unit, "seconds", "count", "ratio") || !oneOf(d.Headline, "p50", "mean", "p90", "count", "ratio") {
			return nil, fmt.Errorf("%s: invalid unit or headline", d.ID)
		}
		windows := map[string]bool{}
		if len(d.WindowKinds) == 0 {
			return nil, fmt.Errorf("%s: window_kinds is empty", d.ID)
		}
		for _, w := range d.WindowKinds {
			if !oneOf(w, "hour", "day", "week", "span") || windows[w] {
				return nil, fmt.Errorf("%s: invalid or duplicate window kind %q", d.ID, w)
			}
			windows[w] = true
		}
		canonical, err := json.Marshal(d)
		if err != nil {
			return nil, fmt.Errorf("%s: canonical JSON: %w", d.ID, err)
		}
		sum := sha256.Sum256(canonical)
		d.SHA256 = hex.EncodeToString(sum[:])
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return &Registry{definitions: defs}, nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

// Definitions returns independent copies sorted by id.
func (r *Registry) Definitions() []Definition {
	// A JSON round trip also copies nested extractor params.
	data, _ := json.Marshal(r.definitions)
	var out []Definition
	_ = json.Unmarshal(data, &out)
	for i := range out {
		out[i].SHA256 = r.definitions[i].SHA256
	}
	return out
}

// Due returns missing combinations in id then label order. Labels are opaque
// window identifiers supplied by the caller; duplicates are returned only once.
func (r *Registry) Due(have map[DefinitionKey]bool, labels []string) []DefinitionKey {
	unique := map[string]bool{}
	for _, label := range labels {
		unique[label] = true
	}
	sorted := make([]string, 0, len(unique))
	for label := range unique {
		sorted = append(sorted, label)
	}
	sort.Strings(sorted)
	var due []DefinitionKey
	for _, d := range r.definitions {
		for _, label := range sorted {
			key := DefinitionKey{ID: d.ID, Version: d.Version, Label: label}
			if !have[key] {
				due = append(due, key)
			}
		}
	}
	return due
}
