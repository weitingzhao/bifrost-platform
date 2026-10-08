package lineage

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// ReadFile reads one file from a Gitea repo ref. Progress uses it for the
// infra main copies of TECH_DEBT.md and WORK.md.
func (s *Service) ReadFile(ctx context.Context, repo, ref, path string) (string, error) {
	acc, err := s.access(ctx)
	if err != nil {
		return "", err
	}
	g := &gitea{acc: acc, client: s.client}
	return g.raw(ctx, repo, ref, path)
}

// CommitsSince returns every commit on the default branch, plus branch commits
// that are not the same change already listed from the default branch, newest
// window only. Unlike Build, a commit with no session and no Change-Id is kept:
// progress counts those as missing a Work trailer. This does not ask Gitea to
// sync mirrors.
func (s *Service) CommitsSince(ctx context.Context, since time.Time) ([]Commit, []string) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	acc, err := s.access(ctx)
	if err != nil {
		return nil, []string{"gitea access: " + err.Error()}
	}
	g := &gitea{acc: acc, client: s.client}
	repos, err := g.repos(ctx)
	if err != nil {
		return nil, []string{"list repos: " + err.Error()}
	}

	scans := make([]repoScan, len(repos))
	sem := make(chan struct{}, repoConcurrency)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r giteaRepo) {
			defer wg.Done()
			defer safego.Recover("lineage.commits")
			sem <- struct{}{}
			defer func() { <-sem }()
			scans[i] = scanRepo(ctx, g, r, since)
		}(i, r)
	}
	wg.Wait()

	var errs []string
	for _, sc := range scans {
		if sc.err != nil {
			errs = append(errs, sc.repo+": "+sc.err.Error())
		}
	}
	var rels []Release
	if s.releases != nil {
		rels, err = s.releases(ctx)
		if err != nil {
			errs = append(errs, "releases: "+err.Error())
			rels = nil
		}
	}
	ri := newReachIndex(scans, rels)
	var out []Commit
	for _, sc := range scans {
		out = append(out, flattenScan(sc, ri)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, errs
}

func stamped(repo, ref string, gc giteaCommit, landed bool, landedSHA, landedBy string) Commit {
	subject, t := parseMessage(gc.Msg)
	return Commit{
		Repo: repo, SHA: gc.SHA, Subject: subject, At: gc.At,
		Session: t.session, Transcript: t.transcript, ChangeID: t.changeID, Work: t.work,
		Ref: ref, Landed: landed, LandedSHA: landedSHA, LandedBy: landedBy,
	}
}

func flattenScan(sc repoScan, ri *reachIndex) []Commit {
	var out []Commit
	seen := map[string]bool{}
	mi := newMainIndex(sc)
	for _, gc := range sc.main {
		seen[gc.SHA] = true
		c := stamped(sc.repo, sc.def, gc, true, "", "sha")
		c.Reached = ri.reached(c)
		out = append(out, c)
	}
	names := make([]string, 0, len(sc.branches))
	for b := range sc.branches {
		names = append(names, b)
	}
	sort.Strings(names)
	for _, b := range names {
		for _, gc := range sc.branches[b] {
			if seen[gc.SHA] {
				continue
			}
			seen[gc.SHA] = true
			subject, t := parseMessage(gc.Msg)
			dup, landedSHA, landedBy := mi.branchLanding(subject, t.changeID)
			if dup {
				continue
			}
			c := stamped(sc.repo, b, gc, landedBy != "", landedSHA, landedBy)
			c.Reached = ri.reached(c)
			out = append(out, c)
		}
	}
	return out
}
