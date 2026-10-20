package agentrtapi

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
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

// pruneReadBudget bounds one answer's reads of Gitea, all repositories
// together; a repository not read in it is passed over this time.
const pruneReadBudget = time.Minute

// prunes is GET /prunes?host_id=: the branches the rule lets go now, or none
// while a refusal's pause holds (paused_until says until when). A repository
// whose branches or main cannot be read now is passed over and named in
// unread; the others are answered (review 436 B3).
func (a *API) prunes(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host_id")
	if !boundHost(w, r, host) {
		return
	}
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
	ctx, cancel := context.WithTimeout(r.Context(), pruneReadBudget)
	defer cancel()
	var heads []store.AgentHead
	onMain := map[string]store.OnMain{}
	unread := []string{}
	for _, repo := range repos {
		hs, err := a.AgentHeads(ctx, repo)
		if err != nil {
			a.logf("agentrt prunes: %s: %v", repo, err)
			unread = append(unread, repo)
			continue
		}
		heads = append(heads, hs...)
		if a.MainResults != nil {
			if m, ok := a.MainResults(ctx, repo); ok {
				onMain[repo] = m
			}
		}
	}
	ps, err := store.Prunable(r.Context(), a.DB, heads, onMain, now)
	if err != nil {
		a.fail(w, err)
		return
	}
	if ps != nil {
		out["prunes"] = ps
	}
	if len(unread) > 0 {
		out["unread"] = unread
	}
	writeJSON(w, http.StatusOK, out)
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
	if len(b.Results) > store.MaxPrunes {
		writeError(w, http.StatusBadRequest, "bad_request", "more results than prunes are handed out at once")
		return
	}
	if err := store.RecordPrunes(r.Context(), a.DB, b.HostID, b.Results); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recorded": len(b.Results)})
}
