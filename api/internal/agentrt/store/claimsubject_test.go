package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-409 (STG M-93: two reviews of an implementation of effect none failed
// it for want of what it reported): a check's claim names the report the
// attempt it checks handed in and that task's effect class, its
// deterministic check's and its review's alike; an implementation's claim
// names neither.
func TestACheckClaimNamesTheReportItChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{Effect: "none"})
	first := w.run(t, "h1", cursor, "done")
	if first.SubjectReport != "" || first.SubjectEffect != "" {
		t.Fatalf("an implementation's claim names %q %q", first.SubjectReport, first.SubjectEffect)
	}
	var report string
	ok(t, w.rt.QueryRow(ctx, `SELECT report_artifact_id FROM agentrt.attempt WHERE attempt_id = $1`, first.AttemptID).Scan(&report))
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	vc := w.run(t, "h2", deterministic, "done")
	if vc.TaskID != v || vc.SubjectReport != report || vc.SubjectEffect != "none" {
		t.Fatalf("the verification's claim names %q %q, want %q none", vc.SubjectReport, vc.SubjectEffect, report)
	}
	concludeAs(t, w, v, "review")
	rc := w.run(t, "h3", codex, "done")
	if rc.TaskID != w.reviewOf(t, impl) || rc.SubjectReport != report || rc.SubjectEffect != "none" {
		t.Fatalf("the review's claim names %q %q, want %q none", rc.SubjectReport, rc.SubjectEffect, report)
	}
}

// reviewOf is the review task of impl, whatever its state.
func (w *world) reviewOf(t *testing.T, impl string) string {
	t.Helper()
	var id string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE subject_task_id = $1 AND role = 'review'`, impl).Scan(&id))
	return id
}
