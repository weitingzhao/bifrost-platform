package store_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
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
		if _, err := store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), ids...); err == nil {
			t.Fatalf("accepted invalid evidence %v", ids)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type='owner_signoff'`); n != 0 {
		t.Fatal("invalid requests wrote signoffs")
	}
	first, err := store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	slices.Reverse(evidence)
	again, err := store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	if again["already_signed"] != true || again["event_id"] != first["event_id"] {
		t.Fatal("retry was not idempotent")
	}
	if _, err = store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), evidence[0]); err == nil {
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
	pvPoll(t, w, store.PlanSources{})
	var observed []string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation'->'evidence' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+root).Scan(&observed))
	if !slices.Equal(observed[1:], evidence) || !strings.HasPrefix(observed[0], "event:") {
		t.Fatalf("observation lost signed evidence: %v", observed)
	}
}

func TestPV2SignoffCandidateEvidence(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	who := wr("thread:evidence")
	spec := externalSpec(t, w)
	report := w.artifact(t, "report", "Candidate report")
	reg, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "work-633", Artifact: spec})
	ok(t, err)
	id := reg["external_work_id"].(string)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: report})
	ok(t, err)
	// Other-step and unsubmitted reports must not become this acceptance's evidence.
	otherSpec := w.artifact(t, "other", `{"v":1,"kind":"agentrt.external_work/v1","task":"Other","executor":"codex","provider":"openai","repo":"platform","branch":"other","acceptance_refs":["other"],"estimate_minutes":[1,2]}`)
	_, err = store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "other", Root: root, Node: s2, RunKey: "other", Artifact: otherSpec})
	ok(t, err)
	view, err := store.PlanView(ctx, w.rt, root)
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
	verified := false
	for _, c := range out.Ready[0].Candidates {
		if ids[c.ID] || c.Label == "" || c.Source == "" {
			t.Fatalf("bad candidate %+v", c)
		}
		ids[c.ID] = true
		verified = verified || strings.HasPrefix(c.Source, "verification:")
	}
	if !ids[spec] || !ids[report] || ids[otherSpec] || !verified {
		t.Fatalf("missing or cross-step evidence: %+v", out)
	}
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "planned") // Suggestions are not a signature.
}
