package lineage

import (
	"sort"
	"time"
)

// GraphCommit is one node of a repo's commit graph.
type GraphCommit struct {
	SHA      string    `json:"sha"`
	Subject  string    `json:"subject"`
	At       time.Time `json:"at"`
	Session  string    `json:"session,omitempty"`
	ChangeID string    `json:"change_id,omitempty"`
	// Agent: a Claude co-author line without a session trailer (pre-hook agent work).
	Agent bool `json:"agent,omitempty"`
	// Branch commits only: whether and how the change reached the default branch.
	LandedSHA string `json:"landed_sha,omitempty"`
	LandedBy  string `json:"landed_by,omitempty"`
}

// GraphBranch is a branch lane: its commits that the default branch does not have.
type GraphBranch struct {
	Name    string        `json:"name"`
	Commits []GraphCommit `json:"commits"`
	// ForkSHA is the parent of the branch's oldest commit in the window: the
	// default-branch commit it forked from when that is in the window too.
	ForkSHA string `json:"fork_sha,omitempty"`
}

// GraphMarker is a recorded release that built a default-branch commit inside the window.
type GraphMarker struct {
	Lane    string    `json:"lane"`
	Env     string    `json:"env"`
	Run     string    `json:"run"`
	At      time.Time `json:"at"`
	Deploys bool      `json:"deploys"`
	SHA     string    `json:"sha"`
}

// RepoGraph is one repo's lane set: default branch, branch lanes, release markers.
type RepoGraph struct {
	Repo     string        `json:"repo"`
	Default  string        `json:"default"`
	Main     []GraphCommit `json:"main"`
	Branches []GraphBranch `json:"branches"`
	Markers  []GraphMarker `json:"markers"`
}

// buildGraph lays out every scanned repo: the whole default branch in the window
// (threaded or not), branches with at least one change that has not landed, and
// every recorded release whose built commit is on the default branch in the window.
func buildGraph(scans []repoScan, rels []Release) []RepoGraph {
	out := make([]RepoGraph, 0, len(scans))
	for _, sc := range scans {
		g := RepoGraph{Repo: sc.repo, Default: sc.def, Main: make([]GraphCommit, 0, len(sc.main)),
			Branches: []GraphBranch{}, Markers: []GraphMarker{}}
		onMain := make(map[string]bool, len(sc.main))
		for _, gc := range sc.main {
			onMain[gc.SHA] = true
			subject, t := parseMessage(gc.Msg)
			g.Main = append(g.Main, GraphCommit{SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
				ChangeID: t.changeID, Agent: t.session == "" && t.agent})
		}
		mi := newMainIndex(sc)
		names := make([]string, 0, len(sc.branches))
		for b := range sc.branches {
			names = append(names, b)
		}
		sort.Strings(names)
		for _, b := range names {
			var cs []GraphCommit
			open := false
			for _, gc := range sc.branches[b] {
				subject, t := parseMessage(gc.Msg)
				dup, landedSHA, landedBy := mi.branchLanding(subject, t.changeID)
				if dup {
					landedBy = "change_id" // same Change-Id trailer on the default branch
				}
				if landedBy == "" {
					open = true
				}
				cs = append(cs, GraphCommit{SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
					ChangeID: t.changeID, Agent: t.session == "" && t.agent, LandedSHA: landedSHA, LandedBy: landedBy})
			}
			if open { // a branch whose every change landed is a leftover, not a lane
				gb := GraphBranch{Name: b, Commits: cs}
				if raw := sc.branches[b]; len(raw) > 0 && len(raw[len(raw)-1].Parents) > 0 {
					gb.ForkSHA = raw[len(raw)-1].Parents[0]
				}
				g.Branches = append(g.Branches, gb)
			}
		}
		for _, r := range rels {
			if sha, ok := r.Repos[sc.repo]; ok && onMain[sha] {
				g.Markers = append(g.Markers, GraphMarker{Lane: r.Lane, Env: r.Env, Run: r.Run, At: r.At, Deploys: r.Deploys, SHA: sha})
			}
		}
		sort.Slice(g.Markers, func(i, j int) bool { return g.Markers[i].At.After(g.Markers[j].At) })
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}
