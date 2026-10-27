package lineage

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
)

// PlanHistoryReader answers, for the plan completion atoms, whether a task's
// accepted result is effective on main (or on a pinned snapshot of it). It
// keeps each repo's main commits between passes and reads only what is new:
// a pass over 50 bindings is a head read per repo, not 50 full histories
// (PV2 review 2026-10-26: about 30 requests per binding every 15 seconds).
//
// A result is read from the organization agent branches are pushed to
// (Access.AgentOrg) first: a replayed result never exists in the delivery
// organization, which mirrors GitHub and takes no push; then from the
// delivery organization, where a result pushed straight to main lives.
type PlanHistoryReader struct {
	Access   AccessFunc
	Client   *http.Client
	MaxPages int
	// HeadTTL is how long a read of main's head serves later bindings of the
	// same pass (default 30s).
	HeadTTL time.Duration
	Now     func() time.Time

	mu      sync.Mutex
	repos   map[string]*repoMain
	results map[string]string // repo@sha → message; a commit's message never changes
}

type repoMain struct {
	head    string
	headAt  time.Time
	commits map[string]giteaCommit
}

// History implements store.PlanSources.History.
func (r *PlanHistoryReader) History(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
	unknown := landing.MainHistory{State: landing.Unknown}
	if r.MaxPages < 1 {
		return unknown, errors.New("history page budget missing")
	}
	acc, err := r.Access(ctx)
	if err != nil {
		return unknown, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.repos == nil {
		r.repos, r.results = map[string]*repoMain{}, map[string]string{}
	}
	g := &gitea{acc: acc, client: r.Client}
	m := r.repos[repo]
	if m == nil {
		m = &repoMain{commits: map[string]giteaCommit{}}
		r.repos[repo] = m
	}
	if snapshot == "" {
		if snapshot, err = r.head(ctx, g, repo, m); err != nil {
			return unknown, err
		}
	}
	history, complete := ancestry(m.commits, snapshot)
	if !complete {
		// New commits only: stop at the first one already held. A merged
		// branch's older commits can follow a held one in the listing, so an
		// ancestry still incomplete after that is read again in full.
		known := func(sha string) bool { _, ok := m.commits[sha]; return ok }
		if err = r.fetch(ctx, g, repo, snapshot, m, known); err != nil {
			return unknown, err
		}
		if history, complete = ancestry(m.commits, snapshot); !complete {
			if err = r.fetch(ctx, g, repo, snapshot, m, nil); err != nil {
				return unknown, err
			}
			history, complete = ancestry(m.commits, snapshot)
		}
	}
	if len(history) == 0 || history[0].SHA != snapshot {
		return unknown, errors.New("snapshot unavailable")
	}
	msg, err := r.result(ctx, acc, repo, result)
	if err != nil {
		// An attempt branch is deleted once landed (TD-383) and its commit is
		// collected later. Recovery requires provenance and the independently
		// bound task; a candidate's own Change-Id cannot prove its identity.
		var ok bool
		if msg, ok = pickedResult(history, task, result); !ok {
			return unknown, err
		}
	}
	h, err := landinggit.EvaluateHistory(task, result, msg, history, complete)
	h.Snapshot = snapshot
	return h, err
}

func (r *PlanHistoryReader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *PlanHistoryReader) head(ctx context.Context, g *gitea, repo string, m *repoMain) (string, error) {
	ttl := r.HeadTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if m.head != "" && r.now().Sub(m.headAt) < ttl {
		return m.head, nil
	}
	var head struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := g.get(ctx, g.repoPath(repo)+"/branches/main", nil, &head); err != nil {
		return "", err
	}
	if head.Commit.ID == "" {
		return "", errors.New("main unavailable")
	}
	m.head, m.headAt = head.Commit.ID, r.now()
	return m.head, nil
}

// fetch lists commits reachable from ref into m; a partial read keeps what it
// got (ancestry decides whether it is enough).
func (r *PlanHistoryReader) fetch(ctx context.Context, g *gitea, repo, ref string, m *repoMain, stop func(string) bool) error {
	commits, err := g.commits(ctx, repo, ref, time.Time{}, r.MaxPages, stop)
	for _, c := range commits {
		m.commits[c.SHA] = c
	}
	return err
}

// result is the accepted result's message, from the agent organization first.
func (r *PlanHistoryReader) result(ctx context.Context, acc Access, repo, sha string) (string, error) {
	key := repo + "@" + sha
	if msg, ok := r.results[key]; ok {
		return msg, nil
	}
	orgs := []string{}
	if acc.AgentOrg != "" {
		orgs = append(orgs, acc.AgentOrg)
	}
	if acc.Org != acc.AgentOrg {
		orgs = append(orgs, acc.Org)
	}
	g := &gitea{acc: acc, client: r.Client}
	for _, org := range orgs {
		var c struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		err := g.get(ctx, "/repos/"+url.PathEscape(org)+"/"+url.PathEscape(repo)+"/git/commits/"+url.PathEscape(sha), nil, &c)
		if err == nil && c.SHA == sha {
			r.results[key] = c.Commit.Message
			return c.Commit.Message, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", errors.New("result unavailable")
}

// pickedResult recovers only the independently bound task. Never borrow a
// candidate's Change-Id and then match that candidate against itself.
func pickedResult(history []landinggit.HistoryCommit, task, result string) (string, bool) {
	if task == "" {
		return "", false
	}
	msg, found := "", false
	for _, c := range history {
		if landinggit.CherryPickSource(c.Message) != result {
			continue
		}
		if !landinggit.HasTaskTrailer(c.Message, task) {
			return "", false
		}
		if found && c.Message != msg {
			return "", false
		}
		msg, found = c.Message, true
	}
	return "Recovered result\n\nAgentrt-Task: " + task, found
}

// ancestry is the commits reachable from tip, tip first, and whether every
// parent on the way is held and the history has exactly one root (a reader
// that dropped parents would otherwise look complete).
func ancestry(commits map[string]giteaCommit, tip string) ([]landinggit.HistoryCommit, bool) {
	if _, ok := commits[tip]; !ok {
		return nil, false
	}
	out := []landinggit.HistoryCommit{}
	seen := map[string]bool{tip: true}
	queue := []string{tip}
	roots := 0
	for len(queue) > 0 {
		sha := queue[0]
		queue = queue[1:]
		c, ok := commits[sha]
		if !ok {
			return out, false
		}
		out = append(out, landinggit.HistoryCommit{SHA: c.SHA, Message: c.Msg, Parents: c.Parents})
		if len(c.Parents) == 0 {
			roots++
		}
		for _, p := range c.Parents {
			if !seen[p] {
				seen[p] = true
				queue = append(queue, p)
			}
		}
	}
	return out, roots == 1
}
