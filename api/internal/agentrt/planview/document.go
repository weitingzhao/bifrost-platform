// Package planview validates the versioned, offline plan-view artifact.
// It grants no execution authority and does not treat evidence as live facts.
package planview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

const Kind = "agentrt.plan_view/v1"
const MaxBytes = 1 << 20
const MaxNodes = 500
const MaxEdges = 2000

type Document struct {
	V            int       `json:"v"`
	Kind         string    `json:"kind"`
	Tenant       string    `json:"tenant"`
	Root         string    `json:"root"`
	Version      int       `json:"plan_version"`
	Adoption     string    `json:"adoption"`
	SourceCommit string    `json:"source_commit"`
	Decisions    Decisions `json:"decisions"`
	Sources      []Source  `json:"sources"`
	Items        []Item    `json:"items"`
}

type Decisions struct {
	Storage  string `json:"D-PV1"`
	External string `json:"D-PV2"`
	Read     string `json:"D-PV3"`
}

type Source struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Locator string `json:"locator"`
	Basis   string `json:"basis"`
}

type Item struct {
	Node         string            `json:"plan_node_id"`
	Parent       string            `json:"parent"`
	Ordinal      int               `json:"ordinal"`
	Title        string            `json:"title"`
	WorkID       string            `json:"work_id"`
	Lane         string            `json:"lane_kind"`
	Order        *int              `json:"mainline_order"`
	Anchor       *string           `json:"anchor_node_id"`
	Milestones   []string          `json:"milestones"`
	Dependencies []Edge            `json:"depends_on"`
	Responsible  Responsible       `json:"next_responsible"`
	Acceptance   []Acceptance      `json:"acceptance"`
	Estimate     Estimate          `json:"estimate"`
	Bindings     []json.RawMessage `json:"bindings"`
	Evidence     []Evidence        `json:"evidence"`
}

type Edge struct {
	Root        string   `json:"root"`
	Node        string   `json:"node"`
	Milestone   string   `json:"milestone"`
	RequiredFor string   `json:"required_for"`
	Relation    string   `json:"relation"`
	Reason      string   `json:"reason"`
	Sources     []string `json:"sources"`
}

type Responsible struct {
	Kind     string  `json:"kind"`
	Actor    *string `json:"actor_ref"`
	Seat     *string `json:"seat_ref"`
	Provider *string `json:"provider"`
}

type Acceptance struct {
	ID          string   `json:"id"`
	Milestone   string   `json:"milestone"`
	Criterion   string   `json:"criterion"`
	Rule        string   `json:"rule_ref"`
	Evidence    []string `json:"evidence_required"`
	Environment string   `json:"target_environment"`
	Sources     []string `json:"sources"`
}

type Estimate struct {
	WorkDays     []float64 `json:"work_days"`
	ReviewRounds int       `json:"review_rounds"`
	Source       string    `json:"source"`
	Confidence   string    `json:"confidence"`
	Reason       string    `json:"reason"`
}

type Evidence struct {
	Source         string `json:"source"`
	Milestone      string `json:"milestone"`
	Classification string `json:"classification"`
	Assessment     string `json:"assessment"`
	ObservedAt     string `json:"observed_at"`
	LiveVerified   bool   `json:"live_verified"`
}

// Parse refuses unknown, missing and duplicate fields at every level. The
// artifact's decimal estimates are allowed: it is stored by its byte digest,
// not encoded as an event (the event names only the digest and version).
func Parse(raw []byte) (*Document, error) {
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("plan view exceeds %d bytes", MaxBytes)
	}
	v, err := canon.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err = shape(v, reflect.TypeFor[Document](), "artifact"); err != nil {
		return nil, err
	}
	var d Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&d); err != nil {
		return nil, err
	}
	if err = d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// shape preserves the PV0 distinction between an absent field, a null
