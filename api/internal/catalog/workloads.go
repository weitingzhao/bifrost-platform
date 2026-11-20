package catalog

import (
	"encoding/json"
	"fmt"
)

// ClusterRepo names the pool of what runs in the cluster (segment 3, 2026-11-20):
// each workload "<namespace>/<name>" (the run key agreed with the metrics
// line) is scanned like a file of a repository, and the ownership table's
// "workloads" section are its rows: a namespace row covers every workload in
// it, a "<namespace>/<name>" row one workload. What no row covers is in the
// pool, reviewed like any entry.
const ClusterRepo = "cluster"

// keptWorkloads is the "workloads" section of the ownership table, as the last
// scan of a repository whose table has one read it.
const keptWorkloads = "workloads-table"

// WorkloadTree is the cluster as a tree: each workload is a file
// "<namespace>/<name>/<kind>" (so its pool entry is "<namespace>/<name>"),
// its ownership table the "workloads" rows (raw: the section as kept), commit
// the commit those rows were read at.
func WorkloadTree(commit string, files []string, rows []byte) (Tree, error) {
	var section map[string]json.RawMessage
	if len(rows) > 0 {
		if err := json.Unmarshal(rows, &section); err != nil {
			return Tree{}, fmt.Errorf("the workloads section does not parse: %w", err)
		}
	}
	table, err := json.Marshal(map[string]any{"paths": section})
	if err != nil {
		return Tree{}, err
	}
	return Tree{Commit: commit, Files: files, Read: func(p string) ([]byte, bool, error) {
		if p == OwnershipPath {
			return table, true, nil
		}
		return nil, false, nil
	}}, nil
}

type keptRows struct {
	Repo    string          `json:"repo"`
	Commit  string          `json:"commit"`
	Rows    json.RawMessage `json:"rows"`
	Domains json.RawMessage `json:"domains,omitempty"`
}

// keepWorkloads keeps the "workloads" section of own (a no-op without one),
// with the table's domains (their contracts name the routes the status reads).
func (s *Scanner) keepWorkloads(repo, commit string, own []byte) {
	var o struct {
		Workloads json.RawMessage `json:"workloads"`
		Domains   json.RawMessage `json:"domains"`
	}
	if json.Unmarshal(own, &o) != nil || len(o.Workloads) == 0 {
		return
	}
	if raw, err := json.Marshal(keptRows{Repo: repo, Commit: commit, Rows: o.Workloads, Domains: o.Domains}); err == nil {
		_ = s.kept().Write(keptWorkloads, raw)
	}
}

func (s *Scanner) keptSections() (keptRows, bool) {
	raw, err := s.kept().Read(keptWorkloads)
	if err != nil || raw == nil {
		return keptRows{}, false
	}
	var k keptRows
	if json.Unmarshal(raw, &k) != nil {
		return keptRows{}, false
	}
	return k, true
}

// WorkloadRows is the kept "workloads" section and the commit it was read at
// (nil before any scan read one). It does not take the scan lock: a Source
// calls it during a scan.
func (s *Scanner) WorkloadRows() ([]byte, string) {
	k, ok := s.keptSections()
	if !ok {
		return nil, ""
	}
	return k.Rows, k.Commit
}
