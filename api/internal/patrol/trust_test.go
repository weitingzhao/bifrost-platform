package patrol

import "testing"

func TestEvaluateTrustL0L1L2(t *testing.T) {
	writes := writeToolSet()
	l0 := PatrolSkill{ID: "a", TrustLevel: TrustL0, MCPTools: []string{"get_cluster_summary"}}
	if g := EvaluateTrust(l0, TriggerCron, writes); !g.Allow {
		t.Fatalf("L0 read should allow: %+v", g)
	}
	l0w := PatrolSkill{ID: "b", TrustLevel: TrustL0, MCPTools: []string{"delete_pod"}}
	if g := EvaluateTrust(l0w, TriggerManual, writes); g.Allow || g.Result != ResultEscalated {
		t.Fatalf("L0 write should escalate: %+v", g)
	}

	l1 := PatrolSkill{ID: "c", TrustLevel: TrustL1, MCPTools: []string{"delete_pod"}, CronActuation: CronActuationEscalate}
	if g := EvaluateTrust(l1, TriggerManual, writes); !g.Allow {
		t.Fatalf("L1 manual should allow: %+v", g)
	}
	if g := EvaluateTrust(l1, TriggerCron, writes); g.Allow || g.Result != ResultEscalated {
		t.Fatalf("L1 cron should escalate: %+v", g)
	}
	l1confirm := PatrolSkill{ID: "d", TrustLevel: TrustL1, MCPTools: []string{"delete_pod"}, CronActuation: CronActuationConfirm}
	if g := EvaluateTrust(l1confirm, TriggerCron, writes); !g.Allow {
		t.Fatalf("L1 cron confirm should allow: %+v", g)
	}

	// Webhook trigger follows same policy as cron
	if g := EvaluateTrust(l1, TriggerWebhook, writes); g.Allow || g.Result != ResultEscalated {
		t.Fatalf("L1 webhook escalate should block: %+v", g)
	}
	if g := EvaluateTrust(l1confirm, TriggerWebhook, writes); !g.Allow {
		t.Fatalf("L1 webhook confirm should allow: %+v", g)
	}

	l2 := PatrolSkill{ID: "e", TrustLevel: TrustL2, MCPTools: []string{"get_cluster_summary"}}
	if g := EvaluateTrust(l2, TriggerManual, writes); g.Allow || g.Result != ResultEscalated {
		t.Fatalf("L2 should escalate: %+v", g)
	}
	if g := EvaluateTrust(l2, TriggerCron, writes); g.Allow {
		t.Fatal("L2 cron should not allow")
	}
}
