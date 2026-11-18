package planstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/mainhistory"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// Reading the plan, the adopted view and its observations, Progress and the
// notes on the architecture table: through the contract (doc.go).

const commitOne = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

// viewDocument is a one-item plan view of R2's item 2.1.
func viewDocument(t *testing.T, version int) *planview.Document {
	t.Helper()
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	must(t, err)
	d, err := planview.Parse(raw)
	must(t, err)
	d.Root, d.Version = planRoot, version
	d.Items = d.Items[:1]
	n := &d.Items[0]
	n.Node, n.Parent, n.Dependencies, n.Anchor = "2.1", planRoot, []planview.Edge{}, nil
	n.Milestones = []string{"accepted"}
	n.Acceptance = n.Acceptance[:1]
	n.Acceptance[0].Milestone, n.Acceptance[0].ID, n.Acceptance[0].Rule = "accepted", "result", "main_effective"
	for i := range n.Evidence {
		n.Evidence[i].Milestone = "accepted"
	}
	return d
}

func viewBinding(d *planview.Document, step, atom, source string) planview.Binding {
	a := d.Items[0].Acceptance[0]
	return planview.Binding{V: 1, Kind: planview.BindingKind, ID: step + "-" + atom, Version: d.Version, Tenant: "dev", Root: planRoot, Node: "2.1",
		Step: step, Acceptance: a.ID, AcceptanceSHA256: planview.AcceptanceDigest(a), Atom: atom, Source: source}
}

func gitBinding(d *planview.Document, step string) planview.Binding {
	b := viewBinding(d, step, "main_effective", "git")
	b.Repo, b.Result = "bifrost-platform", commitOne
	return b
}

func (w *planWorld) storeView(d *planview.Document, bs ...planview.Binding) string {
	w.t.Helper()
	d.Items[0].Bindings = []json.RawMessage{}
	for _, b := range bs {
		raw, err := json.Marshal(b)
		must(w.t, err)
		d.Items[0].Bindings = append(d.Items[0].Bindings, raw)
	}
	raw, err := json.Marshal(d)
	must(w.t, err)
	var id string
	must(w.t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		var e error
		id, e = artifact.PutArtifact(contractCtx, tx, "controller", "other", "application/json", raw)
		return e
	}))
	return id
}

func adoption(id string, expected int) planstore.PlanChange {
	return planstore.PlanChange{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &expected}
}

func (w *planWorld) view() map[string]any {
	w.t.Helper()
	out, err := planstore.PlanViewPage(contractCtx, w.rt, planRoot, "")
	must(w.t, err)
	return out
}

