package lineage

import (
	"testing"
	"time"
)

// Origin: 2026-11-22, the Owner (relayed by the CI line): the commit graph drew
// agentd attempts whose task another attempt had already landed as if they were
// pending work (16 of platform's land/prod/* lanes that day), so merged work and
// orphans were not told apart. These pin the kinds the graph now gives each branch.
func TestGraphBranchKinds(t *testing.T) {
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	const landedTask, failedTask = "t_01M71ECKS79SCNWTHPMB9V5DB1", "t_01M71ECKS79SCNWTHPMB9V5DB2"
	m1 := giteaCommit{SHA: "m1", Msg: "agentrt: attempt 2\n\nAgentrt-Task: " + landedTask + "\nChange-Id: " + cidX, At: at}
	m0 := giteaCommit{SHA: "m0", Msg: "feat: base\n\nChange-Id: " + cidY, At: at.Add(-time.Hour)}
	superseded := giteaCommit{SHA: "b1", Msg: "agentrt: attempt 1\n\nAgentrt-Task: " + landedTask + "\nChange-Id: " + cidG, At: at, Parents: []string{"m0"}}
	failed := giteaCommit{SHA: "b2", Msg: "agentrt: attempt 1\n\nAgentrt-Task: " + failedTask + "\nChange-Id: " + cidZ, At: at, Parents: []string{"m0"}}
	human := giteaCommit{SHA: "b3", Msg: "feat: wip\n\nChange-Id: " + cidB, At: at, Parents: []string{"m0"}}
	leftover := giteaCommit{SHA: "b4", Msg: "feat: base\n\nChange-Id: " + cidY, At: at, Parents: []string{"m0"}}
	sc := repoScan{repo: "r1", def: "main", main: []giteaCommit{m1, m0}, branches: map[string][]giteaCommit{
		"land/prod/a/1": {superseded}, "land/prod/b/1": {failed}, "feature": {human},
		"leftover": {leftover}, "merged": nil,
	}}
	g := buildGraph([]repoScan{sc}, nil)[0]
	got := map[string]string{}
	for _, b := range g.Branches {
		got[b.Name] = b.Status
	}
	want := map[string]string{"land/prod/a/1": BranchSuperseded, "land/prod/b/1": BranchOpen, "feature": BranchOpen}
	if len(got) != len(want) {
		t.Fatalf("lanes = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("lane %s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
	counts := map[string]int{BranchOpen: 2, BranchSuperseded: 1, BranchLanded: 1, BranchEven: 1}
	for k, v := range counts {
		if g.BranchCounts[k] != v {
			t.Fatalf("counts = %v, want %v", g.BranchCounts, counts)
		}
	}
}
