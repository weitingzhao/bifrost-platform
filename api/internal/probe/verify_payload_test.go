package probe

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestClassifyDatastorePair_ProbeDrift(t *testing.T) {
	class, detail := classifyDatastorePair(ReachFail, ReachOK, "TCP dial failed: lookup bifrost-postgres-rw.data.svc.cluster.local")
	if class != ClassProbeDrift {
		t.Fatalf("got %s want PROBE_DRIFT detail=%s", class, detail)
	}
}

func TestClassifyDatastorePair_DataLayer(t *testing.T) {
	class, _ := classifyDatastorePair(ReachFail, ReachFail, "cluster_api: cnpg down")
	if class != ClassDataLayer {
		t.Fatalf("got %s", class)
	}
}

func TestClassifyDatastorePair_Nominal(t *testing.T) {
	class, _ := classifyDatastorePair(ReachOK, ReachOK, "cluster_api: ok")
	if class != ClassNominal {
		t.Fatalf("got %s", class)
	}
}

func TestVerifyPayload_AllNominal(t *testing.T) {
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
			{ID: "postgres", Category: "datastore", Reachability: ReachOK, Detail: "cluster_api: ok"},
			{ID: "redis", Category: "datastore", Reachability: ReachOK, Detail: "cluster_api: ok"},
			{ID: "api-monitor", Category: "trade_api", Reachability: ReachOK},
		},
	}}
	resp := VerifyPayload(envs, matrices, ds)
	if resp.Summary.Overall != ClassNominal {
		t.Fatalf("overall %s", resp.Summary.Overall)
	}
	if len(resp.Environments) != 1 || resp.Environments[0].Classification != ClassNominal {
		t.Fatalf("env class %v", resp.Environments)
	}
}
