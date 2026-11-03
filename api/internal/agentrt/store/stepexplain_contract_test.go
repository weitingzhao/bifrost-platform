package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// explainContract is §7's key set of each blocker state, optional keys
// marked with a trailing "?" (R5:5.5-19). A blocker is a closed object:
// every required key, no other.
var explainContract = map[string][]string{
	"tasks_short":            {"atom", "state", "have", "need", "candidates", "omitted?"},
	"no_task":                {"atom", "state"},
	"no_implementation":      {"atom", "state"},
	"no_landing_evidence":    {"atom", "state", "task_id"},
	"landing_unclaimed":      {"atom", "state", "task_id", "accepted_at"},
	"landing_pending":        {"atom", "state", "task_id", "op", "phase", "last_record_at", "grant", "cards", "cards_omitted?"},
	"landing_main_unsettled": {"atom", "state", "task_id", "op", "phase", "last_record_at", "grant", "cards", "cards_omitted?"},
	"landing_failed":         {"atom", "state", "task_id", "op", "outcome", "cards", "cards_omitted?"},
	"landing_declined":       {"atom", "state", "task_id", "op", "rework"},
	"landing_unmapped":       {"atom", "state", "task_id", "op", "phase", "outcome"},
	"stg_needs_landing":      {"atom", "state"},
	"stg_not_supported":      {"atom", "state", "repo"},
	"stg_withheld":           {"atom", "state", "task_id", "op", "kind", "cards", "cards_omitted?", "blocks", "blocks_omitted?"},
	"stg_batch_open":         {"atom", "state", "task_id", "op", "batch_id", "batch_state", "try", "quarantine", "blocks", "blocks_omitted?"},
	"stg_not_admitted": {"atom", "state", "task_id", "op", "to", "blocks", "blocks_omitted?", "repo_blocks", "repo_blocks_omitted?",
		"unexplained"},
	"stg_exhausted": {"atom", "state", "task_id", "op", "cards", "cards_omitted?", "blocks", "blocks_omitted?"},
	"stg_disposed":  {"atom", "state", "task_id", "op", "blocks", "blocks_omitted?"},
}

// contractProblem says what b has that §7 does not, or lacks.
func contractProblem(b map[string]any) string {
	state, _ := b["state"].(string)
	if b["atom"] == "one_of" {
		if len(b) != 2 || b["branches"] == nil {
			return "one_of has atom and branches only"
		}
		for _, br := range b["branches"].([][]Blocker) {
			for _, x := range br {
				if p := contractProblem(x); p != "" {
					return p
				}
			}
		}
		return ""
	}
	keys, ok := explainContract[state]
	if !ok {
		return "no contract for state " + state
	}
	for _, k := range keys {
		if k[len(k)-1] != '?' {
			if _, has := b[k]; !has {
				return state + " lacks " + k
			}
		}
	}
	var extra []string
	for k := range b {
		if !slices.Contains(keys, k) && !slices.Contains(keys, k+"?") {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return state + " has keys outside the contract: " + extra[0]
	}
	return ""
}

// The member blockers of the STG tests, against §7.
func TestExplainSTGContract(t *testing.T) {
	t.Parallel()
	m := exMember("t_m")
	states := map[string]StgState{}
	for _, to := range []string{"", "released", "exhausted", "disposed"} {
		s := newStgState()
		s.Batches["b"] = &StgBatch{Phase: "CLOSED"}
		if to != "" {
			s.Members[m] = StgMemberState{Batch: "b", To: to}
		}
		states["to="+to] = s
	}
	open := newStgState()
	open.Batches["b"] = &StgBatch{Phase: "OPEN"}
	open.Members[m] = StgMemberState{Batch: "b"}
	states["open"] = open
	held := newStgState()
	held.Withheld[m] = StgEvent{Seq: 3, Record: StgRecord{"why": "hard_floor"}}
	states["withheld"] = held
	for name, s := range states {
		b := exExplain(s).stgMember(m)
		if b == nil {
			t.Fatalf("%s: no blocker", name)
		}
		if p := contractProblem(b); p != "" {
			t.Errorf("%s: %s", name, p)
		}
	}
	if p := contractProblem(map[string]any{"atom": "step_landed", "state": "landing_failed", "task_id": "t", "op": "o", "outcome": "x", "cards": nil, "extra": 1}); p == "" {
		t.Error("the check finds no extra key")
	}
}

// The Console's contract samples (console/src/api/__fixtures__/plan-explain-
// *.json, §7) hold to the server's key sets too, so both sides read one
// contract.
func TestExplainConsoleSamples(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../../../console/src/api/__fixtures__/plan-explain-*.json")
	if err != nil || len(files) != 4 {
		t.Fatalf("the four samples: %v %v", files, err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var block struct {
			Steps map[string]struct {
				Kind     string           `json:"kind"`
				Blockers []map[string]any `json:"blockers"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &block); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for id, s := range block.Steps {
			for _, b := range s.Blockers {
				if p := contractProblem(asBlocker(b)); p != "" {
					t.Errorf("%s %s: %s", filepath.Base(f), id, p)
				}
			}
		}
	}
}

// asBlocker turns decoded JSON's one_of branches into the shape the server
// builds ([][]Blocker), so one check reads both.
func asBlocker(m map[string]any) map[string]any {
	if m["atom"] != "one_of" {
		return m
	}
	var branches [][]Blocker
	for _, br := range m["branches"].([]any) {
		var list []Blocker
		for _, x := range br.([]any) {
			list = append(list, asBlocker(x.(map[string]any)))
		}
		branches = append(branches, list)
	}
	return map[string]any{"atom": "one_of", "branches": branches}
}
