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
