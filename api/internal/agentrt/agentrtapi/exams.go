package agentrtapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The chief exam over HTTP (W-31 2.4 ④; store/exam.go, the exam's cut §6
// 甲): agentd asks for a paper of a job's exam for one executor, runs it, and
// hands the answers in a part at a time; the platform grades. An agentd
// token only, for the host it is bound to, as a benchmark run is. Without a
// set (the platform reads it at start from AGENTRT_EXAM_DIR) both routes
// answer 503.

// examSet answers 503 when the platform has no exam set.
func (a *API) examSet(w http.ResponseWriter) bool {
	if a.Exams == nil {
		writeError(w, http.StatusServiceUnavailable, "no_exam_set",
			"no exam set is loaded: AGENTRT_EXAM_DIR is not set or its set did not load (the platform's start log says which)")
		return false
	}
	return true
}

// issueExam hands out a paper (GET /exams/{job}?host_id=…&executor=…).
func (a *API) issueExam(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !a.examSet(w) || !boundHost(w, r, q.Get("host_id")) {
		return
	}
	who := actuation.PrincipalFromContext(r.Context()).Name
	// A host that never claimed is registered as a claim would register it.
	if err := store.RegisterAgentdHost(r.Context(), a.DB, q.Get("host_id"), who); err != nil {
		a.fail(w, err)
		return
	}
	paper, err := store.IssueExam(r.Context(), a.DB, store.ExamRequest{Host: q.Get("host_id"), Token: who, Job: chi.URLParam(r, "job"),
		Executor: q.Get("executor")}, a.Exams)
	if err != nil {
		a.fail(w, err)
		return
	}
	// Each GET may issue a paper: never a cached answer (review 406 C1).
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, paper)
}

type examBody struct {
	hostBody
	store.ExamSubmission
}

// recordExam takes a part of a run's answers (POST /exams).
func (a *API) recordExam(w http.ResponseWriter, r *http.Request) {
	var b examBody
	if !a.examSet(w) || !decode(w, r, &b) || !boundHost(w, r, b.HostID) {
		return
	}
	who := actuation.PrincipalFromContext(r.Context()).Name
	if err := store.RegisterAgentdHost(r.Context(), a.DB, b.HostID, who); err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.RecordExamAnswers(r.Context(), a.DB, b.HostID, who, b.ExamSubmission, a.Exams)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
