package actuation

import "testing"

// The Loop's CronJob needs to record its own outcomes. Handing it an operator
// token to do so would also hand it `POST /cluster/workloads/scale`. D10
// forbids scaling the trade daemon outright.
func TestReporterCannotActuate(t *testing.T) {
	if roleLevel(RoleReporter) >= roleLevel(RoleOperator) {
		t.Fatal("reporter reaches operator-gated routes: scale, gitops, trust overrides")
	}
	if roleLevel(RoleReporter) >= roleLevel(RoleAdmin) {
		t.Fatal("reporter reaches admin-gated routes: drain, poweroff")
	}
}

func TestReporterOutranksViewer(t *testing.T) {
	// It writes evidence, so a plain read-only token must not satisfy it.
	if roleLevel(RoleReporter) <= roleLevel(RoleViewer) {
		t.Fatal("a viewer token can record skill runs")
	}
}

func TestOperatorAndAdminStillSatisfyReporter(t *testing.T) {
	// Require(min) is a floor; a human operator must not be locked out of a
	// route that a workload can reach.
	for _, r := range []Role{RoleOperator, RoleAdmin} {
		if roleLevel(r) < roleLevel(RoleReporter) {
			t.Fatalf("%s cannot satisfy reporter", r)
		}
	}
}

func TestTheExistingLadderIsUnchanged(t *testing.T) {
	// Inserting a rung must not reorder the ones already in use.
	if !(roleLevel(RoleViewer) < roleLevel(RoleOperator) &&
		roleLevel(RoleOperator) < roleLevel(RoleAdmin)) {
		t.Fatal("viewer < operator < admin no longer holds")
	}
}

func TestAnUnknownRoleIsTreatedAsViewer(t *testing.T) {
	if roleLevel(Role("nonsense")) != roleLevel(RoleViewer) {
		t.Fatal("an unrecognised role must not outrank viewer")
	}
}

// agentd sits below viewer like executor, and an agentd token is bound to a
// host by its name.
func TestAgentdIsBelowViewerAndBoundToAHost(t *testing.T) {
	if roleLevel(RoleAgentd) >= roleLevel(RoleViewer) {
		t.Fatal("agentd must not pass a Require(viewer) route")
	}
	if host, ok := AgentdHost(Principal{Name: "agentd:mac-dev", Role: RoleAgentd}); !ok || host != "mac-dev" {
		t.Fatalf("host %q %v", host, ok)
	}
	for _, p := range []Principal{{Name: "agentd:", Role: RoleAgentd}, {Name: "mac-dev", Role: RoleAgentd}, {Name: "agentd:x", Role: RoleViewer}} {
		if _, ok := AgentdHost(p); ok {
			t.Fatalf("%+v counted as bound", p)
		}
	}
}
