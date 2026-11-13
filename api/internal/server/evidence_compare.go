package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// GET /api/v1/evidence/compare puts the evidence ledger's answer beside the
// release policy's CI reading (delivery.PolicyFacts.CIIndex), for every
// commit that reading has a verdict for (CI design v4, landing wave 1 block
// F). Read-only: it decides nothing and writes nothing.
//
// For each commit: old is CIIndex's status and run; verdict is the ledger's
// answer now (commitEvidence: the commit's tree with the main trees beside
// it now); as_tested says whether the ledger has a full pass recorded for
// the commit itself, with the inputs its run had. The class names how the
// two differ:
//
//	agree             both green, or neither
//	beside_moved      old green, the ledger has the full pass, but a
//	                  repository beside it moved since: its key changed
//	not_in_ledger     old green and the ledger has no full pass for the
//	                  commit: the ledger missed an input. This must be zero.
//	ledger_only       the ledger is green and old is not: the same content
//	                  passed under another commit, or old looks at another run
type evidenceComparison struct {
	Repo     string `json:"repo"`
	Commit   string `json:"commit"`
	Old      string `json:"old"`
	OldRun   string `json:"old_run"`
	Verdict  string `json:"verdict"`
	AsTested bool   `json:"as_tested"`
	Class    string `json:"class"`
	Note     string `json:"note,omitempty"`
}

type evidenceCompareAnswer struct {
	At      time.Time            `json:"at"`
	Counts  map[string]int       `json:"counts"`
	Commits []evidenceComparison `json:"commits"`
	// Skipped is the commits the old reading still has running, or that
	// could not be read from Gitea (a deleted branch), with why.
	Skipped []evidenceComparison `json:"skipped"`
}

func compareEvidence(ctx context.Context, db store.DB, index map[string]delivery.CIOutcome,
	tree func(context.Context, string, string) (string, error), main func(context.Context, string) (string, error)) evidenceCompareAnswer {
	out := evidenceCompareAnswer{At: time.Now().UTC(), Counts: map[string]int{}, Commits: []evidenceComparison{}, Skipped: []evidenceComparison{}}
	// One read of each tree and each main head for the whole comparison.
	type read struct {
		value string
		err   error
	}
	trees, mains := map[string]read{}, map[string]read{}
	cachedTree := func(ctx context.Context, repo, commit string) (string, error) {
		r, ok := trees[repo+"@"+commit]
		if !ok {
			r.value, r.err = tree(ctx, repo, commit)
			trees[repo+"@"+commit] = r
		}
		return r.value, r.err
	}
	cachedMain := func(ctx context.Context, repo string) (string, error) {
		r, ok := mains[repo]
		if !ok {
			r.value, r.err = main(ctx, repo)
			mains[repo] = r
		}
		return r.value, r.err
	}
	keys := make([]string, 0, len(index))
	for k := range index {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		repo, commit, _ := strings.Cut(k, "@")
		o := index[k]
		c := evidenceComparison{Repo: repo, Commit: commit, Old: o.Status, OldRun: o.Run}
		if o.Status == delivery.CIRunning || !giteaName.MatchString(repo) || !commitID.MatchString(commit) {
			c.Note = "the old reading has no verdict yet, or names no commit"
			out.Skipped = append(out.Skipped, c)
			continue
		}
		answer, err := commitEvidence(ctx, db, cachedTree, cachedMain, repo, commit)
		if err != nil {
			c.Note = err.Error()
			out.Skipped = append(out.Skipped, c)
			continue
		}
		c.Verdict = answer.Verdict
		for _, row := range answer.CommitRows {
			if row.Result == store.EvidencePassed && row.Scope == store.EvidenceScopeFull {
				c.AsTested = true
			}
		}
		oldGreen, newGreen := o.Status == delivery.CISucceeded, answer.Verdict == store.EvidenceGreen
		switch {
		case oldGreen == newGreen:
			c.Class = "agree"
		case oldGreen && c.AsTested:
			c.Class = "beside_moved"
		case oldGreen:
			c.Class = "not_in_ledger"
			if len(answer.CommitRows) == 0 {
				c.Note = "the ledger has no row for this commit"
			}
		default:
			c.Class = "ledger_only"
		}
		out.Counts[c.Class]++
		out.Commits = append(out.Commits, c)
	}
	return out
}

func (s *Server) handleEvidenceCompare(w http.ResponseWriter, r *http.Request) {
	main := s.agentRuntimeMains()
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil || main == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	index, err := s.delivery.PolicyFacts().CIIndex(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "CI runs: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, compareEvidence(ctx, s.agentRuntime.api.DB, index,
		giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), main))
}
