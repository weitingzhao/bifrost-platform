package agentrtapi_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestAcceptanceSignoffEvidenceHTTP(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	must(t, err)
	d, err := planview.Parse(raw)
	must(t, err)
	d.Root = "R2"
	d.Items = d.Items[:1]
	n := &d.Items[0]
	n.Node = "1"
	n.Parent = "R2"
	n.Dependencies = []planview.Edge{}
	n.Evidence = []planview.Evidence{{Source: "work", Milestone: "accepted", Classification: "repository_evidence", Assessment: "test input", ObservedAt: "2026-10-26"}}
	n.Milestones = []string{"accepted"}
	n.Acceptance = n.Acceptance[:1]
	n.Acceptance[0].Milestone = "accepted"
	b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: "acceptance", Version: 1, Tenant: "dev", Root: "R2", Node: "1", Step: "R2:1-1", Acceptance: n.Acceptance[0].ID, AcceptanceSHA256: planview.AcceptanceDigest(n.Acceptance[0]), Atom: "external_verified", Source: "owner_signoff"}
	raw, err = json.Marshal(b)
	must(t, err)
	n.Bindings = []json.RawMessage{raw}
	raw, err = json.Marshal(d)
	must(t, err)
	if code, out := call(t, s, "proposer", "PUT", "artifacts/"+artifactID(raw), raw, "X-Artifact-Kind", "other", session, "signoff-test"); code != 200 {
		t.Fatalf("upload %d %v", code, out)
	}
	if code, out := call(t, s, "proposer", "POST", "plan/proposals", map[string]any{"idempotency_key": "acceptance", "root": "R2", "reason": "adopt acceptance", "changes": []any{map[string]any{"op": "adopt_plan_view", "artifact": artifactID(raw), "expected_version": 0}}}, session, "signoff-test"); code != 200 {
		t.Fatalf("adopt %d %v", code, out)
	}
	if count, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || count != 1 {
		t.Fatalf("decide %d %v", count, err)
	}
	evidence := []byte("Owner-selected evidence")
	if code, out := call(t, s, "proposer", "PUT", "artifacts/"+artifactID(evidence), evidence, "X-Artifact-Kind", "report", session, "signoff-test"); code != 200 {
		t.Fatalf("report %d %v", code, out)
	}
	body := map[string]any{"ref": b.SignoffRef(), "evidence": []string{artifactID(evidence)}}
	for _, role := range []string{"viewer", "operator", "reporter", "proposer", "agentd:h1", ""} {
		if code, _ := call(t, s, role, "POST", "plan/R2:1-1/signoff", body, session, "signoff-test"); code != 401 && code != 403 {
			t.Fatalf("non-owner %s: %d", role, code)
		}
	}
	if code, _ := call(t, s, "admin", "POST", "plan/R2:1-1/signoff", map[string]any{"ref": b.SignoffRef()}); code != 422 {
		t.Fatalf("missing evidence: %d", code)
	}
	if code, out := call(t, s, "admin", "POST", "plan/R2:1-1/signoff", body); code != 200 {
		t.Fatalf("sign %d %v", code, out)
	}
	var event map[string]any
	must(t, pool.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type='owner_signoff' AND plan_node_id='R2:1-1' ORDER BY seq DESC LIMIT 1`).Scan(&event))
	if event["binding_sha256"] != b.Digest() || event["acceptance_sha256"] != b.AcceptanceSHA256 {
		t.Fatalf("wrong identity %v", event)
	}
	if code, out := call(t, s, "viewer", "GET", "plan-view?root=R2", nil); code != 200 {
		t.Fatalf("view %d %v", code, out)
	} else {
		// No work has started on R2:1-1, so the sign-off waits in owner_later as
		// Not started (Work 635); it still carries its candidates.
		var found map[string]any
		for _, e := range append(out["owner_waiting"].([]any), out["owner_later"].([]any)...) {
			if m := e.(map[string]any); m["ref"] == b.SignoffRef() {
				found = m
			}
		}
		if found == nil || found["candidate_evidence"] == nil || found["readiness"] != "Not started" {
			t.Fatalf("missing candidates: %v", found)
		}
	}
}