// optional value, and an empty array. encoding/json alone loses it.
func shape(v any, typ reflect.Type, path string) error {
	if typ.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		return shape(v, typ.Elem(), path)
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok || len(obj) != typ.NumField() {
			return fmt.Errorf("%s: unknown or missing fields", path)
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			key := f.Tag.Get("json")
			value, exists := obj[key]
			if !exists {
				return fmt.Errorf("%s: missing %s", path, key)
			}
			if err := shape(value, f.Type, path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v == nil && strings.HasSuffix(path, ".work_days") {
			return nil
		}
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		for i, value := range arr {
			if err := shape(value, typ.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		if v == nil {
			return fmt.Errorf("%s: null is not allowed", path)
		}
	}
	return nil
}

func text(s string) bool          { return strings.TrimSpace(s) != "" && !strings.ContainsRune(s, 0) }
func optionalText(s *string) bool { return s == nil || text(*s) }

func (d *Document) Validate() error {
	if d.V != 1 || d.Kind != Kind {
		return fmt.Errorf("unknown plan-view protocol")
	}
	if !slices.Contains([]string{"dev", "business"}, d.Tenant) || !text(d.Root) || d.Version < 1 || d.Adoption != "proposed" || !text(d.SourceCommit) {
		return fmt.Errorf("invalid plan identity, version or adoption")
	}
	if d.Decisions != (Decisions{"A", "A", "A"}) {
		return fmt.Errorf("unexpected architecture decisions")
	}
	if len(d.Items) == 0 || len(d.Items) > MaxNodes {
		return fmt.Errorf("plan must contain 1 to %d nodes", MaxNodes)
	}
	sources := map[string]bool{}
	for _, s := range d.Sources {
		if !text(s.ID) || !text(s.Path) || !text(s.Locator) || !text(s.Basis) || sources[s.ID] {
			return fmt.Errorf("invalid or duplicate source %q", s.ID)
		}
		sources[s.ID] = true
	}
	refs := func(ss []string) bool {
		if len(ss) == 0 {
			return false
		}
		for _, s := range ss {
			if !sources[s] {
				return false
			}
		}
		return true
	}
	items := map[string]Item{}
	orders := map[int]bool{}
	parents := map[string][]string{}
	edges := 0
	for _, n := range d.Items {
		if _, exists := items[n.Node]; exists {
			return fmt.Errorf("duplicate node %s", n.Node)
		}
		if !text(n.Node) || n.Node == d.Root || !text(n.Parent) || !text(n.Title) || !text(n.WorkID) || n.Ordinal < 1 {
			return fmt.Errorf("invalid node %q", n.Node)
		}
		items[n.Node] = n
		if n.Lane == "mainline" {
			if n.Order == nil || *n.Order < 1 || orders[*n.Order] || n.Anchor != nil {
				return fmt.Errorf("invalid mainline order or anchor: %s", n.Node)
			}
			orders[*n.Order] = true
		} else if !slices.Contains([]string{"support", "branch"}, n.Lane) || n.Order != nil || n.Anchor == nil {
			return fmt.Errorf("invalid lane: %s", n.Node)
		}
		seen := map[string]bool{}
		for _, m := range n.Milestones {
			if !text(m) || seen[m] {
				return fmt.Errorf("invalid/duplicate milestone: %s", n.Node)
			}
			seen[m] = true
		}
		if len(seen) == 0 {
			return fmt.Errorf("missing milestones: %s", n.Node)
		}
		r := n.Responsible
		if !slices.Contains([]string{"owner", "crew_seat", "vendor_executor", "review"}, r.Kind) || (r.Actor == nil && r.Seat == nil) || !optionalText(r.Actor) || !optionalText(r.Seat) || !optionalText(r.Provider) {
			return fmt.Errorf("invalid responsibility: %s", n.Node)
		}
		e := n.Estimate
		if e.ReviewRounds < 1 || !slices.Contains([]string{"low", "medium"}, e.Confidence) || !refs([]string{e.Source}) || !text(e.Reason) {
			return fmt.Errorf("invalid estimate: %s", n.Node)
		}
		if e.WorkDays != nil && (len(e.WorkDays) != 2 || e.WorkDays[0] < 0 || e.WorkDays[1] < e.WorkDays[0]) {
			return fmt.Errorf("invalid estimate range: %s", n.Node)
		}
		if len(n.Acceptance) == 0 || len(n.Evidence) == 0 {
			return fmt.Errorf("missing acceptance or evidence: %s", n.Node)
		}
		ids := map[string]bool{}
		for _, a := range n.Acceptance {
			if !text(a.ID) || ids[a.ID] || !seen[a.Milestone] || !text(a.Criterion) || !text(a.Rule) || !text(a.Environment) || !refs(a.Sources) || len(a.Evidence) == 0 {
				return fmt.Errorf("invalid acceptance: %s", n.Node)
			}
			for _, s := range a.Evidence {
				if !text(s) {
					return fmt.Errorf("empty evidence requirement")
				}
			}
			ids[a.ID] = true
		}
		for _, e := range n.Evidence {
			if !refs([]string{e.Source}) || !seen[e.Milestone] || !text(e.Assessment) || !text(e.ObservedAt) || e.LiveVerified || !slices.Contains([]string{"repository_evidence", "legacy_evidence", "owner_statement"}, e.Classification) {
				return fmt.Errorf("invalid offline evidence: %s", n.Node)
			}
		}
		duplicate := map[string]bool{}
		for _, e := range n.Dependencies {
			key := e.Root + "\x00" + e.Node + "\x00" + e.Milestone + "\x00" + e.RequiredFor + "\x00" + e.Relation
			if !text(e.Root) || !text(e.Node) || !text(e.Milestone) || !seen[e.RequiredFor] || !slices.Contains([]string{"blocks", "related"}, e.Relation) || !text(e.Reason) || !refs(e.Sources) || duplicate[key] || (e.Root == d.Root && e.Node == n.Node) {
				return fmt.Errorf("invalid/duplicate dependency: %s", n.Node)
			}
			duplicate[key] = true
			edges++
		}
	}
	if edges > MaxEdges {
		return fmt.Errorf("more than %d edges", MaxEdges)
	}
	for i := 1; i <= len(orders); i++ {
		if !orders[i] {
			return fmt.Errorf("mainline order has gaps")
		}
	}
	for _, n := range d.Items {
		parents[n.Node] = nil
		if n.Parent != d.Root {
			if _, ok := items[n.Parent]; !ok {
				return fmt.Errorf("dangling parent: %s", n.Parent)
			}
			parents[n.Node] = []string{n.Parent}
		}
		if n.Anchor != nil && items[*n.Anchor].Lane != "mainline" {
			return fmt.Errorf("dangling/non-mainline anchor: %s", n.Node)
		}
		for _, e := range n.Dependencies {
			if e.Root == d.Root && !slices.Contains(items[e.Node].Milestones, e.Milestone) {
				return fmt.Errorf("dangling node or milestone: %s", e.Node)
			}
		}
	}
	if err := acyclic(parents); err != nil {
		return err
	}
	graph := map[string][]string{}
	for _, n := range d.Items {
		graph[n.Node] = nil
		for _, e := range n.Dependencies {
			if e.Root == d.Root && e.Relation == "blocks" {
				graph[n.Node] = append(graph[n.Node], e.Node)
			}
		}
	}
	if err := acyclic(graph); err != nil {
		return err
	}
	_, err := d.Bindings()
	return err
}

func acyclic(graph map[string][]string) error {
	states := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if states[id] == 1 {
			return fmt.Errorf("cycle at %s", id)
		}
		if states[id] == 2 {
			return nil
		}
		states[id] = 1
		for _, dep := range graph[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		states[id] = 2
		return nil
	}
	for id := range graph {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
