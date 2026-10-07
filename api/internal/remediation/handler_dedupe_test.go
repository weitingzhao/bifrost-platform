package remediation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// dedupeRunner is a fake runner whose job-1 stays in the given status and
// which counts POST /run calls, so a test can tell a deduped start apart from
// a second dispatch.
func dedupeRunner(t *testing.T, liveStatus *atomic.Value, posts *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		n := posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		id := "job-1"
		if n > 1 {
			id = "job-2"
		}
		_, _ = w.Write([]byte(`{"id":"` + id + `","status":"running","phase":"starting"}`))
	})
	mux.HandleFunc("/run/job-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"job-1","status":"` + liveStatus.Load().(string) + `","phase":"diagnosing"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func postStart(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/start", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.HandleStart(rec, req)
	return rec
}

// TD-224: a second start for a scope whose job is still running must not
// dispatch another agent; it answers 409 with the running job's id.
func TestStartDedupsActiveScope(t *testing.T) {
	var live atomic.Value
	live.Store("running")
	var posts atomic.Int32
	h := newTestHandler(t, dedupeRunner(t, &live, &posts).URL)

	if rec := postStart(h, `{"scope":"cluster-issues-full-auto"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("first start status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec := postStart(h, `{"scope":"cluster-issues-full-auto"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		JobID string `json:"job_id"`
		Job   Job    `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.JobID != "job-1" || body.Job.ID != "job-1" || body.Job.Scope != "cluster-issues-full-auto" {
		t.Fatalf("409 body = %+v, want job-1 with scope", body)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("runner POST /run calls = %d, want 1", got)
	}
}

func TestStartAllowsOtherScopeWhileOneRuns(t *testing.T) {
	var live atomic.Value
	live.Store("running")
	var posts atomic.Int32
	h := newTestHandler(t, dedupeRunner(t, &live, &posts).URL)

	_ = postStart(h, `{"scope":"a"}`)
	if rec := postStart(h, `{"scope":"b"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("other scope status = %d, want 202", rec.Code)
	}
	if rec := postStart(h, `{}`); rec.Code != http.StatusAccepted {
		t.Fatalf("empty scope status = %d, want 202 (empty scope never dedupes)", rec.Code)
	}
}

func TestStartAfterScopeJobFinishedDispatchesAgain(t *testing.T) {
	var live atomic.Value
	live.Store("running")
	var posts atomic.Int32
	h := newTestHandler(t, dedupeRunner(t, &live, &posts).URL)

	_ = postStart(h, `{"scope":"a"}`)
	// The archive still says running; the runner says done. The runner wins.
	live.Store("done")
	if rec := postStart(h, `{"scope":"a"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("status after finish = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	if got := posts.Load(); got != 2 {
		t.Fatalf("runner POST /run calls = %d, want 2", got)
	}
}
