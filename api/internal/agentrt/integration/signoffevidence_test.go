package integration_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

func TestPV2SignoffEvidenceAtSigning(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	// Adopt before there is any evidence to choose.
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	var evidence []string
	for i := 0; i < 20; i++ {
		evidence = append(evidence, w.artifact(t, "report", fmt.Sprintf("Evidence %d", i)))
	}
	for _, ids := range [][]string{nil, {}, {"not-an-artifact"}, {evidence[0], evidence[0]}, append(slices.Clone(evidence), evidence[0]), {"sha256:" + strings.Repeat("f", 64)}} {
		if _, err := planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), ids...); err == nil {
			t.Fatalf("accepted invalid evidence %v", ids)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type='owner_signoff'`); n != 0 {
		t.Fatal("invalid requests wrote signoffs")
	}
	first, err := planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	slices.Reverse(evidence)
	again, err := planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	if again["already_signed"] != true || again["event_id"] != first["event_id"] {
		t.Fatal("retry was not idempotent")
	}
	if _, err = planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), evidence[0]); err == nil {
		t.Fatal("silently ignored changed evidence")
	}
	var body struct {
		Binding    string   `json:"binding_sha256"`
		Acceptance string   `json:"acceptance_sha256"`
		Version    int      `json:"plan_version"`
		Evidence   []string `json:"evidence"`
	}
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_id=$1`, first["event_id"]).Scan(&raw))
	ok(t, json.Unmarshal(raw, &body))
	slices.Sort(evidence)
	if body.Binding != b.Digest() || body.Acceptance != b.AcceptanceSHA256 || body.Version != 1 || !slices.Equal(body.Evidence, evidence) {
		t.Fatalf("wrong signoff body: %s", raw)
	}
	pvPoll(t, w, planstore.PlanSources{})
	var observed []string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation'->'evidence' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+root).Scan(&observed))
	if !slices.Equal(observed[1:], evidence) || !strings.HasPrefix(observed[0], "event:") {
		t.Fatalf("observation lost signed evidence: %v", observed)
	}
}

func TestPV2SignoffCandidateEvidence(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	who := wr("thread:evidence")
	spec := externalSpec(t, w)
	report := w.artifact(t, "report", "Candidate report")
	reg, err := externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "work-633", Artifact: spec})
	ok(t, err)
	id := reg["external_work_id"].(string)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: report})
	ok(t, err)
	// Other-step and unsubmitted reports must not become this acceptance's evidence.
	otherSpec := w.artifact(t, "other", `{"v":1,"kind":"agentrt.external_work/v1","task":"Other","executor":"codex","provider":"openai","repo":"platform","branch":"other","acceptance_refs":["other"],"estimate_minutes":[1,2]}`)
	_, err = externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "other", Root: root, Node: s2, RunKey: "other", Artifact: otherSpec})
	ok(t, err)
	// Origin: 2026-11-18, TD-582: the candidates are now made from the work the plan view reads through its port, no longer by one query over the records; what that query left out must stay out, and its order must stay: work not submitted, and a report no artifact holds.
	running := w.artifact(t, "other", `{"v":1,"kind":"agentrt.external_work/v1","task":"Running","executor":"codex","provider":"openai","repo":"platform","branch":"running","acceptance_refs":["running"],"estimate_minutes":[1,2]}`)
	_, err = externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "running", Root: root, Node: s1, RunKey: "running", Artifact: running})
	ok(t, err)
	gone := "sha256:" + strings.Repeat("0", 64)
	report2 := w.artifact(t, "report", "Second candidate report")
	for key, digest := range map[string]string{"gone": gone, "second": report2} {
		reg, err := externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: key, Root: root, Node: s1, RunKey: key, Artifact: spec})
		ok(t, err)
		work := reg["external_work_id"].(string)
		_, err = externalwork.PostExternalEvent(ctx, w.rt, who, work, externalwork.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
		ok(t, err)
		_, err = externalwork.PostExternalEvent(ctx, w.rt, who, work, externalwork.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: digest})
		ok(t, err)
	}
	view, err := planstore.PlanView(ctx, w.rt, root)
	ok(t, err)
	raw, err := json.Marshal(view)
	ok(t, err)
	var out struct {
		Ready []struct {
			Candidates []struct {
				ID            string `json:"artifact_id"`
				Label, Source string
			} `json:"candidate_evidence"`
		} `json:"owner_waiting"`
	}
	ok(t, json.Unmarshal(raw, &out))
	if len(out.Ready) != 1 {
		t.Fatalf("not ready: %s", raw)
	}
	ids := map[string]bool{}
	order := []string{}
	for _, c := range out.Ready[0].Candidates {
		order = append(order, c.ID)
		if ids[c.ID] || c.Label == "" || c.Source == "" {
			t.Fatalf("bad candidate %+v", c)
		}
		ids[c.ID] = true
	}
	if !ids[spec] || !ids[report] || ids[otherSpec] {
		t.Fatalf("missing or cross-step evidence: %+v", out)
	}
	if !ids[report2] || ids[running] || ids[gone] || len(order) != 3 || !slices.IsSorted(order) {
		t.Fatalf("the candidates, in the database's order: %v (running %s, no artifact %s)", order, running, gone)
	}
	pvPoll(t, w, planstore.PlanSources{})
	pvEvaluate(t, w, s1, "planned") // Suggestions are not a signature.
}
