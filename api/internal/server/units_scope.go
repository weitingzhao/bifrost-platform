package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// GET /api/v1/units/scope?repo=…&commit=…&base=… is which modules of a commit
// CI has to verify again against a base commit (declaration standard §2): a
// module whose tasks all keep the keys they had at the base does not run.
// The answer is computed from the two commits' declarations and trees; the
// platform holds no list of any project's modules. A commit with no
// declaration, a declaration that does not read, or a base that cannot be
// compared with answers "everything runs" and says why. So does a base the
// evidence ledger has no passing full CI for: a module that does not run is
// vouched for by the base's run (base_run), and a base that was red or never
// verified vouches for nothing. Read-only. format=text answers one line a
// module, "run <module>" or "same <module>", for a shell caller.
//
// No token, as /evidence/ci beside it: the in-cluster task that decides what
// a CI run runs holds no platform credential, and the answer names only what
// the repository's own declaration names.

// baseCIFunc is the ledger's word on a commit's full CI: Succeeded, Failed or
// "", and the run that says so.
type baseCIFunc func(ctx context.Context, repo, commit string) (status, run string, err error)

func unitScope(ctx context.Context, trees unitTreesFunc, baseCI baseCIFunc, repo, commit, base string) (unitplan.Scope, error) {
	pinned := unitplan.Source{Pinned: func(p, at string) (*unitplan.Tree, error) { return trees(ctx, p, at) }}
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return unitplan.Scope{}, err
	}
	if _, err = t.ReadFile(unitplan.ManifestPath); err != nil {
		return unitplan.WholeProject(repo, commit, base, "the commit has no "+unitplan.ManifestPath+": the project is one block", false, nil), nil
	}
	head, err := unitplan.Build(repo, commit, t, pinned)
	if err != nil {
		return unitplan.WholeProject(repo, commit, base, "the declaration does not read: "+err.Error(), true, nil), nil
	}
	if base == "" {
		return unitplan.WholeProject(repo, commit, base, "no base to compare with", true, head), nil
	}
	if base == commit {
		return unitplan.WholeProject(repo, commit, base, "the base is the commit itself", true, head), nil
	}
	bt, err := trees(ctx, repo, base)
	if err != nil {
		return unitplan.Scope{}, err
	}
	was, err := unitplan.Build(repo, base, bt, pinned)
	if err != nil {
		return unitplan.WholeProject(repo, commit, base, "the base has no declaration that reads: "+err.Error(), true, head), nil
	}
	status, run, err := baseCI(ctx, repo, base)
	if err != nil {
		return unitplan.Scope{}, err
	}
	if status != "Succeeded" {
		if status == "" {
			status = "none"
		}
		return unitplan.WholeProject(repo, commit, base, "the evidence ledger has no passing full CI of the base (it says "+status+"): nothing vouches for a module that would not run", true, head), nil
	}
	out := unitplan.Compare(was, head)
	out.BaseRun = run
	return out, nil
}

func (s *Server) handleUnitScope(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, commit, base := strings.TrimSpace(q.Get("repo")), strings.TrimSpace(q.Get("commit")), strings.TrimSpace(q.Get("base"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) || (base != "" && !commitID.MatchString(base)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; commit and base are 40-character lowercase SHAs (base may be left out)"})
		return
	}
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	baseCI := ledgerCIScoped(func() eventlog.DB { return s.agentRuntime.api.DB },
		giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), false)
	out, err := unitScope(ctx, s.unitTreesOf(), baseCIFunc(baseCI), repo, commit, base)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if q.Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(scopeText(out)))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// scopeText is the answer for a shell: "declared yes|no", then one line a
// module, "run <module>" or "same <module>", and a last line "end" so that a
// cut-off answer is not taken for a whole one. A commit that runs whole and
// names no module (no declaration) has no module lines.
func scopeText(s unitplan.Scope) string {
	var b strings.Builder
	if s.Declared {
		b.WriteString("declared yes\n")
	} else {
		b.WriteString("declared no\n")
	}
	for _, m := range s.Modules {
		if m.Run {
			b.WriteString("run " + m.Module + "\n")
		} else {
			b.WriteString("same " + m.Module + "\n")
		}
	}
	b.WriteString("end\n")
	return b.String()
}
