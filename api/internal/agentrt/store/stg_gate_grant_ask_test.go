package store

import "testing"

// B5 takes a member landed on a grant the platform asked for itself (R5:5.7):
// that grant has STGRelease true, so the gate sends it, for a core change as
// for a soft one; a grant with STGRelease false is still refused "rule".
func TestGrantAskSTGGateAcceptsControllerGrant(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"api/internal/agentrt/controller/landing_grant_request.go", "console/src/components/A.tsx"} {
		x := stgPolicy(t)
		op := "land:one:1"
		o := x.s.Legacy.Operations[op]
		x.o.Commits[0].Changes[0].OldPath = path
		x.o.Commits[0].Changes[0].NewPath = path
		grant := GrantEvidence{GrantID: "g1", ApprovalID: "approval-319", Purpose: "landing_main", STGRelease: true, IntentSeq: 12,
			Task: o.Task, Attempt: o.Attempt, A: o.A, R: o.R, M0: o.M0, Action: ActionCoreLanding, ApprovalNumber: 319}
		x.f.Authority[op] = stgLandingAuthority{UsedGrant: true, HasGrant: true, Grant: grant}
		if d := x.decide(false); !d.Allowed || d.Why == "rule" {
			t.Fatal(path, "STGRelease true grant refused", d)
		}
		grant.STGRelease = false
		x.f.Authority[op] = stgLandingAuthority{UsedGrant: true, HasGrant: true, Grant: grant}
		if d := x.decide(false); d.Allowed || d.Why != "rule" {
			t.Fatal(path, "STGRelease false grant not refused rule", d)
		}
	}
}
