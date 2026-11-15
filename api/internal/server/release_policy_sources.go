package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// What the release policy's newer sections read (Owner 2026-11-14): the plan
// of an apply_manifest request, the commits a release brings in, and the
// review records of a commit. Each fails rather than guessing, and a source
// that is not connected is an error, so the policy covers nothing then.

// policyPlans reads a plan through the service that will apply it, so the
// policy judges the objects Apply would write.
type policyPlans struct {
	summarize func(ctx context.Context, planID string) (workactions.Summary, error)
}

func (p policyPlans) ApplyPlan(ctx context.Context, planID string) (releasepolicy.ApplyPlan, error) {
	if p.summarize == nil {
		return releasepolicy.ApplyPlan{}, errors.New("the manifest actions are not wired")
	}
	sum, err := p.summarize(ctx, planID)
	if err != nil {
		return releasepolicy.ApplyPlan{}, err
	}
	out := releasepolicy.ApplyPlan{Ready: sum.Ready, Policy: sum.Policy, Repo: sum.Repo, Path: sum.Path, Commit: sum.Commit}
	for _, o := range sum.Objects {
		out.Objects = append(out.Objects, releasepolicy.ApplyObject{
			Namespace: o.Namespace, Kind: o.Kind, Name: o.Name, Ownership: o.Ownership, AppliedBefore: o.AppliedBefore,
		})
	}
	return out, nil
}

// policyHistory lists a release's commits from the mirror.
type policyHistory struct{ facts delivery.PolicyFacts }

func (h policyHistory) Commits(ctx context.Context, repo, old, next string) ([]releasepolicy.Commit, error) {
	listed, err := h.facts.CommitsBetween(ctx, repo, old, next)
	if err != nil {
		return nil, err
	}
	out := make([]releasepolicy.Commit, 0, len(listed))
	for _, c := range listed {
		out = append(out, releasepolicy.Commit{SHA: c.SHA, Message: c.Message, Files: c.Files})
	}
	return out, nil
}

// policyReviews reads review records from the evidence ledger.
type policyReviews struct{ db func() store.DB }

func (p policyReviews) Reviews(ctx context.Context, repo, commit string) ([]releasepolicy.Review, error) {
	d := p.db()
	if d == nil {
		return nil, errors.New("the evidence ledger is not connected")
	}
	rows, err := evidence.Reviews(ctx, d, repo, commit)
	if err != nil {
		return nil, err
	}
	out := make([]releasepolicy.Review, 0, len(rows))
	for _, r := range rows {
		out = append(out, releasepolicy.Review{Reviewer: r.Reviewer, Files: r.Files, Passed: r.Passed, NotRead: r.NotRead, At: r.RecordedAt})
	}
	return out, nil
}

func (s *Server) evidenceDB() store.DB {
	if s.agentRuntime == nil || s.agentRuntime.api == nil {
		return nil
	}
	return s.agentRuntime.api.DB
}

// POST /api/v1/release-policy/reviews {repo, commit, files, verdict,
// not_read}: record that the calling thread read these files of the
// repository at this commit, and what it concluded (operator, tier B). The
// reviewer is the caller's session, never a field of the body: the release
// policy compares it with the sessions that wrote the commits. The record
// is one appended row of the evidence ledger; recording again gives the
// reviewer a newer verdict.
func (s *Server) handleRecordReleaseReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo    string   `json:"repo"`
		Commit  string   `json:"commit"`
		Files   []string `json:"files"`
		Verdict string   `json:"verdict"`
		NotRead string   `json:"not_read"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is {repo, commit, files, verdict, not_read}"})
		return
	}
	repo, commit, verdict := strings.TrimSpace(body.Repo), strings.TrimSpace(body.Commit), strings.TrimSpace(body.Verdict)
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	if verdict != evidence.EvidencePassed && verdict != evidence.EvidenceFailed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "verdict is passed or failed"})
		return
	}
	reviewer := strings.TrimSpace(r.Header.Get(approvals.SessionHeader))
	if !actor.IsSession(reviewer) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a review is recorded as the calling thread: the request needs its " + approvals.SessionHeader + " header"})
		return
	}
	row, err := evidence.ReviewRow(evidence.Review{Repo: repo, Commit: commit, Reviewer: reviewer, Files: body.Files,
		Passed: verdict == evidence.EvidencePassed, NotRead: body.NotRead}, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	db := s.evidenceDB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	n, err := evidence.RecordEvidence(ctx, db, []evidence.EvidenceRow{row})
	if err == nil && n != 1 {
		err = errors.New("the ledger did not take the record")
	}
	if s.audit != nil {
		result, detail := verdict, fmt.Sprintf("reviewer=%s files=%d", reviewer, len(body.Files))
		if err != nil {
			result, detail = "failed", detail+" error="+err.Error()
		}
		s.audit.Record(r, "release_policy.review", repo+"@"+commit, result, detail)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recorded": true, "repo": repo, "commit": commit, "reviewer": reviewer, "verdict": verdict, "files": len(body.Files),
	})
}
