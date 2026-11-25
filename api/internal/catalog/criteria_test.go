package catalog

import (
	"encoding/json"
	"testing"
)

// 2026-11-25, Plan: four of the map's criteria (R6:6.9) closed on numbers
// the status already had; the others waited for numbers a done rule can
// read. These pin each: the index's classes by check, the blind spots (the
// cluster listed one by one, the database's tables, repositories with no
// table), the domains with and without their own account, and those past
// the cap of 16 buildings. The control: nothing scanned gives zeros, not a
// guess.
func TestCriteriaAreNumbersFromTheKeptScan(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	if c := s.criteria(nil, nil); c.Index.Measured || c.BlindSpots.DatabaseListed || len(c.OverCap) != 0 {
		t.Fatalf("nothing scanned: %+v", c)
	}
	ix, _ := json.Marshal(Index{Classes: []Class{{Class: "a"}, {Class: "b"}}, Scanned: 1, Unseen: 1})
	_ = s.kept().Write(keptIndex, ix)
	cl, _ := json.Marshal(Pool{Version: Version, Repo: ClusterRepo, Measured: true, Covered: []Covered{{Path: "ns/a", ByNamespace: true}, {Path: "ns/b"}}})
	_ = s.kept().Write(keptName(ClusterRepo), cl)
	db, _ := json.Marshal(Pool{Version: Version, Repo: DatabaseRepo, Measured: true, Total: 19})
	_ = s.kept().Write(keptName(DatabaseRepo), db)
	c := s.criteria(nil, []RepoCount{{Repo: "t", Whole: true}, {Repo: "p"}})
	if c.Index != (CriteriaIndex{Measured: true, Classes: 2, Scanned: 1, Unseen: 1}) {
		t.Errorf("index %+v", c.Index)
	}
	if c.BlindSpots != (CriteriaBlindSpots{ClusterListed: 2, ClusterByNamespace: 1, DatabaseListed: true, DatabaseTables: 19, ReposWithoutTable: 1}) {
		t.Errorf("blind spots %+v", c.BlindSpots)
	}
}
