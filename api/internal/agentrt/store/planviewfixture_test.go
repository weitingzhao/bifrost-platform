package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// Runs the actual PlanView response builder against deterministic database
// rows. SQL semantics are covered by the PostgreSQL candidate integration test.
// Update explicitly: UPDATE_PLAN_SIGNOFF_FIXTURE=1 go test ./internal/agentrt/store -run TestPlanSignoffResponseFixture
type signoffViewDB struct {
	DB
	pgx.Tx
	doc      *planview.Document
	adoption viewAdoption
	now      time.Time
	nodes    []map[string]any
}
type signoffViewRows struct {
	pgx.Rows
	data [][]any
	pos  int
}

func (r *signoffViewRows) Next() bool { r.pos++; return r.pos <= len(r.data) }
func (r *signoffViewRows) Close()     {}
func (r *signoffViewRows) Err() error { return nil }
func (r *signoffViewRows) Scan(dest ...any) error {
	for i, d := range dest {
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.data[r.pos-1][i]))
	}
	return nil
}
func (d *signoffViewDB) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if opts.IsoLevel != pgx.RepeatableRead || opts.AccessMode != pgx.ReadOnly {
		return nil, fmt.Errorf("not a read-only snapshot")
	}
	return d, nil
}
func (d *signoffViewDB) Rollback(context.Context) error { return nil }
func (d *signoffViewDB) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	return collectorTestRow(func(dest ...any) error {
		switch {
		case q == "SELECT transaction_timestamp()":
			*dest[0].(*time.Time) = d.now
		case strings.HasPrefix(q, "SELECT b.body"):
			raw, _ := json.Marshal(d.doc)
			*dest[0].(*[]byte) = raw
		case strings.HasPrefix(q, "WITH views AS"):
			raw, _ := json.Marshal(map[string]any{"now": d.now, "views": []any{d.adoption}, "docs": []any{d.doc}})
			*dest[0].(*[]byte) = raw
		case strings.Contains(q, "jsonb_agg"):
			*dest[0].(*[]byte) = []byte(`[]`)
		case strings.Contains(q, "count(DISTINCT e.actor_id)"):
			*dest[0].(*int) = 0
		default:
			return fmt.Errorf("unexpected fixture SQL: %s", q)
		}
		return nil
	})
}
func (d *signoffViewDB) Query(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
	r := &signoffViewRows{}
	switch {
	case strings.Contains(q, "SELECT to_jsonb(n)"):
		for _, n := range d.nodes {
			raw, _ := json.Marshal(n)
			r.data = append(r.data, []any{raw})
		}
	case strings.Contains(q, "SELECT DISTINCT ON (event_stream_id) body_json->'plan_view'"):
		raw, _ := json.Marshal(d.adoption)
		r.data = [][]any{{raw}}
	case strings.Contains(q, "SELECT c.step,c.id,c.label,c.source"):
		for _, step := range []string{"R4:4.1-1", "R4:4.2-1"} {
			for i, source := range []string{"external_work.report:ew_fixture", "external_work.registration:ew_fixture", "verification:v_fixture"} {
				r.data = append(r.data, []any{step, "sha256:" + strings.Repeat(fmt.Sprint(i+1), 64), []string{"Submitted report · work-633", "Work registration · work-633", "deterministic · pass · task-fixture"}[i], source})
			}
		}
	case strings.HasPrefix(q, "SELECT body,at FROM"), strings.HasPrefix(q, "SELECT a.attempt_id"), strings.HasPrefix(q, "SELECT event_stream_id"), strings.HasPrefix(q, "SELECT to_jsonb(e)"):
	default:
		return nil, fmt.Errorf("unexpected fixture SQL: %s", q)
	}
	return r, nil
}
func TestPlanSignoffResponseFixture(t *testing.T) {
	t.Parallel()
	db, _ := collectorTestFixture(t)
	d := db.doc
	first := d.Items[0]
	d.Items = append(d.Items, first)
	d.Items[1].Node = "4.2"
	d.Items[1].Ordinal = 1
	order := 2
	d.Items[1].Order = &order
	d.Items[1].Title = "Blocked acceptance"
	d.Items[1].Dependencies = []planview.Edge{{Root: d.Root, Node: first.Node, Milestone: first.Acceptance[0].Milestone, RequiredFor: first.Acceptance[0].Milestone, Relation: "blocks", Reason: "First acceptance must be verified", Sources: []string{"work"}}}
	f := &signoffViewDB{doc: d, now: time.Date(2026, 10, 26, 22, 0, 0, 0, time.UTC), adoption: viewAdoption{V: 1, Kind: "agentrt.plan_view/v1", Root: d.Root, Tenant: "dev", Version: d.Version, Artifact: "sha256:" + strings.Repeat("a", 64), Nodes: map[string]string{}}}
	addNode := func(id string, parent any, kind, label, title string, ordinal int, rule any) {
		f.nodes = append(f.nodes, map[string]any{"plan_node_id": id, "parent_plan_node_id": parent, "node_kind": kind, "ordinal": ordinal, "label": label, "title": title, "work_id": nil, "mission_id": nil, "step_kind": nil, "owner_step": false, "effect_step": false, "done_rule_json": rule, "state": "planned", "state_event_id": nil, "usual_s": nil, "created_by_actor_id": "controller", "created_at": timeText(f.now), "version": 1})
	}
	addNode(d.Root, nil, "round", "4", "Fixture plan", 0, map[string]any{})
	for i := range d.Items {
		n := &d.Items[i]
		n.Evidence = []planview.Evidence{{Source: "work", Milestone: n.Acceptance[0].Milestone, Classification: "repository_evidence", Assessment: "Fixture input only", ObservedAt: "2026-10-26"}}
		n.Milestones = []string{n.Acceptance[0].Milestone}
		n.Acceptance = n.Acceptance[:1]
		b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: fmt.Sprintf("signoff-%d", i), Version: d.Version, Tenant: d.Tenant, Root: d.Root, Node: n.Node, Step: "R4:" + n.Node + "-1", Acceptance: n.Acceptance[0].ID, AcceptanceSHA256: planview.AcceptanceDigest(n.Acceptance[0]), Atom: "external_verified", Source: "owner_signoff"}
		raw, _ := json.Marshal(b)
		n.Bindings = []json.RawMessage{raw}
		f.adoption.Nodes[n.Node] = "R4:" + n.Node
		addNode("R4:"+n.Node, d.Root, "item", n.Node, n.Title, i, map[string]any{})
		addNode(b.Step, "R4:"+n.Node, "step", n.Node+"-1", "Review submitted evidence", 0, map[string]any{"external_verified": map[string]any{"binding": b.ID, "version": b.Version}})
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := PlanView(context.Background(), f, d.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["owner_waiting"].([]any)) != 1 || len(out["owner_later"].([]any)) != 1 {
		t.Fatalf("missing ready/blocked acceptances: %v", out)
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := "../../../../console/src/api/__fixtures__/plan-view-signoff.generated.json"
	if os.Getenv("UPDATE_PLAN_SIGNOFF_FIXTURE") == "1" {
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("Go plan-view response drifted; regenerate and review the Console fixture")
	}
}
