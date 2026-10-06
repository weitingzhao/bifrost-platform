package lineage

import (
	"context"
	"sort"
)

// ReleasesFunc lists recorded releases (the releases package, adapted by the server).
type ReleasesFunc func(ctx context.Context) ([]Release, error)

// WithReleases makes Build annotate commits with the releases that contained them.
func (s *Service) WithReleases(f ReleasesFunc) *Service {
	s.releases = f
	return s
}

type laneEnv struct{ lane, env string }

// reachIndex answers "first release per lane/env that contains commit X" using
// default-branch order: a release that built the commit at index i contains
// every commit at index >= i (0 is the newest). A built commit outside the
// scanned window is older than every commit in it, so it contains none of them.
type reachIndex struct {
	order map[string]map[string]int // repo -> sha -> index on the default branch
	// per repo and lane/env, releases oldest first with the index they built
	byRepo map[string]map[laneEnv][]indexedRelease
}

type indexedRelease struct {
	rel Release
	idx int
}

func newReachIndex(scans []repoScan, rels []Release) *reachIndex {
	ri := &reachIndex{order: map[string]map[string]int{}, byRepo: map[string]map[laneEnv][]indexedRelease{}}
	for _, sc := range scans {
		m := make(map[string]int, len(sc.main))
		for i, c := range sc.main {
			m[c.SHA] = i
		}
		ri.order[sc.repo] = m
	}
	sorted := append([]Release(nil), rels...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	for _, rel := range sorted {
		for repo, sha := range rel.Repos {
			idx, ok := ri.order[repo][sha]
			if !ok {
				continue
			}
			if ri.byRepo[repo] == nil {
				ri.byRepo[repo] = map[laneEnv][]indexedRelease{}
			}
			k := laneEnv{rel.Lane, rel.Env}
			ri.byRepo[repo][k] = append(ri.byRepo[repo][k], indexedRelease{rel: rel, idx: idx})
		}
	}
	return ri
}

// reached returns, per lane/env, the first release containing the commit.
func (ri *reachIndex) reached(c Commit) []Reach {
	if !c.Landed {
		return nil
	}
	anchor := c.SHA
	if c.LandedSHA != "" {
		anchor = c.LandedSHA
	}
	idx, ok := ri.order[c.Repo][anchor]
	if !ok {
		return nil
	}
	var out []Reach
	for k, rels := range ri.byRepo[c.Repo] {
		for _, r := range rels {
			if r.idx <= idx {
				out = append(out, Reach{Lane: k.lane, Env: k.env, Run: r.rel.Run, At: r.rel.At, Deploys: r.rel.Deploys})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// heads is the latest release per lane/env.
func heads(rels []Release) []ReleaseHead {
	latest := map[laneEnv]Release{}
	for _, r := range rels {
		k := laneEnv{r.Lane, r.Env}
		if cur, ok := latest[k]; !ok || r.At.After(cur.At) {
			latest[k] = r
		}
	}
	out := make([]ReleaseHead, 0, len(latest))
	for k, r := range latest {
		out = append(out, ReleaseHead{Lane: k.lane, Env: k.env, Run: r.Run, At: r.At, Deploys: r.Deploys, Repos: r.Repos})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		return out[i].Env < out[j].Env
	})
	return out
}

// summarize recomputes a thread's counters from its commits.
func summarize(t *Thread) {
	t.CommitCount, t.Landed, t.Reached = 0, 0, map[string]int{}
	for _, rc := range t.Repos {
		for _, c := range rc.Commits {
			t.CommitCount++
			if c.Landed {
				t.Landed++
			}
			for _, r := range c.Reached {
				t.Reached[r.Lane+"/"+r.Env]++
			}
		}
	}
}