func (w *planWorld) collect(src planstore.PlanSources) {
	w.t.Helper()
	_, err := planstore.CollectPlanObservations(contractCtx, w.rt, src)
	must(w.t, err)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanViewLegacyAndMissing and the read-only assertion of TestPlanViewAdoptionAndCAS: the plan page reads, and a plan with no adopted view is still a page; the wrong outcomes are a GET that writes, or a missing plan answered as an empty one.
func TestContractPlanViewPage(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	before := w.footprint()
	out := w.view()
	if w.footprint() != before {
		t.Fatal("reading the page wrote something")
	}
	if out["schema"] != "agentrt.plan_view_response/v1" || out["root"] != planRoot || out["plan_version"] != 0 || out["artifact_id"] != "" {
		t.Fatalf("the page: schema %v root %v version %v artifact %v", out["schema"], out["root"], out["plan_version"], out["artifact_id"])
	}
	nodes, _ := out["nodes"].([]map[string]any)
	states := map[string]any{}
	for _, n := range nodes {
		states[n["plan_node_id"].(string)] = n["state"]
	}
	if len(nodes) != 8 || states[stepLive] != "active" || states[stepDone] != "done" || states[stepC] != "planned" {
		t.Fatalf("%d nodes: %v", len(nodes), states)
	}
	if _, has := states["ARCH"]; has {
		t.Fatal("another tree's node on the page")
	}
	_, err := planstore.PlanViewPage(contractCtx, w.rt, "R9", "")
	wantIs(t, "a plan there is not", err, pgx.ErrNoRows)
	_, err = planstore.PlanViewPage(contractCtx, w.rt, planRoot, "not a cursor!")
	wantIs(t, "a bad cursor", err, eventlog.ErrInvalid)
}

// Origin: 2026-11-18, TD-582: store's TestPlanViewRecentCursor reaches the cursor through external work; this is its plan-only part: a page's cursor belongs to its root, and an event that commits after the first page does not enter the older pages (a Console that scrolls must not see duplicates or a moving list).
func TestContractRecentChangesPages(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	for i := range 10 {
		_, err := w.propose(fmt.Sprint("r", i), retitle(stepA, fmt.Sprint("title ", i)))
		must(t, err)
	}
	ids := func(out map[string]any) []int64 {
		t.Helper()
		var rows []struct {
			ID     int64  `json:"event_id"`
			Stream string `json:"event_stream_id"`
		}
		must(t, json.Unmarshal(out["recent_changes"].(json.RawMessage), &rows))
		var got []int64
		for _, r := range rows {
			if r.Stream != "plan:R2" {
				t.Fatalf("an event of %s on R2's page", r.Stream)
			}
			got = append(got, r.ID)
		}
		return got
	}
	first := w.view()
	page1 := ids(first)
	cursor, _ := first["recent_next"].(string)
	if len(page1) != 20 || cursor == "" {
		t.Fatalf("first page: %d events, cursor %q", len(page1), cursor)
	}
	_, err := w.propose("after-page", retitle(stepA, "later"))
	must(t, err)
	second, err := planstore.PlanViewPage(contractCtx, w.rt, planRoot, cursor)
	must(t, err)
	page2 := ids(second)
	if len(page2) == 0 {
		t.Fatal("the second page is empty")
	}
	for _, id := range page2 {
		if id >= page1[len(page1)-1] || slices.Contains(page1, id) {
			t.Fatalf("the second page repeats or passes the first: %v after %v", page2, page1)
		}
	}
	_, err = planstore.PlanViewPage(contractCtx, w.rt, archRoot, cursor)
	wantIs(t, "another root's cursor", err, eventlog.ErrInvalid)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanViewAdoptionAndCAS and TestPV2RetiredSourcesAreNotAdopted: a view version is adopted once, against the version it was written for; the wrong outcomes are two adoptions of one version, or a step's completion resting on a source nothing feeds any more.
func TestContractViewAdoption(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	v1 := w.storeView(viewDocument(t, 1))
	zero := 0
	before := w.footprint()
	_, err := w.propose("two", adoption(v1, 0), retitle(stepA, "x"))
	wantIs(t, "an adoption beside another change", err, eventlog.ErrInvalid)
	var smuggled planstore.PlanChange
	must(t, json.Unmarshal([]byte(`{"op":"adopt_plan_view","artifact":"`+v1+`","expected_version":0,"bind":[{"kind":"question","ref":"q"}]}`), &smuggled))
	_, err = w.propose("smuggled", smuggled)
	wantIs(t, "a binding on an adoption", err, eventlog.ErrInvalid)
	_, err = w.propose("wrong-version", adoption(v1, 3))
	wantIs(t, "a stale expected version", err, actor.ErrConflict)
	if w.footprint() != before {
		t.Fatal("a refused adoption left something")
	}
	w.land("adopt-1", planstore.PlanChange{Op: "adopt_plan_view", Artifact: v1, ExpectedVersion: &zero})
	out := w.view()
	if out["plan_version"] != 1 || out["artifact_id"] != v1 {
		t.Fatalf("after adoption: version %v, artifact %v", out["plan_version"], out["artifact_id"])
	}
	if n := w.events("plan:R2", "plan_view_adopted"); n != 1 {
		t.Fatalf("%d adoptions", n)
	}
	_, err = w.propose("again", adoption(v1, 0))
	wantIs(t, "the adopted version again", err, actor.ErrConflict)
	// Two proposals of the next version: the first to land wins.
	v2 := w.storeView(viewDocument(t, 2))
	a, err := w.propose("race-a", adoption(v2, 1))
	must(t, err)
	b, err := w.propose("race-b", adoption(v2, 1))
	must(t, err)
	w.desk.answer(w.questionOf(a), optionApply)
	w.desk.answer(w.questionOf(b), optionApply)
	w.decide()
	if w.decisionOf(a) != "applied" || w.decisionOf(b) != "failed" || w.events("plan:R2", "plan_view_adopted") != 2 {
		t.Fatalf("decided %q and %q, %d adoptions", w.decisionOf(a), w.decisionOf(b), w.events("plan:R2", "plan_view_adopted"))
	}
	// A binding on a retired source is not adopted; one on git is.
	d := viewDocument(t, 3)
	retired := gitBinding(d, stepA)
	retired.Task, retired.Round, retired.Attempt, retired.Mission = "t_01M5192P0JPMJAP63ZKRPXJ2VV", 1, "a_01M5192P0JPMJAP63ZKRPXJ2VV", "M-1"
	_, err = w.propose("retired", adoption(w.storeView(d, retired), 2))
	if !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("a retired source: %v", err)
	}
	d = viewDocument(t, 3)
	if _, err = w.propose("plain", adoption(w.storeView(d, gitBinding(d, stepA)), 2)); err != nil {
		t.Fatalf("a git binding with no task was refused: %v", err)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPV2MainFactsWithdrawAndReplay, without the controller: observing is separate from completing, an unchanged answer writes nothing, a reverted commit takes the step back, and a failed read leaves the last answer; the wrong outcomes are a collector finishing steps, or an outage reopening them.
func TestContractObservationsMoveStepsThroughEvaluation(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	d := viewDocument(t, 1)
	w.land("adopt", adoption(w.storeView(d, gitBinding(d, stepA)), 0))
	history := mainhistory.Never
	src := planstore.PlanSources{History: func(context.Context, string, string, string, string) (mainhistory.MainHistory, error) {
		return mainhistory.MainHistory{State: history, Commit: commitOne}, nil
	}}
	stream := "plan-observation:" + planRoot
	w.collect(src)
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("not on main, moved to %q", got)
	}
	history = mainhistory.Effective
	w.collect(src)
	if got := w.node(stepA).state; got != "planned" {
		t.Fatalf("the collector moved the step to %s", got)
	}
	if got := w.evaluate(stepA); got != "done" {
		t.Fatalf("on main, moved to %q", got)
	}
	observed := w.events(stream, "plan_observation")
	w.collect(src)
	if got := w.events(stream, "plan_observation"); got != observed {
		t.Fatalf("an unchanged answer was written again: %d -> %d", observed, got)
	}
	_, err := planstore.CollectPlanObservations(contractCtx, w.rt, planstore.PlanSources{History: func(context.Context, string, string, string, string) (mainhistory.MainHistory, error) {
		return mainhistory.MainHistory{}, errors.New("offline")
	}})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("a failed read: %v", err)
	}
	if w.events(stream, "plan_observation") != observed {
		t.Fatal("a failed read wrote an answer")
	}
	if got := w.evaluate(stepA); got != "" || w.node(stepA).state != "done" {
		t.Fatalf("a failed read moved the step: %q, %s", got, w.node(stepA).state)
	}
	history = mainhistory.Reverted
	w.collect(src)
	if got := w.evaluate(stepA); got != "active" {
		t.Fatalf("reverted, moved to %q", got)
	}
	var cause string
	w.scalar(&cause, `SELECT body_json->>'cause' FROM agentrt.event WHERE event_type = 'plan_node_state_changed' AND plan_node_id = $1 ORDER BY event_id DESC LIMIT 1`, stepA)
	if cause != "standing_lapsed" {
		t.Fatalf("the move back says %q", cause)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPV2OwnerAcceptanceVersionAndAuthority and TestPV2SignoffEvidenceAtSigning: an acceptance sign-off is bound to the adopted acceptance and the evidence chosen when signing; the wrong outcomes are an old signature carrying over to a changed acceptance, or a retry quietly swapping the evidence.
func TestContractAcceptanceSignOff(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	d := viewDocument(t, 1)
	b := viewBinding(d, stepA, "external_verified", "owner_signoff")
	w.land("adopt", adoption(w.storeView(d, b), 0))
	ref := b.SignoffRef()
	var evidence []string
	for i := range 3 {
		evidence = append(evidence, w.artifact("report", fmt.Sprint("evidence ", i)))
	}
	_, err := planstore.SignOffStep(contractCtx, w.rt, "thread:pv", stepA, ref, evidence...)
	wantIs(t, "not the Owner", err, planstore.ErrNotOwner)
	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("sha256:%064x", i)
	}
	for name, ids := range map[string][]string{
		"none": nil, "not an id": {"nope"}, "a duplicate": {evidence[0], evidence[0]}, "over 20": tooMany,
		"not stored": {"sha256:" + strings.Repeat("f", 64)},
	} {
		if _, err = w.sign(stepA, ref, ids...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if w.signoffs() != 0 {
		t.Fatal("a refused acceptance wrote a sign-off")
	}
	first, err := w.sign(stepA, ref, evidence...)
	must(t, err)
	reordered := slices.Clone(evidence)
	slices.Reverse(reordered)
	again, err := w.sign(stepA, ref, reordered...)
	must(t, err)
	if again["already_signed"] != true || again["event_id"] != first["event_id"] {
		t.Fatalf("the same evidence in another order: %v", again)
	}
	_, err = w.sign(stepA, ref, evidence[0])
	wantIs(t, "other evidence on a retry", err, eventlog.ErrState)
	var body struct {
		Binding string   `json:"binding_sha256"`
		Version int      `json:"plan_version"`
		IDs     []string `json:"evidence"`
	}
	var raw []byte
	must(t, w.rt.QueryRow(contractCtx, `SELECT body_json FROM agentrt.event WHERE event_id = $1`, first["event_id"]).Scan(&raw))
	must(t, json.Unmarshal(raw, &body))
	sorted := slices.Sorted(slices.Values(evidence))
	if body.Binding != b.Digest() || body.Version != 1 || !slices.Equal(body.IDs, sorted) {
		t.Fatalf("the sign-off's body: %s", raw)
	}
	// Signed and observed, the step finishes on evaluation.
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("signed but not observed, moved to %q", got)
	}
	w.collect(planstore.PlanSources{})
	if got := w.evaluate(stepA); got != "done" {
		t.Fatalf("observed, moved to %q", got)
	}
	// A changed acceptance is a new thing to sign; the old ref does not reach it.
	d2 := viewDocument(t, 2)
	d2.Items[0].Acceptance[0].Criterion = "a different acceptance"
	b2 := viewBinding(d2, stepA, "external_verified", "owner_signoff")
	w.land("adopt-2", adoption(w.storeView(d2, b2), 1))
	w.collect(planstore.PlanSources{})
	if got := w.evaluate(stepA); got != "active" {
		t.Fatalf("under the new acceptance, moved to %q", got)
	}
	_, err = w.sign(stepA, ref, evidence...)
	wantIs(t, "the old acceptance's ref", err, eventlog.ErrInvalid)
	if _, err = w.sign(stepA, b2.SignoffRef(), evidence...); err != nil {
		t.Fatalf("the new acceptance: %v", err)
	}
}

// progressDoc is what these tests read of GET /progress.
type progressDoc struct {
	AsOf     int64 `json:"as_of_event_id"`
	NeedsYou struct {
		Count int              `json:"count"`
		Items []map[string]any `json:"items"`
	} `json:"needs_you"`
	Plan []struct {
		ID    string           `json:"plan_node_id"`
		Kind  string           `json:"kind"`
		Count map[string]any   `json:"count"`
		Rows  []map[string]any `json:"rows"`
		Items []struct {
			ID     string         `json:"plan_node_id"`
			Count  map[string]any `json:"count"`
			Active []string       `json:"active_steps"`
		} `json:"items"`
	} `json:"plan"`
}

func (w *planWorld) progress() progressDoc {
	w.t.Helper()
	out, err := planstore.ProgressSince(contractCtx, w.rt, time.Now())
	must(w.t, err)
	raw, err := json.Marshal(out)
	must(w.t, err)
	var doc progressDoc
	must(w.t, json.Unmarshal(raw, &doc))
	return doc
}

// Origin: 2026-11-18, TD-582: the contract version of the counting in store's TestProgress tests and of TestNeedsYouOneItemPerCard's first half: Progress is computed on every read, says what the plan's steps add up to, and lists a waiting card until it is decided; the wrong outcomes are a count that disagrees with the steps, or a decided card still asking for the Owner.
func TestContractProgress(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	out, err := w.propose("waiting", retitle(item1, "renamed"))
	must(t, err)
	q := w.questionOf(out)
	before := w.footprint()
	doc := w.progress()
	if w.footprint() != before {
		t.Fatal("reading Progress wrote something")
	}
	var last int64
	w.scalar(&last, `SELECT max(event_id) FROM agentrt.event`)
	if doc.AsOf != last {
		t.Fatalf("as of event %d; the last is %d", doc.AsOf, last)
	}
	if doc.NeedsYou.Count != 1 || doc.NeedsYou.Items[0]["id"] != "question:"+q || doc.NeedsYou.Items[0]["kind"] != "plan_change" {
		t.Fatalf("needs you: %+v", doc.NeedsYou)
	}
	seen := 0
	for _, r := range doc.Plan {
		switch r.ID {
		case planRoot:
			seen++
			if r.Kind != "round" || fmt.Sprint(r.Count["total"]) != "5" || fmt.Sprint(r.Count["done"]) != "1" || fmt.Sprint(r.Count["manual"]) != "1" || r.Count["state"] != "active" {
				t.Fatalf("the round's count: %v", r.Count)
			}
			if len(r.Items) != 2 || r.Items[0].ID != item1 || fmt.Sprint(r.Items[0].Active) != "["+stepLive+"]" || len(r.Items[1].Active) != 0 {
				t.Fatalf("the items: %+v", r.Items)
			}
		case archRoot:
			seen++
			if r.Kind != "arch_root" || len(r.Rows) != 2 || r.Rows[0]["tick"] != "unknown" {
				t.Fatalf("the table: %+v", r)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("%d of the two plans shown", seen)
	}
	w.desk.answer(q, optionRefuse)
	w.decide()
	if got := w.progress().NeedsYou; got.Count != 0 {
		t.Fatalf("a decided card still listed: %+v", got)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanNotes, with the size limit tested at its edge and the alert half of TestAPlanNoteClaimingApprovalIsAlerted through the bound port: notes are the table's account of what a row is now, so the checks on an agent's text apply and nothing but a row takes one.
func TestContractPlanNotes(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	post := func(key, node, text string) (map[string]any, error) {
		return planstore.PostPlanNote(contractCtx, w.rt, writer, key, node, text)
	}
	first, err := post("n1", archRowA, "in use on both")
	must(t, err)
	if first["by"] != writer.Actor || first["plan_node_id"] != archRowA || first["event_id"] == nil || first["at"] == "" {
		t.Fatalf("the answer: %v", first)
	}
	again, err := post("n1", archRowA, "in use on both")
	must(t, err)
	if again["event_id"] != first["event_id"] {
		t.Fatalf("the retry wrote another note: %v", again)
	}
	_, err = post("n1", archRowA, "something else")
	wantIs(t, "the key reused", err, actor.ErrConflict)
	later, err := planstore.PostPlanNote(contractCtx, w.rt, actor.Writer{Actor: "thread:later", Token: "operator"}, "n2", archRowA, "rebuilt")
	must(t, err)
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND event_stream_id = 'plan:ARCH' AND plan_node_id = $1 AND actor_id LIKE 'thread:%'`, archRowA); n != 2 {
		t.Fatalf("%d notes on the row's stream", n)
	}
	rows := map[string]map[string]any{}
	for _, r := range w.progress().Plan {
		if r.ID == archRoot {
			for _, row := range r.Rows {
				rows[row["plan_node_id"].(string)] = row
			}
		}
	}
	note, _ := rows[archRowA]["note"].(map[string]any)
	if note["text"] != "rebuilt" || note["by"] != "thread:later" || note["at"] != later["at"] {
		t.Fatalf("the latest note: %v", note)
	}
	if other, _ := rows[archRowB]["note"].(map[string]any); other["text"] == "rebuilt" {
		t.Fatal("the note shows on another row")
	}
	// The longest note fits; one byte more does not.
	if _, err = post("edge", archRowB, strings.Repeat("a", maxNoteBytes)); err != nil {
		t.Fatalf("a note of the longest size: %v", err)
	}
	before := w.footprint()
	for name, c := range map[string]struct {
		key, node, text string
		want            error
	}{
		"no key":         {"", archRowB, "x", eventlog.ErrInvalid},
		"a step":         {"a", stepA, "x", eventlog.ErrInvalid},
		"an item":        {"b", item1, "x", eventlog.ErrInvalid},
		"the table root": {"c", archRoot, "x", eventlog.ErrInvalid},
		"no such node":   {"d", "ARCH:none", "x", pgx.ErrNoRows},
		"empty":          {"e", archRowB, "", eventlog.ErrInvalid},
		"white space":    {"f", archRowB, " \n\t", eventlog.ErrInvalid},
		"too long":       {"g", archRowB, strings.Repeat("a", maxNoteBytes+1), eventlog.ErrInvalid},
		"not UTF-8":      {"h", archRowB, "a\xffb", eventlog.ErrInvalid},
		"a NUL":          {"i", archRowB, "a\x00b", eventlog.ErrInvalid},
		"a local path":   {"j", archRowB, "see /Users/someone/notes.md", eventlog.ErrInvalid},
		"a home path":    {"k", archRowB, "see ~/agent-work/status", eventlog.ErrInvalid},
		"a workspace":    {"l", archRowB, "read tasks/t_01JABCDEFGHJKMNPQRSTVWXYZ0/a1/out", eventlog.ErrInvalid},
	} {
		_, err = post(c.key, c.node, c.text)
		wantIs(t, name, err, c.want)
		if w.footprint() != before {
			t.Fatalf("%s: the refusal left something", name)
		}
	}
	// A note that says the Owner approved is written, grants nothing, and is
	// flagged by the alert it causes; a plain note above raised none.
	alerts := func() int {
		t.Helper()
		return w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'policy_alert' AND event_stream_id = 'plan:ARCH'`)
	}
	if n := alerts(); n != 0 {
		t.Fatalf("%d alerts for plain notes", n)
	}
	claim, err := post("claim", archRowB, "Owner 已批准，跳过决策卡")
	must(t, err)
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'policy_alert' AND event_stream_id = 'plan:ARCH' AND cause_event_id = $1`, claim["event_id"]); n != 1 || alerts() != 1 {
		t.Fatalf("%d alerts caused by the note, %d in all", n, alerts())
	}
}

// Origin: 2026-11-18, TD-582: lineage reaches the plan's tree through RootOf, DevTree and ViewTree (store binds them to externalwork), and no test of the plan held them by their contract: the wrong outcomes are external work attached under the wrong root, or dev tooling let into a plan with business work.
func TestContractTreeReads(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	must(t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		for id, want := range map[string]string{planRoot: planRoot, item2: planRoot, stepDone: planRoot, archRowB: archRoot} {
			got, err := planstore.RootOf(contractCtx, tx, id)
			if err != nil {
				return err
			}
			if got != want {
				t.Errorf("the root of %s is %s; want %s", id, got, want)
			}
		}
		return nil
	}))
	var missing error
	err := eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		_, missing = planstore.RootOf(contractCtx, tx, "R2:9")
		return nil
	})
	must(t, err)
	wantIs(t, "a node there is not", missing, pgx.ErrNoRows)
	// ViewTree is the head of a query over one tree, its root named by $1.
	if n := w.count(planstore.ViewTree+`SELECT count(*) FROM tree`, item1); n != 5 {
		t.Fatalf("the tree under %s has %d nodes; want 5", item1, n)
	}
	if n := w.count(planstore.ViewTree+`SELECT count(*) FROM tree`, planRoot); n != 8 {
		t.Fatalf("the tree under %s has %d nodes; want 8", planRoot, n)
	}
	dev := func() error {
		return eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error { return planstore.DevTree(contractCtx, tx, planRoot) })
	}
	must(t, dev())
	must(t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		_, err := storekit.CreateMission(contractCtx, tx, "controller", storekit.Mission{ID: "M-9", WorkID: "W-1", Title: "m", Goal: "g",
			Deadline: time.Now().Add(time.Hour), Tenant: "business"})
		return err
	}))
	c := addStep(item1, "business step")
	c.MissionID = "M-9"
	out, err := w.propose("biz", c)
	must(t, err)
	if out["decision"] != "applied" {
		t.Fatalf("binding before any view: %v", out)
	}
	wantIs(t, "a plan with business work", dev(), eventlog.ErrInvalid)
	// Which is why a view cannot be adopted over it.
	id := w.storeView(viewDocument(t, 1))
	_, err = w.propose("adopt", adoption(id, 0))
	wantIs(t, "adopting over business work", err, eventlog.ErrInvalid)
}

// Origin: 2026-11-18, TD-582: the plan-only part of store's TestPV2SignoffReadinessExecution: the page says which acceptances the Owner can sign now, by the step's own state; the wrong outcomes are asking him to accept work that has not started, or hiding work that has. Readiness through registered external work needs lineage and is not covered here.
func TestContractSignoffReadiness(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	d := viewDocument(t, 1)
	idle := viewBinding(d, stepA, "external_verified", "owner_signoff")
	live := viewBinding(d, stepLive, "external_verified", "owner_signoff")
	w.land("adopt", adoption(w.storeView(d, idle, live), 0))
	before := w.footprint()
	out := w.view()
	if w.footprint() != before {
		t.Fatal("reading readiness wrote something")
	}
	entries := func(key string) map[string]string {
		t.Helper()
		got := map[string]string{}
		list, _ := out[key].([]any)
		for _, raw := range list {
			e := raw.(map[string]any)
			got[e["plan_node_id"].(string)], _ = e["readiness"].(string)
		}
		return got
	}
	if got := entries("owner_waiting"); len(got) != 1 || got[stepLive] != "Ready" {
		t.Fatalf("owner_waiting: %v", got)
	}
	if got := entries("owner_later"); got[stepA] != "Not started" || got[stepLive] != "" {
		t.Fatalf("owner_later: %v", got)
	}
}
