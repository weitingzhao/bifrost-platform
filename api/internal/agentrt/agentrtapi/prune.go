package agentrtapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Cleaning attempt branches (TD-383; store/prune.go has the rule). agentd
// asks which branches its host may delete (GET /prunes), deletes each with
// its own Gitea identity while it still names the commit given, and says
// what happened (POST /prunes). Until the Owner lifts Gitea's protection of
// agent/**, Gitea refuses: the refusal is recorded and the handing out
// pauses (store.PrunePause); nothing is an error for it.

// AgentHeads reads a repository's attempt branches on Gitea; nil hands out
// nothing.
type AgentHeads func(ctx context.Context, repo string) ([]store.AgentHead, error)

// pruneReadTimeout bounds each repository's read of Gitea, its branches or
// its main, on its own: a slow repository holds up no other (review 437 B3).
// pruneCollect bounds all the reads of one answer together, the branches
// within pruneBranches of it: what is read by then is answered, the rest
// named in unread, well within the 40 s agentd waits for the answer (review
// 438 B3). pruneReaders is how many repositories are read at once.
var (
	pruneReadTimeout = 20 * time.Second
	pruneCollect     = 30 * time.Second
	pruneBranches    = 20 * time.Second
)

const pruneReaders = 4

// unreadRepo is a repository passed over this time, and which read failed.
type unreadRepo struct {
	Repo string `json:"repo"`
	What string `json:"what"` // branches, main
}

// prunes is GET /prunes?host_id=: the branches the rule lets go now, or none
// while a refusal's pause holds (paused_until says until when). Each
// repository's branches are read, then main where it decides something
// (store.PruneMainsNeeded), each read on its own clock; a repository whose
// read fails is named in unread and keeps what that read would decide.
func (a *API) prunes(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host_id")
	if !boundHost(w, r, host) {
		return
	}
	// The host is in contact while it waits for its list (review 526 A1):
	// the reads of Gitea may take most of pruneCollect.
	defer a.keepTouching(r.Context(), host)()
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	out := map[string]any{"prunes": []store.Prune{}}
	if a.AgentHeads == nil {
		out["why"] = "the platform has no reader for the repositories' branches"
		writeJSON(w, http.StatusOK, out)
		return
	}
	until, err := store.PrunePausedUntil(r.Context(), a.DB, now)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !until.IsZero() {
		out["paused_until"] = until.UTC().Format(time.RFC3339)
		out["why"] = "Gitea refused a deletion, or could not be reached, lately (agent/** may still be protected)"
		writeJSON(w, http.StatusOK, out)
		return
	}
	repos, err := store.PruneRepos(r.Context(), a.DB)
	if err != nil {
		a.fail(w, err)
		return
	}
	unread := []unreadRepo{}
	collect, cancel := context.WithTimeout(r.Context(), pruneCollect)
	defer cancel()
	branchesBy, cancelBranches := context.WithTimeout(collect, pruneBranches)
	defer cancelBranches()
	branches := readEach(branchesBy, repos, func(ctx context.Context, repo string) ([]store.AgentHead, bool) {
		hs, err := a.AgentHeads(ctx, repo)
		if err != nil {
			a.logf("agentrt prunes: branches of %s: %v", repo, err)
		}
		return hs, err == nil
	})
	var heads []store.AgentHead
	for _, repo := range repos {
		if hs, ok := branches[repo]; ok {
			heads = append(heads, hs...)
		} else {
			unread = append(unread, unreadRepo{repo, "branches"})
		}
	}
	need, err := store.PruneMainsNeeded(r.Context(), a.DB, heads, now)
	if err != nil {
		a.fail(w, err)
		return
	}
	onMain := map[string]store.OnMain{}
	if a.MainResults != nil {
		onMain = readEach(collect, need, func(ctx context.Context, repo string) (store.OnMain, bool) {
			return a.MainResults(ctx, repo)
		})
	}
	for _, repo := range need {
		if _, ok := onMain[repo]; !ok {
			unread = append(unread, unreadRepo{repo, "main"})
		}
	}
	ps, err := store.Prunable(r.Context(), a.DB, heads, onMain, now)
	if err != nil {
		a.fail(w, err)
		return
	}
	if ps != nil {
		out["prunes"] = ps
		// agentd deletes them before it claims again: its host may be silent
		// for its whole prune budget (store.PruneSilence).
		if err = store.OpenPruneRound(r.Context(), a.DB, host, len(ps)); err != nil {
			a.fail(w, err)
			return
		}
	}
	if len(unread) > 0 {
		out["unread"] = unread
	}
	writeJSON(w, http.StatusOK, out)
}

// readEach reads each repository, pruneReaders at a time, each within
// pruneReadTimeout; the map holds the reads that succeeded.
func readEach[T any](ctx context.Context, repos []string, read func(context.Context, string) (T, bool)) map[string]T {
	out := map[string]T{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, pruneReaders)
	for _, repo := range repos {
		wg.Add(1)
		slots <- struct{}{}
		safego.Go("agentrt-prunes-read", func() {
			defer wg.Done()
			defer func() { <-slots }()
			rctx, cancel := context.WithTimeout(ctx, pruneReadTimeout)
			defer cancel()
			if v, ok := read(rctx, repo); ok && rctx.Err() == nil {
				mu.Lock()
				out[repo] = v
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return out
}

type pruneReport struct {
	HostID  string              `json:"host_id"`
	Results []store.PruneResult `json:"results"`
}

// reportPrunes is POST /prunes: what the host did with what it was given.
func (a *API) reportPrunes(w http.ResponseWriter, r *http.Request) {
	var b pruneReport
	if !decode(w, r, &b) || !boundHost(w, r, b.HostID) {
		return
	}
	// The report ends the host's prune round, whatever is made of it
	// (review 526 A1); a round that cannot be closed now ends at its budget.
	defer a.keepTouching(r.Context(), b.HostID)()
	if err := store.ClosePruneRound(r.Context(), a.DB, b.HostID, "it reported what it deleted"); err != nil && r.Context().Err() == nil {
		a.logf("agentrt prunes: the prune round of host %s could not be closed: %v", b.HostID, err)
	}
	if len(b.Results) > store.MaxPrunes {
		writeError(w, http.StatusBadRequest, "bad_request", "more results than prunes are handed out at once")
		return
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	if err := store.RecordPrunes(r.Context(), a.DB, b.HostID, b.Results, now); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recorded": len(b.Results)})
}

// touchEvery is how often a host's call that is still being answered counts
// as its contact.
const touchEvery = 10 * time.Second

// keepTouching records host's contact now and every touchEvery until the
// returned function is called, and once more then.
func (a *API) keepTouching(ctx context.Context, host string) func() {
	a.touch(ctx, host)
	done := make(chan struct{})
	stopped := make(chan struct{})
	safego.Go("agentrt-touch", func() {
		defer close(stopped)
		t := time.NewTicker(touchEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				a.touch(ctx, host)
			}
		}
	})
	return func() {
		close(done)
		<-stopped
		a.touch(context.WithoutCancel(ctx), host)
	}
}
