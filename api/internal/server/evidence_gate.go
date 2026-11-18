package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// "Is this commit's CI green" has one source in every environment: the
// evidence ledger (CI design v4, landing wave 1 blocks G and H). The release
// policy's ci_succeeded (delivery.PolicyFacts.CIRun) and the runtime's CI
// source read it through wireEvidenceGate. Until 2026-11-16 the retained
// ci-* runs were a second reading, chosen by a switch in the environment and
// the default in dev; it did not count an incremental pass, so it could no
// longer stand in for the ledger, and it is retired with the switch and the
// side-by-side route that compared the two. A platform with no ledger
// connected has no CI verdict: the reader answers an error, never a pass.

// ledgerCI answers "did CI pass for this commit" from the ledger: Succeeded
// when a full pass is recorded for the commit (with the commits beside it
// that run used) which no later row of the same inputs invalidated; else
// Failed when it has a failed row; else "" (no verdict the ledger knows: not
// tested, only partly tested, or still running). The run is the one the
// answer rests on. It asks what was recorded, not the key the commit would
// have now: a repository beside it moving on does not turn a tested commit
// untested (Owner 2026-11-12, until dependencies are pinned).
//
// A commit with no verdict of its own is asked again by its content: the rows
// of its git tree, under whatever commit (a rebase or a squash onto main, a
// run that reused another's result). tree reads a commit's tree; nil, or a
// tree that cannot be read, leaves the commit's own answer.
func ledgerCI(db func() eventlog.DB, tree func(context.Context, string, string) (string, error)) delivery.CIReader {
	return ledgerCIWith(db, tree, false, true)
}

// ledgerCIStrict is ledgerCI without the chain: only a full pass of the
// commit itself (by commit, then by tree) is Succeeded. It is what a release
// to PROD is judged on (the policy's ci_succeeded, the PROD pipeline's
// check-source through GET /api/v1/evidence/ci): a commit that landed on an
// incremental pass reaches STG on the chain, and PROD only once the full run
// of the push to main has passed (Owner 2026-11-16: the incremental run lets
// it in, the full run after it is the reconciliation).
func ledgerCIStrict(db func() eventlog.DB, tree func(context.Context, string, string) (string, error)) delivery.CIReader {
	return ledgerCIWith(db, tree, false, false)
}

// ledgerCIScoped is ledgerCI; with anyScope a pass of part of the repository
// (an incremental or a console run) counts as well when no full run failed
// after it, as the push to main reads CI (ci_gate.py without --require-full).
func ledgerCIScoped(db func() eventlog.DB, tree func(context.Context, string, string) (string, error), anyScope bool) delivery.CIReader {
	return ledgerCIWith(db, tree, anyScope, true)
}

