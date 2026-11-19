package evidenceread

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// ScopeSources is what one request of the scope route reads.
type ScopeSources struct {
	DB    eventlog.DB
	Tree  func(ctx context.Context, repo, commit string) (string, error)
	Trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)
}

// ScopeRoute is GET /api/v1/units/scope?repo=…&commit=…&base=…: which modules
// of a commit CI has to verify again against a base commit (unitplan.ScopeOf
// has the answer and why; format=text is unitplan.ScopeText). Read-only. No
// token, as /evidence/ci beside it: the in-cluster task that decides what a CI
// run runs holds no platform credential, and the answer names only what the
// repository's own declaration names. The checks of a request's names are the
// caller's; Sources says, per request, what to read.
type ScopeRoute struct {
	RepoOK, CommitOK func(string) bool
	Sources          func() (ScopeSources, bool)
}

func (c ScopeRoute) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (c ScopeRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, commit, base := strings.TrimSpace(q.Get("repo")), strings.TrimSpace(q.Get("commit")), strings.TrimSpace(q.Get("base"))
	if !c.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !c.CommitOK(commit) || (base != "" && !c.CommitOK(base)) {
		c.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; commit and base are 40-character lowercase SHAs (base may be left out)"})
		return
	}
	src, ok := c.Sources()
	if !ok {
		c.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	baseCI := LedgerCIScoped(func() eventlog.DB { return src.DB }, src.Tree, false)
	out, err := unitplan.ScopeOf(ctx, src.Trees, unitplan.BaseCIFunc(baseCI), repo, commit, base)
	if err != nil {
		c.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if q.Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(unitplan.ScopeText(out)))
		return
	}
	c.writeJSON(w, http.StatusOK, out)
}
