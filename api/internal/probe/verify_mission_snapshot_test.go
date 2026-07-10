package probe

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestVerifyMissionSnapshot_PostFixPassed(t *testing.T) {
	envs := []config.Environment{
		{ID: "dev", Label: "Development"},
		{ID: "prod", Label: "Production"},
	}
	ds := DatastoreSnapshot{
		ByEnv: map[string]DatastoreEnvReach{
			"dev":  {Postgres: ReachOK, Redis: ReachOK},
			"prod": {Postgres: ReachOK, Redis: ReachOK},
		},
	}
	okTargets := []Target{
		{ID: "postgres", Category: "datastore", Reachability: ReachOK},
		{ID: "redis", Category: "datastore", Reachability: ReachOK},
		{ID: "api-monitor", Category: "trade_api", Reachability: ReachOK},
	}
	matrices := []MatrixResponse{
		{Environment: "dev", Label: "Development", Targets: okTargets},
		{Environment: "prod", Label: "Production", Targets: okTargets},
	}

	resp := VerifyMissionSnapshot(envs, matrices, ds)
	if resp.PayloadOverall != MissionOK {
		t.Fatalf("payload overall %s", resp.PayloadOverall)
	}
	if !resp.PostFixVerification.Passed {
		t.Fatalf("post_fix should pass: %+v", resp.PostFixVerification)
	}
}

func TestVerifyMissionSnapshot_ProbeDriftBlocksPostFix(t *testing.T) {
	envs := []config.Environment{{ID: "dev", Label: "Development"}}
	ds := DatastoreSnapshot{
		ByEnv: map[string]DatastoreEnvReach{
			"dev": {Postgres: ReachOK, Redis: ReachOK},
		},
	}
	matrices := []MatrixResponse{{
		Environment: "dev",
		Label:       "Development",
		Targets: []Target{
			{ID: "postgres", Category: "datastore", Reachability: ReachFail, Detail: "lookup bifrost-postgres-rw.data.svc.cluster.local"},
			{ID: "redis", Category: "datastore", Reachability: ReachOK},
			{ID: "api-monitor", Category: "trade_api", Reachability: ReachOK},
		},
	}}

	resp := VerifyMissionSnapshot(envs, matrices, ds)
	if resp.PostFixVerification.Passed {
		t.Fatal("probe drift should block post_fix pass")
	}
	if !resp.PostFixVerification.ProbeDriftRemaining {
		t.Fatal("expected probe_drift_remaining")
	}
}

func TestTradeEnvSnapshot_IgnoresPolicyBlockedAndSkippedAuth(t *testing.T) {
	env := config.Environment{ID: "prod", Label: "Production"}
	matrix := MatrixResponse{
		Environment: "prod",
		Label:       "Production",
		Targets: []Target{
			{ID: "api-monitor", Category: "trade_api", Reachability: ReachOK, Auth: AuthSkipped},
			{ID: "postgres", Category: "datastore", Reachability: ReachOK, Auth: AuthSkipped},
			{ID: "redis", Category: "datastore", Reachability: ReachOK, Auth: AuthSkipped},
			{
				ID: "ops-capabilities", Category: "trade_auth",
				Reachability: ReachUnknown, Auth: AuthSkipped,
				Detail: "No ops token configured",
			},
			{
				ID: "ib-operator-rpc", Category: "trade_write",
				Reachability: ReachUnknown, Auth: AuthBlocked,
			},
			{
				ID: "daemon-control-write", Category: "trade_write",
				Reachability: ReachUnknown, Auth: AuthBlocked,
			},
		},
	}

	snap := tradeEnvSnapshot(env, matrix)
	if snap.Signal != MissionOK {
		t.Fatalf("expected ok, got %s (%s)", snap.Signal, snap.Detail)
	}
	if snap.Total != 3 || snap.Reachable != 3 {
		t.Fatalf("expected 3/3 scored targets, got %d/%d", snap.Reachable, snap.Total)
	}
}
