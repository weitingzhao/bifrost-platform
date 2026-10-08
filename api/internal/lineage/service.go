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
	// clock is wall time for the mirror sync (now may be pinned in tests).
	clock        func() time.Time
	mirrors      mirrorThrottle
	mirrorEvery  time.Duration
	mirrorSettle time.Duration
	mirrorPoll   time.Duration
}

func NewService(access AccessFunc) *Service {
	return &Service{
		access: access,
		client: &http.Client{Timeout: 15 * time.Second},
		now:    func() time.Time { return time.Now().UTC() },
		clock:  time.Now,

		mirrorEvery:  mirrorEvery,
		mirrorSettle: mirrorSettle,
		mirrorPoll:   mirrorPoll,
	}
}

type repoScan struct {
	repo     string
	def      string
	mirrored *time.Time
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
	repos, out.MirrorSync = s.refreshMirrors(ctx, g, repos)

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
	rels := s.annotateReach(ctx, &out, scans)
	out.Graph = buildGraph(scans, rels)

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
	if r.Mirror && r.MirrorUpdated.Year() > 1970 {
		at := r.MirrorUpdated.UTC()
		sc.mirrored = &at
	}
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
	cov := RepoCoverage{Repo: sc.repo, MainCommits: len(sc.main), Branches: len(sc.branches), MirrorUpdated: sc.mirrored}
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
	mi := newMainIndex(sc)
	for _, gc := range sc.main {
		seen[gc.SHA] = true
		subject, t := parseMessage(gc.Msg)
		if t.session != "" {
			cov.WithSession++
		} else if t.agent {
			cov.AgentNoLineage++
		}
		if t.changeID != "" {
			cov.WithChangeID++
		}
		if t.session == "" && t.changeID == "" {
			continue
		}
		add(Commit{Repo: sc.repo, SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
			Transcript: t.transcript, ChangeID: t.changeID, Work: t.work, Ref: sc.def, Landed: true, LandedBy: "sha"})
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
			dup, landedSHA, landedBy := mi.branchLanding(subject, t.changeID)
			if dup {
				continue // the same change, already listed from main
			}
			add(Commit{Repo: sc.repo, SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
				Transcript: t.transcript, ChangeID: t.changeID, Work: t.work, Ref: b,
				Landed: landedBy != "", LandedSHA: landedSHA, LandedBy: landedBy})
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
func (s *Service) annotateReach(ctx context.Context, out *Response, scans []repoScan) []Release {
	if s.releases == nil {
		return nil
	}
	rels, err := s.releases(ctx)
	if err != nil {
		out.ReleasesError = err.Error()
		return nil
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
	return rels
}

// mainIndex answers, for a branch commit, whether and how its change reached the default branch.
type mainIndex struct {
	trailerCID map[string]bool   // Change-Id trailers of default-branch commits
	anyCID     map[string]string // Change-Id anywhere in a default-branch message -> that sha
	subject    map[string]string // subject -> newest default-branch sha with it
}

func newMainIndex(sc repoScan) mainIndex {
	mi := mainIndex{trailerCID: map[string]bool{}, anyCID: map[string]string{}, subject: map[string]string{}}
	for _, gc := range sc.main {
		subject, t := parseMessage(gc.Msg)
		if _, ok := mi.subject[subject]; !ok {
			mi.subject[subject] = gc.SHA
		}
		for _, cid := range changeIDsAnywhere(gc.Msg) {
			if _, ok := mi.anyCID[cid]; !ok {
				mi.anyCID[cid] = gc.SHA
			}
		}
		if t.changeID != "" {
			mi.trailerCID[t.changeID] = true
		}
	}
	return mi
}

// branchLanding: dup when a default-branch commit carries the same Change-Id
// trailer (the same change, listed from there); otherwise landedBy is
// "change_id" (folded into a squash), "subject" (no Change-Id, pre-hook
// heuristic) or "" (not landed).
func (mi mainIndex) branchLanding(subject, changeID string) (dup bool, landedSHA, landedBy string) {
	if changeID != "" {
		if mi.trailerCID[changeID] {
			return true, "", ""
		}
		if sha, ok := mi.anyCID[changeID]; ok {
			return false, sha, "change_id"
		}
		return false, "", ""
	}
	if sha, ok := mi.subject[subject]; ok {
		// before the hooks: rebased lanes keep their subject (prune-merged-branches.sh does the same)
		return false, sha, "subject"
	}
	return false, "", ""
}
