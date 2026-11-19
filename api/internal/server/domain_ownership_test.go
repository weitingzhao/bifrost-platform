//bifrost:owner verification

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep the source-owned directory attached to real packages. The production
// projection reads real imports from the requested commit, not these stubs.
func TestDomainOwnershipCatalog(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config/domain-ownership.json": string(raw),
		"bifrost.units.yaml":           "version: 1\nmodules:\n  - name: api\n    kind: go\n    root: api\n    tasks: {test: {per: package}, vet: {per: module}}\n",
		"api/go.mod":                   "module example.com/catalog\n\ngo 1.25\n",
	}
	packages := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(root, "api"), func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == "testdata" || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") || e.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		packages[dir] = true
		files[dir+"/catalog.go"] = "package catalog\n"
		if strings.HasSuffix(path, "_test.go") {
			files[dir+"/catalog_test.go"] = "package catalog\n"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := unitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{"c": files}), "r", "c")
	if err != nil || got.Domains == nil || got.DomainNote != "" {
		t.Fatalf("catalog: %+v %v", got, err)
	}
	if len(got.Domains.Packages) != len(packages) || !got.Domains.InventoryComplete {
		t.Fatalf("inventory: %d packages, want %d", len(got.Domains.Packages), len(packages))
	}
	if got.Domains.Unresolved == 0 {
		t.Fatal("mixed and default-maintainer responsibilities must remain visible")
	}
}

// Origin: 2026-11-16, cluster reads the node pools through an evaluator the
// server binds (cluster.BindPools) instead of importing compute's placement
// package. Unbound, GET /cluster/placement answers unknown and service
// readiness finds no pool, and nothing else would say so: this holds the one
// line of wiring.
func TestServerBindsThePoolEvaluator(t *testing.T) {
	t.Parallel()
	// New calls bindClusterReadings, and that function holds the three lines.
	root, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(root), "\tbindClusterReadings(clusterH)\n") {
		t.Fatal("New does not call bindClusterReadings")
	}
	built, err := os.ReadFile("../tenant/assembly/assembly.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(built), "DataClone:  dataclone.NewHandler(entry, audit, kube),") {
		t.Fatal("the tenant's assembly does not build the data clone with the cluster's entry, the audit log and its PostgreSQL adapter")
	}
	src, err := os.ReadFile("mount_cluster.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "clusterH.Service().BindPools(placement.ClusterPools)") {
		t.Fatal("bindClusterReadings does not bind placement.ClusterPools to the cluster service")
	}
	// The same for the tenant's readings of applications
	// (cluster.BindAppReadings): unbound, the applications domain is unknown.
	if !strings.Contains(string(src), "cluster.BindAppReadings(cluster.AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain})") {
		t.Fatal("bindClusterReadings does not bind tenant/appreadings to the cluster package")
	}
	// And for compute's readings of the elastic domains (cluster.BindComputeReadings).
	if !strings.Contains(string(src), "cluster.BindComputeReadings(cluster.ComputeReadings{GPUDomain: elasticreadings.GPUDomain, WarehouseDomain: elasticreadings.WarehouseDomain, WorkersDomain: elasticreadings.WorkersDomain, NodeMode: elasticreadings.NodeMode, DisplayedHost: (&elasticreadings.HostReadings{}).Read})") {
		t.Fatal("bindClusterReadings does not bind elasticreadings to the cluster package")
	}
}

// Origin: 2026-11-17, the data clone, data freshness and data probe moved
// out of cluster into package dataclone. Three lines of wiring hold it
// together and nothing else would notice one missing: the data clone runs
// its commands through cluster's PostgreSQL adapter (unbound, every clone
// fails); the trust-anchored action wiring reaches its two write handlers
// through cluster's forwarders (unbound, they answer 503); the market-data
// watchlist union reads the watchlists through its data probe (unbound,
// every environment is an error). And the hourly scheduler is started on
// the data clone's service in the workers process.
func TestServerBindsTheDataClone(t *testing.T) {
	t.Parallel()
	root, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		// Built in the tenant's assembly since 2026-11-19 (tenant/assembly).
		"\ttenantA := tenantassembly.New(cfg, clusterH.Service().Entry(), audit, clusterH.Service())\n",
		"\tdataH, marketdataH, ibgatewayH := tenantA.DataClone, tenantA.MarketData, tenantA.IBGateway\n",
		"\tbindDataClone(clusterH, dataH, marketdataH)\n",
		"\t\tdataH.Service().StartDataCloneScheduler(context.Background())\n",
		"\t\tdataclone:       dataH,\n",
		"\t\tmarketdata:      marketdataH,\n",
	} {
		if !strings.Contains(string(root), line) {
			t.Fatalf("server.go lacks %q", line)
		}
	}
	src, err := os.ReadFile("mount_cluster.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"clusterH.BindDataClone(cluster.DataCloneWrites{Clone: dataH.HandleDataClone, SchedulePut: dataH.HandleDataCloneSchedulePut})",
		"marketdataH.Service().BindWatchlist(dataH.Service().WatchlistSymbols)",
	} {
		if !strings.Contains(string(src), line) {
			t.Fatalf("bindDataClone does not hold %q", line)
		}
	}
}
