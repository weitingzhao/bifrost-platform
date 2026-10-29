package store

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// C-6's offline wire fixture is marshalled from the same Go structs as the
// handlers, including omitempty/omitzero and the material handler's envelope.
// Regenerate explicitly: C6_UPDATE_FIXTURES=1 go test ./internal/agentrt/store -run TestC6ConsoleFixture
func TestC6ConsoleFixture(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	a, r, m0 := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	op := "land:t_00000000000000000000000001:1"
	c := GrantCandidate{Env: "prod", Tenant: "dev", Repo: "bifrost-platform", Mission: "M-31", PlanStep: "R4:4.1", Task: "t_00000000000000000000000001", Attempt: "a_console", Op: op,
		A: a, Base: m0, R: r, M0: m0, CommitterDate: at.Format(time.RFC3339), Files: 1, Lines: 3,
		Changes:     []GrantChange{{Old: "console/src/api/approvals.ts", New: "console/src/api/approvals.ts", OldMode: "100644", NewMode: "100644", Added: 2, Deleted: 1}},
		PatchSHA256: strings.Repeat("7", 64), Acceptance: []GrantAcceptance{{VerificationID: "v_console", Result: "pass"}},
		Review:           GrantReview{Task: "t_review", Attempt: "a_review", Executor: "codex", Provider: "openai", Conclusion: "pass", Artifact: "sha256:" + strings.Repeat("8", 64)},
		ReviewPatchMatch: true, CI: GrantCandidateCI{Pipeline: "bifrost-ci-platform", Run: "ci-platform-console", UID: "ci-uid", CreatedAt: at.Format(time.RFC3339), Status: "Succeeded", SHA: r, Siblings: map[string]string{"bifrost-ui": a, "bifrost-trade-infra": m0}},
		STGRelease: true, HardCorePaths: []GrantHardPath{{Path: "console/src/api/approvals.ts", Entry: "console/src/api/approvals.ts"}}, DecisionChain: true, TrustReview: true}
	m := LandingGrantMaterial{Candidate: c, SoftAtRequest: []string{"core", "files"}, WaitingSoft: []string{"core", "files"}, Rule: GrantRule{ID: "landing:dev", Version: 1, SHA256: strings.Repeat("9", 64)}, ChecksVersion: 1,
		Action: ActionCoreLanding, Title: "Trusted core change", FirstLine: "Approving lets the lander push this commit to main. It changes approval, identity or landing-gate code; later approvals are handled by the new code.",
		Phase: "awaiting_grant", Latest: true, Pinned: true, GrantEnabled: true, Patch: "diff --git a/x b/x\n-old\n+new\n+line\n", CompareURL: GiteaWebBase + "/bifrost/bifrost-platform/compare/" + m0 + "..." + r,
		GiteaNotice: "Gitea's own page is not rendered by this platform's code", QuotaReserved: 2, QuotaMax: 5, QuotaNote: "today's count as of this read; the push checks it again", STGNotice: "Approving also authorizes this commit to go to STG with the B5 batch.", Grants: []LandingGrantStatus{}, Problems: []string{}, ComputedAt: at}
	m.PatchSize = len(m.Patch)
	digests := func(m *LandingGrantMaterial) {
		var err error
		m.CandidateDigest, err = m.Candidate.Digest()
		if err != nil {
			t.Fatal(err)
		}
		m.MaterialDigest, err = materialDigest(m.CandidateDigest, m.SoftAtRequest, m.Rule, m.ChecksVersion)
		if err != nil {
			t.Fatal(err)
		}
	}
	digests(&m)
	guardrail := m
	guardrail.Action, guardrail.Title, guardrail.FirstLine = ActionGuardrailLanding, "Soft guardrail change", ""
	guardrail.SoftAtRequest, guardrail.WaitingSoft = []string{"files"}, []string{"files"}
	guardrail.Candidate.HardCorePaths = []GrantHardPath{}
	guardrail.Candidate.DecisionChain, guardrail.Candidate.TrustReview = false, false
	guardrail.Candidate.Changes = []GrantChange{{Old: "console/src/example.ts", New: "console/src/example.ts", OldMode: "100644", NewMode: "100644", Added: 2, Deleted: 1}}
	digests(&guardrail)
	g := LandingGrantStatus{GrantID: "lg_approval_console", ApprovalID: "approval_console", ApprovalNumber: 51, Action: m.Action, Op: op, Waives: m.SoftAtRequest, STGRelease: true, ApprovedAt: at, GrantedAt: at, ExpiresAt: at.Add(24 * time.Hour), Valid: true, CandidateHash: m.CandidateDigest, MaterialHash: m.MaterialDigest}
	w := LandingWaitStatus{Phase: "awaiting_grant", Shown: "authorized", Since: at, Soft: m.SoftAtRequest, Files: 1, Lines: 3, Snapshot: m.MaterialDigest, R: r, M0: m0, Operations: 1, ExecutionSeconds: 30, WaitingSeconds: 60, ApprovalID: g.ApprovalID, ApprovalNumber: 51, ApprovalStatus: "executed", ApprovalURL: "/api/v1/approvals/approval_console", Grants: []LandingGrantStatus{g}}
	w.ObserveMain(m0)
	envelope := func(m LandingGrantMaterial) any {
		return map[string]any{"material": m, "main": m0, "main_known": true, "main_is_M0": true}
	}
	body, err := json.MarshalIndent(map[string]any{"core": envelope(m), "guardrail": envelope(guardrail), "wait": w,
		"revoked": RevokeResult{GrantID: g.GrantID, Op: op, Revoked: true, Message: "revoked"},
		"handed":  RevokeResult{GrantID: g.GrantID, Op: op, Consumed: true, IntentSeq: 42, Message: "handed to the push"}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	path := "../../../../console/src/api/__fixtures__/landing-grant.go.json"
	if os.Getenv("C6_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("Console fixture differs from Go wire structs; regenerate explicitly: %v", err)
	}
}
