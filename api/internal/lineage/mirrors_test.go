package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeMirrors serves two mirrors and one plain repo. A mirror-sync request
// stamps the mirror as fetched `lag` later; "broken" answers 403.
type fakeMirrors struct {
	mu      sync.Mutex
	synced  map[string]time.Time
	calls   map[string]int
	lag     time.Duration
	initial time.Time
}

func (f *fakeMirrors) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/orgs/bifrost/repos", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		stamp := func(name string) time.Time {
			if at, ok := f.synced[name]; ok && !time.Now().Before(at) {
				return at
			}
			return f.initial
		}
		write(w, []map[string]any{
			{"name": "m1", "default_branch": "main", "mirror": true, "mirror_updated": stamp("m1")},
			{"name": "broken", "default_branch": "main", "mirror": true, "mirror_updated": stamp("broken")},
			{"name": "local", "default_branch": "main"},
		})
	})
	mux.HandleFunc("/api/v1/repos/bifrost/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			name := r.URL.Path[len("/api/v1/repos/bifrost/") : len(r.URL.Path)-len("/mirror-sync")]
			f.mu.Lock()
			f.calls[name]++
			f.mu.Unlock()
			if name == "broken" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			f.mu.Lock()
			f.synced[name] = time.Now().Add(f.lag)
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		write(w, []any{}) // branches and commits: empty
	})
	return httptest.NewServer(mux)
}

func newMirrorService(t *testing.T, f *fakeMirrors) *Service {
	srv := f.server(t)
	t.Cleanup(srv.Close)
	s := NewService(func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost", User: "tok", Pass: "pw"}, nil
	})
	s.mirrorPoll = 10 * time.Millisecond
	s.mirrorSettle = 2 * time.Second
	return s
}

func TestBuildSyncsMirrorsFirst(t *testing.T) {
	f := &fakeMirrors{synced: map[string]time.Time{}, calls: map[string]int{}, lag: 50 * time.Millisecond,
		initial: time.Now().Add(-6 * time.Hour).Truncate(time.Second)}
	s := newMirrorService(t, f)

	resp := s.Build(context.Background(), 14)
	ms := resp.MirrorSync
	if ms == nil {
		t.Fatal("first build should ask Gitea to fetch the mirrors")
	}
	if f.calls["m1"] != 1 || f.calls["broken"] != 1 || f.calls["local"] != 0 {
		t.Fatalf("mirror-sync calls = %v, want m1 and broken once, local never", f.calls)
	}
	if ms.Settled || len(ms.Errors) != 1 || len(ms.Pending) != 0 {
		t.Fatalf("mirror sync = %+v, want one error (broken), nothing pending, not settled", ms)
	}
	var m1 *RepoCoverage
	for i := range resp.Coverage {
		if resp.Coverage[i].Repo == "m1" {
			m1 = &resp.Coverage[i]
		}
		if resp.Coverage[i].Repo == "local" && resp.Coverage[i].MirrorUpdated != nil {
			t.Fatal("a repo that is not a mirror has no mirror time")
		}
	}
	if m1 == nil || m1.MirrorUpdated == nil || m1.MirrorUpdated.Before(ms.RequestedAt.Truncate(time.Second)) {
		t.Fatalf("m1 coverage should carry the fetch that just finished, got %+v", m1)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("a failed mirror-sync must not fail the scan: %v", resp.Errors)
	}

	// inside mirrorEvery: no second round of requests
	if again := s.Build(context.Background(), 14); again.MirrorSync != nil || f.calls["m1"] != 1 {
		t.Fatalf("second build within %s asked again (calls %v)", s.mirrorEvery, f.calls)
	}
}

func TestBuildStopsWaitingForSlowMirror(t *testing.T) {
	f := &fakeMirrors{synced: map[string]time.Time{}, calls: map[string]int{}, lag: time.Hour,
		initial: time.Now().Add(-6 * time.Hour).Truncate(time.Second)}
	s := newMirrorService(t, f)
	s.mirrorSettle = 100 * time.Millisecond

	start := time.Now()
	resp := s.Build(context.Background(), 14)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("build waited %s for a mirror that never finished", took)
	}
	if ms := resp.MirrorSync; ms == nil || ms.Settled || len(ms.Pending) != 1 || ms.Pending[0] != "m1" {
		t.Fatalf("mirror sync = %+v, want m1 pending", resp.MirrorSync)
	}
}
