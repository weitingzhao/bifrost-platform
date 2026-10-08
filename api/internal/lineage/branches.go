package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	branchHorizon        = 60 * 24 * time.Hour // how far back main and branches are read
	branchMainMaxPages   = 60                  // 3,000 default-branch commits per repo
	branchWalkMaxPages   = 6                   // 300 commits per branch
	branchCommitsShown   = 30
	branchesCacheTTL     = 10 * time.Minute
	branchesBuildTimeout = 2 * time.Minute
)

// BranchThread is one agent thread with commits on a branch.
type BranchThread struct {
	Session     string   `json:"session"`
	Transcripts []string `json:"transcripts,omitempty"`
	Title       string   `json:"title,omitempty"`
}

// BranchHealth is one non-default branch measured against its repo's default branch.
type BranchHealth struct {
	Repo    string    `json:"repo"`
	Branch  string    `json:"branch"`
	HeadSHA string    `json:"head_sha"`
	HeadAt  time.Time `json:"head_at"`
	// Ahead: commits the branch has that the default branch does not (by commit identity).
	Ahead int `json:"ahead"`
	// Open: of those, changes not found on the default branch by Change-Id or subject
	// either — work that has not landed in any form.
	Open         int        `json:"open"`
	OldestOpenAt *time.Time `json:"oldest_open_at,omitempty"`
	// Behind: default-branch commits since the branch forked; BehindIsFloor when the
	// fork is older than what was read (Ahead and Open are then floors too).
	Behind        int    `json:"behind"`
	BehindIsFloor bool   `json:"behind_is_floor,omitempty"`
	ForkSHA       string `json:"fork_sha,omitempty"`
	// Status: "open" (unlanded changes), "landed" (every change is on the default
	// branch in another form — a leftover), "even" (nothing ahead — merged),
	// "stale" (the head is older than what was read and not on the default branch
	// in that span: not measured, so Ahead and Open are 0 and Behind is a floor).
	Status  string         `json:"status"`
	Threads []BranchThread `json:"threads"`
	// Commits: the branch's own commits, newest first (capped).
	Commits []GraphCommit `json:"commits"`
}

// BranchesResponse is GET /api/v1/lineage/branches.
type BranchesResponse struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Reachability probe.Reachability `json:"reachability"`
	Branches     []BranchHealth     `json:"branches"`
	Errors       []string           `json:"errors"`
}

// Branches measures every non-default branch of every repo against its default branch.
func (s *Service) Branches(ctx context.Context) BranchesResponse {
	now := s.now()
	out := BranchesResponse{GeneratedAt: now, Reachability: probe.ReachFail, Branches: []BranchHealth{}, Errors: []string{}}
	ctx, cancel := context.WithTimeout(ctx, branchesBuildTimeout)
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
	results := make([][]BranchHealth, len(repos))
	errs := make([][]string, len(repos))
	sem := make(chan struct{}, repoConcurrency)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r giteaRepo) {
			defer wg.Done()
			defer safego.Recover("lineage.branches")
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = measureRepo(ctx, g, r, now)
		}(i, r)
	}
	wg.Wait()
	for i := range repos {
		out.Branches = append(out.Branches, results[i]...)
		out.Errors = append(out.Errors, errs[i]...)
	}
	sort.SliceStable(out.Branches, func(i, j int) bool {
		a, b := out.Branches[i], out.Branches[j]
		if ra, rb := statusRank[a.Status], statusRank[b.Status]; ra != rb {
			return ra < rb
		}
		if a.OldestOpenAt != nil && b.OldestOpenAt != nil && !a.OldestOpenAt.Equal(*b.OldestOpenAt) {
			return a.OldestOpenAt.Before(*b.OldestOpenAt) // the longest-waiting work first
		}
		return a.HeadAt.Before(b.HeadAt)
	})
	switch {
	case len(out.Errors) == 0:
		out.Reachability = probe.ReachOK
	case len(out.Branches) > 0:
		out.Reachability = probe.ReachDegraded
	}
	return out
}

// statusRank orders the branch list: unlanded work first, then branches that
// could not be measured, then leftovers, then merged names.
var statusRank = map[string]int{"open": 0, "stale": 1, "landed": 2, "even": 3}

