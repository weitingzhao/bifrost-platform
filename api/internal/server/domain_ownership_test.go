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
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "clusterH.Service().BindPools(placement.ClusterPools)") {
		t.Fatal("server.go does not bind placement.ClusterPools to the cluster service")
	}
	// The same for the tenant's readings of applications and data
	// (cluster.BindAppReadings): unbound, the applications domain is unknown.
	if !strings.Contains(string(src), "cluster.BindAppReadings(cluster.AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain, FreshnessDatabases: appreadings.FreshnessDatabases})") {
		t.Fatal("server.go does not bind tenant/appreadings to the cluster package")
	}
	// And for compute's readings of the elastic domains (cluster.BindComputeReadings).
	if !strings.Contains(string(src), "cluster.BindComputeReadings(cluster.ComputeReadings{GPUDomain: elasticreadings.GPUDomain, WarehouseDomain: elasticreadings.WarehouseDomain})") {
		t.Fatal("server.go does not bind elasticreadings to the cluster package")
	}
}
