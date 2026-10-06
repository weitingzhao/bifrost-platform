package lineage

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	defaultDays     = 14
	maxDays         = 90
	mainMaxPages    = 40 // 2,000 commits per repo per window
	branchMaxPages  = 4
	repoConcurrency = 4
	buildTimeout    = 90 * time.Second
)

// Service builds lineage from the Gitea mirror.
type Service struct {
	access   AccessFunc
	releases ReleasesFunc
	client   *http.Client
	now      func() time.Time
}

func NewService(access AccessFunc) *Service {
	return &Service{
		access: access,
		client: &http.Client{Timeout: 15 * time.Second},
		now:    func() time.Time { return time.Now().UTC() },
	}
}

type repoScan struct {
	repo     string
	def      string
	main     []giteaCommit
	branches map[string][]giteaCommit
	err      error
}

// Build scans every repo's default branch and the branches that moved inside
// the window, and groups lineage-stamped commits by thread.
func (s *Service) Build(ctx context.Context, days int) Response {
	days = clampDays(days)
	now := s.now()
	since := now.AddDate(0, 0, -days)
	out := Response{
		GeneratedAt:  now,
		Since:        since,
		Days:         days,
		Reachability: probe.ReachFail,
		Threads:      []Thread{},
		Coverage:     []RepoCoverage{},
		Releases:     []ReleaseHead{},
		Errors:       []string{},
	}

	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	acc, err := s.access(ctx)
	if err != nil {
		out.Errors = append(out.Errors, "gitea access: "+err.Error())
		return out
	}
	g := &gitea{acc: acc, client: s.client}
	repos, err := g.repos(ctx)
	if err != nil {
		out.Errors = append(out.Errors, "list repos: "+err.Error())
		return out
	}

	scans := make([]repoScan, len(repos))
	sem := make(chan struct{}, repoConcurrency)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r giteaRepo) {
			defer wg.Done()
			defer safego.Recover("lineage.scan")
			sem <- struct{}{}
			defer func() { <-sem }()
			scans[i] = scanRepo(ctx, g, r, since)
		}(i, r)
	}
	wg.Wait()

	threads := map[string]*threadAcc{}
	for _, sc := range scans {
		if sc.err != nil {
			out.Errors = append(out.Errors, sc.repo+": "+sc.err.Error())
		}
		out.Coverage = append(out.Coverage, collect(sc, threads))
	}
	out.Threads = finish(threads)
	s.annotateReach(ctx, &out, scans)

	switch {
	case len(out.Errors) == 0:
		out.Reachability = probe.ReachOK
	case len(out.Errors) < len(repos):
		out.Reachability = probe.ReachDegraded
	}
	sort.Slice(out.Coverage, func(i, j int) bool { return out.Coverage[i].Repo < out.Coverage[j].Repo })
	return out
}

func scanRepo(ctx context.Context, g *gitea, r giteaRepo, since time.Time) repoScan {
	sc := repoScan{repo: r.Name, def: r.Default, branches: map[string][]giteaCommit{}}
	sc.main, sc.err = g.commits(ctx, r.Name, r.Default, since, mainMaxPages, nil)
	if sc.err != nil {
		return sc
	}
	onMain := make(map[string]bool, len(sc.main))
	for _, c := range sc.main {
		onMain[c.SHA] = true
	}
	brs, err := g.branches(ctx, r.Name)
	if err != nil {
		sc.err = fmt.Errorf("branches: %w", err)
		return sc
	}
	for _, b := range brs {
		if b.Name == r.Default || b.At.Before(since) {
			continue
		}
		// a branch walk stops where it joins the default branch
		cs, err := g.commits(ctx, r.Name, b.Name, since, branchMaxPages, func(sha string) bool { return onMain[sha] })
		if err != nil {
			sc.err = fmt.Errorf("branch %s: %w", b.Name, err)
			continue
		}
		sc.branches[b.Name] = cs
	}
	return sc
}

type threadAcc struct {
	t           Thread
	transcripts map[string]bool
	byRepo      map[string][]Commit
}

func (a *threadAcc) add(c Commit) {
	a.byRepo[c.Repo] = append(a.byRepo[c.Repo], c)
	if c.Transcript != "" {
		a.transcripts[c.Transcript] = true
	}
	if a.t.FirstAt.IsZero() || c.At.Before(a.t.FirstAt) {
		a.t.FirstAt = c.At
	}
	if c.At.After(a.t.LastAt) {
		a.t.LastAt = c.At
	}
}