func measureRepo(ctx context.Context, g *gitea, r giteaRepo, now time.Time) ([]BranchHealth, []string) {
	brs, err := g.branches(ctx, r.Name)
	if err != nil {
		return nil, []string{r.Name + ": branches: " + err.Error()}
	}
	var others []giteaBranch
	for _, b := range brs {
		if b.Name != r.Default && b.SHA != "" {
			others = append(others, b)
		}
	}
	if len(others) == 0 {
		return nil, nil
	}
	// The default branch over the horizon: where branch walks stop (the fork),
	// how far each branch fell behind, and where its changes landed.
	since := now.Add(-branchHorizon)
	main, err := g.commits(ctx, r.Name, r.Default, since, branchMainMaxPages, nil)
	if err != nil {
		return nil, []string{r.Name + ": default branch: " + err.Error()}
	}
	mi := newMainIndex(repoScan{repo: r.Name, def: r.Default, main: main})
	pos := make(map[string]int, len(main))
	for i, c := range main {
		pos[c.SHA] = i
	}
	onMain := func(sha string) bool { _, ok := pos[sha]; return ok }

	var errs []string
	out := make([]BranchHealth, 0, len(others))
	for _, b := range others {
		h := BranchHealth{Repo: r.Name, Branch: b.Name, HeadSHA: b.SHA, HeadAt: b.At, Threads: []BranchThread{}, Commits: []GraphCommit{}}
		if !onMain(b.SHA) && b.At.Before(since) {
			// The head predates the horizon and is not on the default branch inside
			// it: the walk would stop at once and read as "nothing ahead". It may be
			// abandoned work or a merge older than the read; say so instead of "even".
			h.Status = "stale"
			h.Behind, h.BehindIsFloor = len(main), true
			out = append(out, h)
			continue
		}
		var ahead []giteaCommit
		if !onMain(b.SHA) {
			// walk the branch back until it meets the default branch: those are its own commits
			ahead, err = g.commits(ctx, r.Name, b.SHA, since, branchWalkMaxPages, onMain)
			if err != nil {
				errs = append(errs, r.Name+" "+b.Name+": "+err.Error())
				continue
			}
		}
		h.Ahead = len(ahead)
		threads := map[string]*BranchThread{}
		var order []string
		for _, gc := range ahead {
			subject, t := parseMessage(gc.Msg)
			dup, landedSHA, landedBy := mi.branchLanding(subject, t.changeID)
			if dup {
				landedBy = "change_id"
			}
			if landedBy == "" {
				h.Open++
				at := gc.At
				if h.OldestOpenAt == nil || at.Before(*h.OldestOpenAt) {
					h.OldestOpenAt = &at
				}
			}
			if t.session != "" {
				bt := threads[t.session]
				if bt == nil {
					bt = &BranchThread{Session: t.session}
					threads[t.session] = bt
					order = append(order, t.session)
				}
				if t.transcript != "" && !contains(bt.Transcripts, t.transcript) {
					bt.Transcripts = append(bt.Transcripts, t.transcript)
				}
			}
			if len(h.Commits) < branchCommitsShown {
				h.Commits = append(h.Commits, GraphCommit{SHA: gc.SHA, Subject: subject, At: gc.At, Session: t.session,
					ChangeID: t.changeID, Agent: t.session == "" && t.agent, LandedSHA: landedSHA, LandedBy: landedBy})
			}
		}
		for _, s := range order {
			h.Threads = append(h.Threads, *threads[s])
		}
		switch n := len(ahead); {
		case n == 0:
			h.ForkSHA = b.SHA // the head is on the default branch: merged
		case len(ahead[n-1].Parents) > 0:
			h.ForkSHA = ahead[n-1].Parents[0]
		}
		if i, ok := pos[h.ForkSHA]; ok {
			h.Behind = i
		} else {
			// forked before the horizon (or the walk hit its page cap)
			h.Behind, h.BehindIsFloor = len(main), true
		}
		switch {
		case h.Ahead == 0:
			h.Status = "even"
		case h.Open == 0:
			h.Status = "landed"
		default:
			h.Status = "open"
		}
		out = append(out, h)
	}
	return out, errs
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type branchesCache struct {
	mu   sync.Mutex
	resp *BranchesResponse
	at   time.Time
}

// HandleBranches is GET /api/v1/lineage/branches[?refresh=true]: every non-default
// branch with how far it is ahead of and behind its default branch, and how much
// of it has not landed. Cached 10 minutes.
func (h *Handler) HandleBranches(w http.ResponseWriter, r *http.Request) {
	h.branches.mu.Lock()
	cached, at := h.branches.resp, h.branches.at
	h.branches.mu.Unlock()
	var resp BranchesResponse
	if cached != nil && r.URL.Query().Get("refresh") != "true" && time.Since(at) < branchesCacheTTL {
		resp = *cached
	} else {
		resp = h.svc.Branches(r.Context())
		if len(resp.Branches) > 0 || len(resp.Errors) == 0 {
			h.branches.mu.Lock()
			h.branches.resp, h.branches.at = &resp, time.Now()
			h.branches.mu.Unlock()
		}
	}
	if h.titles != nil {
		if tt, err := h.titles(r.Context()); err == nil {
			resp = nameBranches(resp, tt)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// nameBranches copies resp with thread titles (manual first, else latest transcript title).
func nameBranches(resp BranchesResponse, tt ThreadTitles) BranchesResponse {
	brs := make([]BranchHealth, len(resp.Branches))
	for i, b := range resp.Branches {
		ths := make([]BranchThread, len(b.Threads))
		for j, t := range b.Threads {
			named := name(Response{Threads: []Thread{{Session: t.Session, Transcripts: t.Transcripts}}}, tt).Threads[0]
			t.Title = named.Title
			ths[j] = t
		}
		b.Threads = ths
		brs[i] = b
	}
	resp.Branches = brs
	return resp
}
