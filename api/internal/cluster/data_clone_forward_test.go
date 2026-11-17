//bifrost:owner tenant

package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Origin: 2026-11-17, the data clone moved to package dataclone; the action
// wiring (server/actions_wire.go, trust-anchored) still names
// HandleDataClone and HandleDataCloneSchedulePut on this Handler, which now
// forward to what the server binds. Unbound they must refuse and start
// nothing; the bound case is the positive control.
func TestDataCloneForwardersRefuseUnboundAndForwardBound(t *testing.T) {
	h := &Handler{}
	for name, handle := range map[string]http.HandlerFunc{"clone": h.HandleDataClone, "schedule": h.HandleDataCloneSchedulePut} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/data-clone", strings.NewReader(`{}`)))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "cluster.BindDataClone") {
			t.Fatalf("%s unbound: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	var called []string
	h.BindDataClone(DataCloneWrites{
		Clone: func(w http.ResponseWriter, _ *http.Request) {
			called = append(called, "clone")
			w.WriteHeader(http.StatusAccepted)
		},
		SchedulePut: func(w http.ResponseWriter, _ *http.Request) {
			called = append(called, "schedule")
			w.WriteHeader(http.StatusOK)
		},
	})
	rec := httptest.NewRecorder()
	h.HandleDataClone(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/data-clone", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("clone bound: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.HandleDataCloneSchedulePut(rec, httptest.NewRequest(http.MethodPut, "/api/v1/cluster/data-clone/schedule", nil))
	if rec.Code != http.StatusOK || strings.Join(called, ",") != "clone,schedule" {
		t.Fatalf("schedule bound: %d, called %v", rec.Code, called)
	}
}