// collect adds one repo's lineage commits to threads and returns its coverage.
func collect(sc repoScan, threads map[string]*threadAcc) RepoCoverage {
	cov := RepoCoverage{Repo: sc.repo, MainCommits: len(sc.main), Branches: len(sc.branches)}
	add := func(c Commit) {
		a := threads[c.Session]
		if a == nil {
			a = &threadAcc{
				t:           Thread{Session: c.Session, Link: sessionLink(c.Session)},
				transcripts: map[string]bool{},
				byRepo:      map[string][]Commit{},
			}
			threads[c.Session] = a
		}
		a.add(c)
	}

	seen := map[string]bool{}
	mainTrailerCID := map[string]bool{}
	mainAnyCID := map[string]string{} // Change-Id anywhere in a main message -> that main sha
	mainSubject := map[string]string{}
	for _, gc := range sc.main {
		seen[gc.SHA] = true
		subject, t := parseMessage(gc.Msg)
		if _, ok := mainSubject[subject]; !ok {
			mainSubject[subject] = gc.SHA
		}
		for _, cid := range changeIDsAnywhere(gc.Msg) {
			if _, ok := mainAnyCID[cid]; !ok {
				mainAnyCID[cid] = gc.SHA
			}
		}
		if t.session != "" {
			cov.WithSession++
		} else if t.agent {
			cov.AgentNoLineage++
		}
		if t.changeID != "" {
			cov.WithChangeID++
			mainTrailerCID[t.changeID] = true
		}
		if t.session == "" && t.changeID == "" {
			continue
		}
		add(Commit{Repo: sc.repo, SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
			Transcript: t.transcript, ChangeID: t.changeID, Ref: sc.def, Landed: true, LandedBy: "sha"})
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
			if t.session == "" && t.changeID == "" {
				continue
			}
			c := Commit{Repo: sc.repo, SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
				Transcript: t.transcript, ChangeID: t.changeID, Ref: b}
			if t.changeID != "" {
				if mainTrailerCID[t.changeID] {
					continue // the same change, already listed from main
				}
				if sha, ok := mainAnyCID[t.changeID]; ok {
					c.Landed, c.LandedSHA, c.LandedBy = true, sha, "change_id" // folded into a squash
				}
			} else if sha, ok := mainSubject[subject]; ok {
				// before the hooks: rebased lanes keep their subject (prune-merged-branches.sh does the same)
				c.Landed, c.LandedSHA, c.LandedBy = true, sha, "subject"
			}
			add(c)
		}
	}
	return cov
}

func finish(threads map[string]*threadAcc) []Thread {
	out := make([]Thread, 0, len(threads))
	for _, a := range threads {
		t := a.t
		t.Transcripts = make([]string, 0, len(a.transcripts))
		for tr := range a.transcripts {
			t.Transcripts = append(t.Transcripts, tr)
		}
		sort.Strings(t.Transcripts)
		t.Repos = make([]RepoCommits, 0, len(a.byRepo))
		for repo, cs := range a.byRepo {
			sort.SliceStable(cs, func(i, j int) bool { return cs[i].At.After(cs[j].At) })
			t.Repos = append(t.Repos, RepoCommits{Repo: repo, Commits: cs})
		}
		sort.Slice(t.Repos, func(i, j int) bool { return t.Repos[i].Repo < t.Repos[j].Repo })
		summarize(&t)
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		// the no-session bucket goes last; otherwise most recent first
		if (out[i].Session == "") != (out[j].Session == "") {
			return out[j].Session == ""
		}
		return out[i].LastAt.After(out[j].LastAt)
	})
	return out
}

func clampDays(d int) int {
	if d <= 0 {
		return defaultDays
	}
	if d > maxDays {
		return maxDays
	}
	return d
}

// annotateReach adds Reached to every landed commit and the release heads.
// Release records are optional: without them lineage still answers v1.
func (s *Service) annotateReach(ctx context.Context, out *Response, scans []repoScan) {
	if s.releases == nil {
		return
	}
	rels, err := s.releases(ctx)
	if err != nil {
		out.ReleasesError = err.Error()
		return
	}
	out.Releases = heads(rels)
	ri := newReachIndex(scans, rels)
	for ti := range out.Threads {
		t := &out.Threads[ti]
		for ri2 := range t.Repos {
			cs := t.Repos[ri2].Commits
			for ci := range cs {
				cs[ci].Reached = ri.reached(cs[ci])
			}
		}
		summarize(t)
	}
}
