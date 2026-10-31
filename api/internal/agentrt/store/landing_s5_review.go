package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Read the latest review, INCLUDING fail and unbound rows. An inner join or
// verdict='pass' predicate here would resurrect an earlier authorization.
func readS5Review(ctx context.Context, tx pgx.Tx, f landing.Facts) (landing.ReviewBinding, error) {
	b := landing.ReviewBinding{Repo: f.Repo, Task: f.Task, AcceptedAttempt: f.Attempt, A: f.A, Base: f.Base}
	var accepted, commit, host, hostKind string
	err := tx.QueryRow(ctx, `SELECT v.verification_id, COALESCE(v.attempt_id,''), COALESCE(a.pushed_commit,''),
 COALESCE(a.attempt_id,''), COALESCE(a.executor_actor_id,''), COALESCE(a.model_provider,''), COALESCE(a.host_id,''), COALESCE(h.host_kind,'')
 FROM agentrt.verification v LEFT JOIN agentrt.attempt a ON a.attempt_id=v.attempt_id AND a.task_id=v.task_id
 LEFT JOIN agentrt.host h ON h.host_id=a.host_id
 WHERE v.task_id=$1 AND v.accepts_attempt ORDER BY v.created_at DESC,v.verification_id DESC LIMIT 1`, f.Task).
		Scan(&b.AcceptanceVerificationID, &accepted, &commit, &b.ImplementerAttempt, &b.ImplementerExecutor, &b.ImplementerProvider, &host, &hostKind)
	if errors.Is(err, pgx.ErrNoRows) {
		b.Problem = "S5_REVIEW_BINDING"
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if accepted != f.Attempt || commit != f.A {
		b.Problem = "S5_REVIEW_BINDING"
	}
	if host == "" || hostKind == "" {
		b.Problem = "S5_REVIEW_PROVIDER"
	}
	if hostKind == HostExternal || f.ExecutorPin != "" {
		entry, e := entryOf(ctx, tx, f.Tenant, f.ExecutorPin, host)
		if e != nil || entry.Executor != b.ImplementerExecutor || entry.Provider != b.ImplementerProvider {
			b.Problem = "S5_REVIEW_PROVIDER"
		}
	}
	var target, subjectCommit, subjectTask, role, mission, reviewHost string
	var body []byte
	err = tx.QueryRow(ctx, `SELECT v.verification_id,COALESCE(v.attempt_id,''),COALESCE(v.subject_commit,''),v.verdict,
 COALESCE(v.evidence_artifact_id,''),COALESCE(a.task_id,''),COALESCE(a.attempt_id,''),COALESCE(a.executor_actor_id,''),COALESCE(a.model_provider,''),
 COALESCE(t.subject_task_id,''),COALESCE(t.role,''),COALESCE(t.mission_id,''),COALESCE(h.host_kind,''),COALESCE(b.body,''::bytea)
 FROM agentrt.verification v LEFT JOIN agentrt.attempt a ON a.attempt_id=v.verifier_attempt_id
 LEFT JOIN agentrt.task t ON t.task_id=a.task_id LEFT JOIN agentrt.host h ON h.host_id=a.host_id
 LEFT JOIN agentrt.artifact_blob b ON b.artifact_id=v.evidence_artifact_id
 WHERE v.task_id=$1 AND v.method='review' ORDER BY v.created_at DESC,v.verification_id DESC LIMIT 1`, f.Task).
		Scan(&b.ReviewVerificationID, &target, &subjectCommit, &b.Verdict, &b.ReviewArtifactID, &b.ReviewerTask, &b.ReviewerAttempt, &b.ReviewerExecutor, &b.ReviewerProvider, &subjectTask, &role, &mission, &reviewHost, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		b.Problem = "S5_REVIEW_MISSING"
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if target != f.Attempt || subjectCommit != f.A || subjectTask != f.Task || role != "review" || mission != f.Mission || b.Verdict != "pass" {
		b.Problem = "S5_REVIEW_BINDING"
	}
	if reviewHost == "" || reviewHost == HostExternal {
		b.Problem = "S5_REVIEW_PROVIDER"
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		b.Problem = "S5_REVIEW_MISSING"
	} else {
		b.ReviewArtifactSHA256 = landing.S5Hash(body)
		if b.ReviewArtifactID != "sha256:"+b.ReviewArtifactSHA256 {
			b.Problem = "S5_REVIEW_BINDING"
		}
	}
	return b, nil
}

func bindS5ReviewedPatch(f *landing.Facts) {
	f.S5Review.ReviewedPatchSHA256 = f.S5Patch.SourcePatchSHA256
	f.S5Review.ReviewedStablePatchID = f.S5Patch.SourceStablePatchID
}