// ledgerCIWith is the one reading of the ledger; chain says whether a passed
// incremental row counts together with its base's verdict (read below).
func ledgerCIWith(db func() eventlog.DB, tree func(context.Context, string, string) (string, error), anyScope, chain bool) delivery.CIReader {
	// verdict also returns chained: the base of the newest passed incremental
	// row that no newer failure follows, when no full run decided.
	verdict := func(ctx context.Context, d eventlog.DB, rows []evidence.EvidenceRow) (status, run, chained, chainedRun string, err error) {
		failed, part := "", ""
		for _, row := range rows { // newest first
			switch {
			case row.Result == evidence.EvidencePassed && row.Scope != evidence.EvidenceScopeFull:
				if part == "" && failed == "" {
					part = row.RunName
				}
				if base := evidence.IncrementalBase(row); base != "" && chained == "" && failed == "" {
					chained, chainedRun = base, row.RunName
				}
			case row.Result == evidence.EvidencePassed && row.Scope == evidence.EvidenceScopeFull:
				same, err := evidence.EvidenceByKey(ctx, d, row.Repo, row.Unit, row.Task, row.InputKey)
				if err != nil {
					return "", "", "", "", err
				}
				if evidence.EvidenceVerdict(same) == evidence.EvidenceGreen {
					return delivery.CISucceeded, row.RunName, "", "", nil
				}
			case row.Result == evidence.EvidenceFailed && failed == "":
				failed = row.RunName
			}
		}
		// A full run decides (above, and a full failure here); a pass of part
		// counts only with anyScope, and not once a newer run failed.
		if anyScope && part != "" {
			return delivery.CISucceeded, part, "", "", nil
		}
		if failed != "" {
			return delivery.CIFailed, failed, "", "", nil
		}
		return "", "", chained, chainedRun, nil
	}
	// read is one commit's verdict. With no verdict of its own (by commit, then
	// by tree) and a passed incremental row, it is the verdict of the commit
	// that row was judged against, as far back as maxVerdictChain commits: the
	// incremental run vouches for what changed, the base for the rest (Owner
	// 2026-11-16; before this a landing whose CI was incremental left main's
	// head without a verdict and STG undelivered, 2026-11-15 12:44-14:30). A
	// base that failed, or has no verdict, or a chain longer than the bound,
	// leaves the commit with none: the answer is never a pass on less.
	var read func(ctx context.Context, d eventlog.DB, repo, sha string, depth int) (string, string, error)
	read = func(ctx context.Context, d eventlog.DB, repo, sha string, depth int) (string, string, error) {
		rows, err := evidence.EvidenceCIByCommit(ctx, d, repo, sha, 50)
		if err != nil {
			return "", "", err
		}
		status, run, chained, chainedRun, err := verdict(ctx, d, rows)
		if err != nil || status != "" {
			return status, run, err
		}
		if tree != nil {
			if t, terr := tree(ctx, repo, sha); terr == nil {
				if rows, err = evidence.EvidenceByTree(ctx, d, repo, t, 50); err != nil {
					return "", "", err
				}
				var c, cr string
				if status, run, c, cr, err = verdict(ctx, d, rows); err != nil || status != "" {
					return status, run, err
				}
				if chained == "" {
					chained, chainedRun = c, cr
				}
			}
		}
		if !chain || chained == "" || chained == sha || depth >= maxVerdictChain {
			return "", "", nil
		}
		baseStatus, _, err := read(ctx, d, repo, chained, depth+1)
		if err != nil || baseStatus != delivery.CISucceeded {
			// A failed base is not this commit's failure: it has no verdict.
			return "", "", err
		}
		return delivery.CISucceeded, chainedRun, nil
	}
	return func(ctx context.Context, repo, sha string) (string, string, error) {
		d := db()
		if d == nil {
			return "", "", errors.New("the evidence ledger is not connected")
		}
		return read(ctx, d, repo, sha, 0)
	}
}

// maxVerdictChain bounds how many incremental landings in a row a verdict may
// rest on before a full pass is needed (the daily full CI is one every day).
const maxVerdictChain = 20

// wireEvidenceGate makes the ledger the delivery service's CI reader and
// returns it; nil when this process has no delivery service. The database is
// read when asked: before it connects, or without one, the answer is an error.
func (s *Server) wireEvidenceGate() delivery.CIReader {
	if s.delivery == nil {
		return nil
	}
	// Strict: this reader is the release policy's ci_succeeded.
	reader := ledgerCIStrict(func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}))
	s.delivery.SetCIReader(reader)
	slog.Info("evidence gate: CI is read from the evidence ledger")
	return reader
}

// GET /api/v1/evidence/ci?repo=…&commit=…[&scope=any|chain] is the ledger's
// answer for one commit: {repo, commit, scope, status, run}, status Succeeded,
// Failed or "" (no verdict the ledger knows). scope=any also counts a pass
// of part of the repository; scope=chain follows an incremental pass to its
// base (ledgerCI); without scope the answer is the strict one. The callers
// are the release scripts and the in-cluster gates (ci_gate.py). It needs no token, as
// /releases and /health need none: those gates run in Pods that hold no
// platform credential, and the answer names only a repository, a commit and
// a CI run, all of which the public mirrors and /releases already show.
func (s *Server) handleEvidenceCI(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "full"
	}
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) || (scope != "full" && scope != "any" && scope != "chain") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name, commit a 40-character lowercase SHA, scope full, any or chain"})
		return
	}
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// scope full (the default, what the PROD pipeline's check-source asks) is
	// strict; any also counts a pass of part; chain is what STG is delivered on.
	read := ledgerCIWith(func() eventlog.DB { return s.agentRuntime.api.DB },
		giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), scope == "any", scope == "chain")
	status, run, err := read(ctx, repo, commit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"repo": repo, "commit": commit, "scope": scope, "status": status, "run": run})
}
