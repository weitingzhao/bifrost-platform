package ownershiptable

import (
	"encoding/json"
	"strings"
	"testing"
)

// 2026-11-22, the map line (law 75 proposal section 3): the ownership table
// changed in 190 commits in seven days and lines collided on it; it is split
// into one file per domain, read back as one document. The control: with no
// parts the main file comes back byte for byte, so nothing changes until a
// row moves.
func TestPartsMergeIntoTheMainTable(t *testing.T) {
	main := []byte(`{"version":1,"domains":[{"id":"plan"}],"packages":{"api/internal/a":{"domains":["plan"]}},"default":"foundation"}`)
	if got, err := Merge(main, nil); err != nil || string(got) != string(main) {
		t.Fatalf("no parts: %s %v", got, err)
	}
	got, err := Merge(main, map[string][]byte{
		"config/domains/lineage.json": []byte(`{"domain":{"id":"lineage","name":"Lineage"},"packages":{"api/internal/l":{"domains":["lineage"]}},"paths":{"scripts/l":{"domains":["lineage"]}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Default  string                     `json:"default"`
		Domains  []struct{ ID string }      `json:"domains"`
		Packages map[string]json.RawMessage `json:"packages"`
		Paths    map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Default != "foundation" || len(doc.Domains) != 2 || doc.Domains[1].ID != "lineage" || len(doc.Packages) != 2 || len(doc.Paths) != 1 {
		t.Errorf("%s", got)
	}
}

func TestARowInTwoPlacesIsAnError(t *testing.T) {
	main := []byte(`{"domains":[{"id":"plan"}],"packages":{"api/internal/a":{"domains":["plan"]}}}`)
	for name, part := range map[string]string{
		"domain twice":  `{"domain":{"id":"plan"}}`,
		"package twice": `{"packages":{"api/internal/a":{"domains":["x"]}}}`,
		"unknown key":   `{"groups":{}}`,
	} {
		if _, err := Merge(main, map[string][]byte{"config/domains/x.json": []byte(part)}); err == nil || !strings.Contains(err.Error(), "x.json") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPartPathsAreTheDirectorysOwnFiles(t *testing.T) {
	got := PartPaths([]string{"config/domains/b.json", "config/domains/a.json", "config/domains/sub/c.json", "config/domain-ownership.json", "config/domains/readme.md"})
	if len(got) != 2 || got[0] != "config/domains/a.json" {
		t.Errorf("%v", got)
	}
}
