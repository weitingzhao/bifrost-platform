package store_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func viewDoc(t *testing.T, w *world, version int) string {
	t.Helper()
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	ok(t, err)
	d, err := planview.Parse(raw)
	ok(t, err)
	d.Root = root
	d.Version = version
	d.Items = d.Items[:1]
	n := &d.Items[0]
	n.Node = "2.1"
	n.Parent = root
	n.Dependencies = []planview.Edge{}
	n.Anchor = nil
	raw, err = json.Marshal(d)
	ok(t, err)
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return err
	}))
	return id
}
func adoptView(t *testing.T, w *world, artifact string, version int) *asker {
	t.Helper()
	a := &asker{}
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), fmt.Sprintf("adopt-%d", version), root, "view metadata", []store.PlanChange{{Op: "adopt_plan_view", Artifact: artifact, ExpectedVersion: &version}}, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	return a
}
func externalSpec(t *testing.T, w *world) string {
	t.Helper()
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PutArtifact(ctx, tx, "controller", "other", "application/json", []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"实现可见性","executor":"codex","provider":"openai","repo":"platform","branch":"w31/pv1","acceptance_refs":["PV1"],"estimate_minutes":[10,20]}`))
		return err
	}))
	return id
}
func TestPlanViewAdoptionAndCAS(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id := viewDoc(t, w, 1)
	zeroVersion := 0
	_, badErr := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "smuggled-binding", root, "view", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &zeroVersion, Bind: []store.Binding{{Kind: "question", Ref: "q_bad"}}}}, (&asker{}).ask)
	is(t, badErr, store.ErrInvalid)
	adoptView(t, w, id, 0)
	before := w.footprint(t)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if before != w.footprint(t) {
		t.Fatal("read wrote state")
	}
	if out["plan_version"] != 1 {
		t.Fatalf("not adopted: %v", out)
	}
	zero := 0
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "stale", root, "stale", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &zero}}, (&asker{}).ask)
	is(t, err, store.ErrConflict)
	id2 := viewDoc(t, w, 2)
	one := 1
	a := &asker{}
	for i := 0; i < 2; i++ {
		_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), fmt.Sprint("race", i), root, "version two", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id2, ExpectedVersion: &one}}, a.ask)
		ok(t, err)
		a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	}
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'plan_view'`); n != 2 {
		t.Fatalf("CAS wrote %d adoptions", n)
	}
}
func TestExternalWorkLifecycle(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	adoptView(t, w, viewDoc(t, w, 1), 0)
	spec := externalSpec(t, w)
	who := wr("thread:external")
	in := store.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "work594", Artifact: spec, ObservedAt: "2000-01-01T00:00:00Z"}
	out, err := store.RegisterExternalWork(ctx, w.rt, who, in)
	ok(t, err)
	id := out["external_work_id"].(string)
	again, err := store.RegisterExternalWork(ctx, w.rt, who, in)
	ok(t, err)
	same(t, out, again)
	in.RunKey = "other"
	_, err = store.RegisterExternalWork(ctx, w.rt, who, in)
	is(t, err, store.ErrConflict)
	_, err = store.PostExternalEvent(ctx, w.rt, wr("thread:intruder"), id, store.ExternalEvent{Key: "steal", Seq: 1, Type: "started"})
	is(t, err, store.ErrNotYours)
	for _, typ := range []string{"verified", "verification_failed"} {
		_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: typ, Seq: 1, Type: typ})
		is(t, err, store.ErrInvalid)
	}
	start := store.ExternalEvent{Key: "start", Seq: 1, Type: "started"}
	out, err = store.PostExternalEvent(ctx, w.rt, who, id, start)
	ok(t, err)
	start.Key = "new-key-same-seq"
	again, err = store.PostExternalEvent(ctx, w.rt, who, id, start)
	ok(t, err)
	same(t, out, again)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "same-seq", Seq: 1, Type: "heartbeat"})
	is(t, err, store.ErrConflict)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "gap", Seq: 3, Type: "heartbeat"})
	is(t, err, store.ErrConflict)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		conn := w.db.Connect(t, w.db.Runtime)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := store.PostExternalEvent(ctx, conn, who, id, store.ExternalEvent{Key: "hb", Seq: 2, Type: "heartbeat"})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		ok(t, e)
	}
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "submit", Seq: 3, Type: "submitted"})
	ok(t, err)
	before := w.footprint(t)
	snapshot, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if before != w.footprint(t) {
		t.Fatal("read wrote state")
	}
	raw, _ := json.Marshal(snapshot)
	var doc struct {
		External []struct {
			State  string `json:"state"`
			Status string `json:"status"`
		} `json:"external_work"`
		Finished []any `json:"finished_today"`
	}
	ok(t, json.Unmarshal(raw, &doc))
	if len(doc.External) != 1 || doc.External[0].State != "Submitted" || len(doc.Finished) != 1 {
		t.Fatalf("bad snapshot: %s", raw)
	}
	var state string
	ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id=$1`, s1).Scan(&state))
	if state != "planned" {
		t.Fatal("external submission completed a step")
	}
	w.chainHolds(t, "external-work:"+id)
}
func TestPlanViewLegacyAndMissing(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if out["plan_version"] != 0 {
		t.Fatal(out)
	}
	_, err = store.PlanView(ctx, w.rt, "absent")
	is(t, err, pgx.ErrNoRows)
}

func TestPlanViewDevBoundary(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	// A business mission anywhere in the tree forbids first adoption.
	w.legacy(t, `UPDATE agentrt.mission SET tenant='business' WHERE mission_id='M-1'`)
	id := viewDoc(t, w, 1)
	zero := 0
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "bad-tenant", root, "view", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &zero}}, (&asker{}).ask)
	is(t, err, store.ErrInvalid)
	w.legacy(t, `UPDATE agentrt.mission SET tenant='dev' WHERE mission_id='M-1'`, `UPDATE agentrt.mission SET tenant='business' WHERE mission_id='M-2'`)
	adoptView(t, w, id, 0)
	for i, c := range []store.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}, {Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "bad", MissionID: "M-2"}} {
		_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), fmt.Sprint("bad-bind", i), root, "bind", []store.PlanChange{c}, (&asker{}).ask)
		is(t, err, store.ErrInvalid)
	}
	_, err = w.task(t, "M-2", s2, "", "implement")
	is(t, err, store.ErrInvalid)
	_, err = store.RegisterExternalWork(ctx, w.rt, wr("thread:ext"), store.ExternalRegistration{Key: "bad", Root: root, Node: s2, RunKey: "bad", Artifact: externalSpec(t, w), Mission: "M-2"})
	is(t, err, store.ErrInvalid)
}

// Serial: freezes the shared event clock.
func TestExternalUnconfirmedRecoveryAndRebind(t *testing.T) {
	w := newKernelWorld(t)
	spec := externalSpec(t, w)
	who := wr("thread:recover")
	thaw := store.FreezeEventClock(time.Now().Add(-61 * time.Second))
	out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "recover", Artifact: spec, ObservedAt: "1999-01-01T00:00:00Z"})
	thaw()
	ok(t, err)
	id := out["external_work_id"].(string)
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	ext := view["external_work"].([]map[string]any)
	if len(ext) != 1 || ext[0]["status"] != "Unconfirmed" {
		t.Fatal(ext)
	}
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	view, err = store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if view["external_work"].([]map[string]any)[0]["status"] != "Running" || view["external_work"].([]map[string]any)[0]["backfilled"] != true {
		t.Fatal(view)
	}
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "bad-link", Seq: 2, Type: "linked", Root: "other", Node: s1})
	is(t, err, store.ErrInvalid)
	task, err := w.task(t, "M-1", s3, "", "implement")
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "cross-step", Seq: 2, Type: "linked", Root: root, Node: s1, Task: task})
	is(t, err, store.ErrInvalid)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "link", Seq: 2, Type: "linked", Root: root, Node: s3, Task: task, Mission: "M-1"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "cancel", Seq: 3, Type: "cancelled"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "late-heartbeat", Seq: 4, Type: "heartbeat"})
	is(t, err, store.ErrState)
}

func TestPlanViewRecentCursor(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	who := wr("thread:pages")
	spec := externalSpec(t, w)
	// Twelve runs, each registered and started: 24 listed events, more than a page.
	var id string
	for i := range 12 {
		out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: fmt.Sprint("run-", i), Root: root, Node: s1, RunKey: fmt.Sprint("pages-", i), Artifact: spec})
		ok(t, err)
		id = out["external_work_id"].(string)
		_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
		ok(t, err)
	}
	first, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	cursor := first["recent_next"].(string)
	if cursor == "" {
		t.Fatal("missing recent continuation")
	}
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "end", Seq: 2, Type: "submitted"})
	ok(t, err)
	page, err := store.PlanViewPage(ctx, w.rt, root, cursor)
	ok(t, err)
	if page["recent_as_of"] != first["recent_as_of"] {
		t.Fatal("recent boundary moved")
	}
	var one, two []map[string]any
	ok(t, json.Unmarshal(first["recent_changes"].(json.RawMessage), &one))
	ok(t, json.Unmarshal(page["recent_changes"].(json.RawMessage), &two))
	seen := map[float64]bool{}
	for _, e := range one {
		seen[e["event_id"].(float64)] = true
	}
	for _, e := range two {
		if seen[e["event_id"].(float64)] {
			t.Fatal("duplicate page event")
		}
	}
	_, err = store.PlanViewPage(ctx, w.rt, root, "invalid")
	is(t, err, store.ErrInvalid)
}

// A dispatch run's heartbeat every 30 seconds says only that it is alive; Recent
// changes lists its start and its end, not each heartbeat (2026-10-26: forty
// heartbeats of one run filled the list on ops.bifrost.lan).
func TestPlanViewRecentSkipsHeartbeats(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	who := wr("thread:beats")
	out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "beats", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	types := []string{"started"}
	for range 30 {
		types = append(types, "heartbeat")
	}
	types = append(types, "submitted")
	for i, typ := range types {
		_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: fmt.Sprint(i + 1), Seq: int64(i + 1), Type: typ})
		ok(t, err)
	}
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	var got []map[string]any
	ok(t, json.Unmarshal(view["recent_changes"].(json.RawMessage), &got))
	var listed []string
	for _, e := range got {
		body := e["body_json"].(map[string]any)
		if x, ok := body["external_work"].(map[string]any); ok && x["external_work_id"] == id {
			listed = append(listed, x["type"].(string))
		}
	}
	if want := []string{"submitted", "started", "registered"}; !slices.Equal(listed, want) {
		t.Fatalf("listed %v; want %v", listed, want)
	}
}

func TestPlanViewLargeSnapshot(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	ok(t, err)
	d, err := planview.Parse(raw)
	ok(t, err)
	d.Root = "Rbig"
	d.Items = nil
	d.Sources = []planview.Source{{ID: "test", Path: "plan.json", Locator: "test", Basis: "repository"}}
	edgeCount := 0
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: d.Root, Root: d.Root, Kind: "round", Label: "big", Title: "big", Ordinal: 99}); err != nil {
			return err
		}
		for i := 0; i < 499; i++ {
			label := fmt.Sprintf("big.%03d", i)
			order := i + 1
			owner := "owner"
			n := planview.Item{Node: label, Parent: d.Root, Ordinal: i + 1, Title: "item", WorkID: "W-31", Lane: "mainline", Order: &order, Milestones: []string{"done"}, Dependencies: []planview.Edge{}, Responsible: planview.Responsible{Kind: "owner", Actor: &owner}, Acceptance: []planview.Acceptance{{ID: "done", Milestone: "done", Criterion: "检查", Rule: "owner_signoff", Evidence: []string{"signoff"}, Environment: "none", Sources: []string{"test"}}}, Estimate: planview.Estimate{WorkDays: []float64{1, 2}, ReviewRounds: 1, Source: "test", Confidence: "low", Reason: "test"}, Bindings: []json.RawMessage{}, Evidence: []planview.Evidence{{Source: "test", Milestone: "done", Classification: "repository_evidence", Assessment: "test", ObservedAt: "2026-10-26"}}}
			for j := max(0, i-5); j < i && edgeCount < 2000; j++ {
				n.Dependencies = append(n.Dependencies, planview.Edge{Root: d.Root, Node: fmt.Sprintf("big.%03d", j), Milestone: "done", RequiredFor: "done", Relation: "blocks", Reason: "test", Sources: []string{"test"}})
				edgeCount++
			}
			d.Items = append(d.Items, n)
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: d.Root + ":" + label, Root: d.Root, Parent: d.Root, Kind: "item", Label: label, Title: "item", Ordinal: i}); err != nil {
				return err
			}
		}
		return nil
	}))
	raw, err = json.Marshal(d)
	ok(t, err)
	var artifact string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		artifact, err = store.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return err
	}))
	a := &asker{}
	zero := 0
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:large"), "large", d.Root, "large plan", []store.PlanChange{{Op: "adopt_plan_view", Artifact: artifact, ExpectedVersion: &zero}}, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	durations := []time.Duration{}
	for i := 0; i < 20; i++ {
		started := time.Now()
		out, err := store.PlanView(ctx, w.rt, d.Root)
		ok(t, err)
		_, err = json.Marshal(out)
		ok(t, err)
		durations = append(durations, time.Since(started))
		if len(out["nodes"].([]map[string]any)) != 500 || len(out["edges"].([]planview.Dependency)) != 2000 {
			t.Fatal("truncated snapshot")
		}
	}
	slices.Sort(durations)
	t.Logf("500 nodes / 2000 edges: p95=%v (20 local snapshots including JSON encoding)", durations[18])
	if durations[18] > time.Second {
		t.Errorf("p95 exceeds 1s: %v", durations[18])
	}
}

func TestPlanViewCrossRootAdoption(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	ok(t, err)
	base, err := planview.Parse(raw)
	ok(t, err)
	base.Items = base.Items[:1]
	base.Items[0].Dependencies = []planview.Edge{}
	base.Items[0].Anchor = nil
	makeDoc := func(root, node string, version int) *planview.Document {
		b, _ := json.Marshal(base)
		var d planview.Document
		ok(t, json.Unmarshal(b, &d))
		d.Root = root
		d.Version = version
		d.Items[0].Node = node
		d.Items[0].Parent = root
		return &d
	}
	save := func(d *planview.Document) string {
		b, err := json.Marshal(d)
		ok(t, err)
		var id string
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			id, err = store.PutArtifact(ctx, tx, "controller", "other", "application/json", b)
			return err
		}))
		return id
	}
	apply := func(d *planview.Document, key string) error {
		a := &asker{}
		version := d.Version - 1
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:cross"), key, d.Root, "cross root graph", []store.PlanChange{{Op: "adopt_plan_view", Artifact: save(d), ExpectedVersion: &version}}, a.ask)
		if err != nil {
			return err
		}
		a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
		_, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
		return err
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, n := range []store.PlanNode{{ID: "R3", Root: "R3", Kind: "round", Label: "3", Title: "three", Ordinal: 3}, {ID: "R3:3.1", Root: "R3", Parent: "R3", Kind: "item", Label: "3.1", Title: "item"}} {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", n); err != nil {
				return err
			}
		}
		return nil
	}))
	first := makeDoc(root, "2.1", 1)
	ok(t, apply(first, "first"))
	second := makeDoc("R3", "3.1", 1)
	second.Items[0].Dependencies = []planview.Edge{{Root: root, Node: "2.1", Milestone: first.Items[0].Milestones[0], RequiredFor: second.Items[0].Milestones[0], Relation: "blocks", Reason: "predecessor", Sources: []string{base.Sources[0].ID}}}
	ok(t, apply(second, "second"))
	first.Version = 2
	first.Items[0].Dependencies = []planview.Edge{{Root: "R3", Node: "3.1", Milestone: second.Items[0].Milestones[0], RequiredFor: first.Items[0].Milestones[0], Relation: "blocks", Reason: "cycle", Sources: []string{base.Sources[0].ID}}}
	is(t, apply(first, "cycle"), store.ErrInvalid)
	first.Items[0].Dependencies = []planview.Edge{}
	first.Items[0].Node = "missing"
	is(t, apply(first, "missing"), store.ErrInvalid)
	first.Items[0].Node = "2.1"
	alias := first.Items[0]
	alias.Node = item
	order := 2
	alias.Order = &order
	first.Items = append(first.Items, alias)
	is(t, apply(first, "alias"), store.ErrInvalid)
}
