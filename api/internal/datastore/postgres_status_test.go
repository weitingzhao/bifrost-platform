package datastore

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// 2026-11-17 audit row #6 (LANE-BIZ-ALIGN): every database was written as
// reachable without any observation. With a read, the databases follow the
// observed CNPG cluster status. Moved from cluster's
// TestPostgresDatabasesComeFromEnvironments with the code on 2026-11-18.
func TestDatabasesFollowTheObservedClusterStatus(t *testing.T) {
	dbs := []PostgresDatabaseView{{Name: "a"}}
	markDatabasesFromCluster(dbs, ServiceDependencyView{Reachability: probe.ReachDegraded, Detail: "ready 1/2"})
	if dbs[0].Reach != probe.ReachDegraded {
		t.Fatalf("database reach with a degraded cluster = %q, want degraded", dbs[0].Reach)
	}
}
