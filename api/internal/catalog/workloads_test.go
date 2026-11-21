package catalog

import (
	"context"
	"testing"
)

// Origin: 0.2 map line, 2026-11-20, segment 3. The index had no source for
// what runs in the cluster (107 workloads read by hand, none labelled with a
// domain); the scan now lists them and reads the table's workloads rows.

func TestTheClusterIsScannedAgainstTheWorkloadRows(t *testing.T) {
	home := tree("api/internal/a/a.go")
	read := home.Read
	home.Read = func(p string) ([]byte, bool, error) {
		if p == OwnershipPath {
			return []byte(`{"packages":{"api/internal/a":{"domains":["plan"]}},
 "workloads":{"ns1":{"domains":["tenant"]},"data/nats":{"domains":["foundation"]}}}`), true, nil
		}
		return read(p)
	}
	keys := []string{"ns1/web/deployment", "ns1/job/cronjob", "data/nats/statefulset", "data/new-thing/deployment", "other/x/daemonset"}
	s := &Scanner{Kept: memKept{}}
	s.Source = func(_ context.Context, repo string) (Tree, error) {
		if repo == ClusterRepo {
			rows, commit := s.WorkloadRows()
			return WorkloadTree(commit, keys, rows)
		}
		return home, nil
	}
	if _, err := s.ScanOnce(context.Background(), "home"); err != nil {
		t.Fatal(err)
	}
	p, err := s.ScanOnce(context.Background(), ClusterRepo)
	if err != nil {
		t.Fatal(err)
	}
	got := paths(p.Entries)
	if len(got) != 2 || got["data/new-thing"] == "" || got["other/x"] == "" || p.Commit != "c1" {
		t.Fatalf("cluster pool = %v (commit %s), want data/new-thing and other/x at the table's commit", got, p.Commit)
	}
	o, _ := s.lastOwners(ClusterRepo)
	if r := OwnerOf(o, nil, "ns1/web"); !r.Perceived || r.Domains[0] != "tenant" || r.By != "path ns1" {
		t.Fatalf("ns1/web: %+v", r)
	}
	if r := OwnerOf(o, nil, "data/nats"); !r.Perceived || r.Domains[0] != "foundation" {
		t.Fatalf("data/nats: %+v", r)
	}
}

func TestALiveListOrAnotherRepositorysFileIsASource(t *testing.T) {
	ix, err := ParseIndex([]byte(`{"classes":[
 {"class":"repository","sources":[],"live":"the organization's listing"},
 {"class":"seat","sources":["infra:agent-config/charters/"]},
 {"class":"x","sources":[]}]}`), "c", func(string) bool { return false }, Table{})
	if err != nil {
		t.Fatal(err)
	}
	if ix.Measured != 2 || ix.Unmeasured != 1 || len(ix.Missing) != 0 {
		t.Fatalf("%+v: a live list and a source in another repository are sources, and the latter is not looked for here", ix)
	}
}

// Origin: 2026-11-21, the compute line asked who owns the database behind
// ComputePainDbReplication; data/bifrost-postgres was in no workload row and
// in no pool entry, because CNPG pods are neither Deployments nor StatefulSets.
func TestDatabaseClustersAreReadFromTheOperatorsList(t *testing.T) {
	raw := []byte(`{"items":[
	 {"metadata":{"namespace":"data","name":"bifrost-postgres"},"spec":{"instances":2},"status":{"readyInstances":1}},
	 {"metadata":{"name":"no-namespace"}}]}`)
	got, err := ParseDatabaseClusters(raw)
	if err != nil || len(got) != 1 || got[0] != (DatabaseCluster{"data", "bifrost-postgres", 2, 1}) {
		t.Fatalf("%+v %v", got, err)
	}
	// Positive control for the pool: as a "<ns>/<name>/postgres" file with no
	// row it is an entry; a namespace row covers it.
	tree, err := WorkloadTree("c", []string{"data/bifrost-postgres/postgres"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p := PoolOf(ClusterRepo, tree); len(p.Entries) != 1 || p.Entries[0].Path != "data/bifrost-postgres" {
		t.Errorf("no row: %+v", p.Entries)
	}
	tree, _ = WorkloadTree("c", []string{"data/bifrost-postgres/postgres"}, []byte(`{"data/bifrost-postgres":{"domains":["foundation"]}}`))
	if p := PoolOf(ClusterRepo, tree); len(p.Entries) != 0 {
		t.Errorf("row: %+v", p.Entries)
	}
	if _, err := ParseDatabaseClusters([]byte("x")); err == nil {
		t.Error("garbage parsed")
	}
}
