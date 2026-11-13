package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The switch of "is this commit's CI green" (CI design v4, landing wave 1
// blocks G and H): the evidence ledger, or the retained ci-* runs as before.
//
// EVIDENCE_GATE=ledger or =runs says which; unset, evidenceGateDefault holds
// for this platform's AGENTRT_ENV. With the ledger, the release policy's
// ci_succeeded (delivery.PolicyFacts.CIRun) and the runtime's CI source read
// ledgerCI; GET /api/v1/evidence/compare keeps reading both, so the two are
// compared after the switch as before it. To go back: EVIDENCE_GATE=runs, or
// the default below, and a restart.
const evidenceGateEnv = "EVIDENCE_GATE"

// evidenceGateDefault is the environments whose platform asks the ledger
// when EVIDENCE_GATE is unset: STG first; PROD joins once STG has been read.
var evidenceGateDefault = map[string]bool{"stg": true}

func evidenceGateOn(env string) bool {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(evidenceGateEnv))); v {
	case "ledger":
		return true
	case "runs":
		return false
	case "":
	default:
		slog.Error("evidence gate: "+evidenceGateEnv+" is neither ledger nor runs; the default holds", "value", v)
	}
	return evidenceGateDefault[env]
}

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
func ledgerCI(db func() store.DB, tree func(context.Context, string, string) (string, error)) delivery.CIReader {
	return ledgerCIScoped(db, tree, false)
}

// ledgerCIScoped is ledgerCI; with anyScope a pass of part of the repository
// (an incremental or a console run) counts as well when no full run failed
// after it, as the push to main reads CI (ci_gate.py without --require-full).
func ledgerCIScoped(db func() store.DB, tree func(context.Context, string, string) (string, error), anyScope bool) delivery.CIReader {
	verdict := func(ctx context.Context, d store.DB, rows []store.EvidenceRow) (string, string, error) {
		failed, part := "", ""
		for _, row := range rows { // newest first
			switch {
			case row.Result == store.EvidencePassed && row.Scope != store.EvidenceScopeFull:
				if part == "" && failed == "" {
					part = row.RunName
				}
			case row.Result == store.EvidencePassed && row.Scope == store.EvidenceScopeFull:
				same, err := store.EvidenceByKey(ctx, d, row.Repo, row.Unit, row.Task, row.InputKey)
				if err != nil {
					return "", "", err
				}
				if store.EvidenceVerdict(same) == store.EvidenceGreen {
					return delivery.CISucceeded, row.RunName, nil
				}
			case row.Result == store.EvidenceFailed && failed == "":
				failed = row.RunName
			}
		}
		// A full run decides (above, and a full failure here); a pass of part
		// counts only with anyScope, and not once a newer run failed.
		if anyScope && part != "" {
			return delivery.CISucceeded, part, nil
		}
		if failed != "" {
			return delivery.CIFailed, failed, nil
		}
		return "", "", nil
	}
	return func(ctx context.Context, repo, sha string) (string, string, error) {
		d := db()
		if d == nil {
			return "", "", errors.New("the evidence ledger is not connected")
		}
		rows, err := store.EvidenceByCommit(ctx, d, repo, sha, 50)
		if err != nil {
			return "", "", err
		}
		status, run, err := verdict(ctx, d, rows)
		if err != nil || status != "" || tree == nil {
			return status, run, err
		}
		t, err := tree(ctx, repo, sha)
		if err != nil {
			return "", "", nil
		}
		if rows, err = store.EvidenceByTree(ctx, d, repo, t, 50); err != nil {
			return "", "", err
		}
		return verdict(ctx, d, rows)
	}
}

// wireEvidenceGate turns the switch for this process and says so once.
func (s *Server) wireEvidenceGate(env string) delivery.CIReader {
	if s.delivery == nil {
		return nil
	}
	if !evidenceGateOn(env) {
		slog.Info("evidence gate: CI is read from the retained runs", "env", env)
		return nil
	}
	reader := ledgerCI(func() store.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}))
	s.delivery.SetCIReader(reader)
	slog.Info("evidence gate: CI is read from the evidence ledger", "env", env)
	return reader
}

// GET /api/v1/evidence/ci?repo=…&commit=…[&scope=any] is ledgerCI's answer
// for one commit: {repo, commit, scope, status, run}, status Succeeded,
// Failed or "" (no verdict the ledger knows). scope=any also counts a pass
// of part of the repository. It answers from the ledger whatever this
// platform's own switch says: the callers are the release scripts and the
// in-cluster gates (ci_gate.py), which have their own. It needs no token, as
// /releases and /health need none: those gates run in Pods that hold no
// platform credential, and the answer names only a repository, a commit and
// a CI run, all of which the public mirrors and /releases already show.
func (s *Server) handleEvidenceCI(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "full"
	}
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) || (scope != "full" && scope != "any") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name, commit a 40-character lowercase SHA, scope full or any"})
		return
	}
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	read := ledgerCIScoped(func() store.DB { return s.agentRuntime.api.DB },
		giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), scope == "any")
	status, run, err := read(ctx, repo, commit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"repo": repo, "commit": commit, "scope": scope, "status": status, "run": run})
}
