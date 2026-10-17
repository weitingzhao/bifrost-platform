package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func TestSyncMirrorsRejectsRepoOutsideAllowList(t *testing.T) {
	s := &Service{policy: &actuationpolicy.Policy{Mirrors: actuationpolicy.Mirrors{Repos: []string{"allowed-repo"}}}}
	if _, err := s.SyncMirrors(context.Background(), []string{"other-repo"}, nil); err == nil {
		t.Fatal("repo outside the allow-list was synced")
	}
}

func TestSyncMirrorsPollsFakeGitea(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/allowed-repo":
			_, _ = w.Write([]byte(`{"name":"allowed-repo","mirror":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/bifrost/allowed-repo/mirror-sync":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/allowed-repo/git/commits/"+sha:
			hits++
			if hits < 2 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := &Service{
		policy:    &actuationpolicy.Policy{Mirrors: actuationpolicy.Mirrors{Repos: []string{"allowed-repo"}}},
		giteaBase: srv.URL,
		mirrorCreds: func(context.Context) (string, string, error) {
			return "user", "secret", nil
		},
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
	got, err := s.SyncMirrors(context.Background(), []string{"allowed-repo"}, map[string]string{"allowed-repo": sha})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Repos) != 1 || !got.Repos[0].Present || !got.Repos[0].SyncRequested {
		t.Fatalf("result = %+v", got)
	}
	if hits < 2 {
		t.Fatalf("commit was not polled, hits=%d", hits)
	}
}

// CUT-gitea-primary 0b: a repository that is Gitea's own is not asked to
// fetch (Gitea refuses mirror-sync on it); its commit is still waited for.
// One that cannot be read, or whose answer does not say, is an error.
func TestSyncMirrorsPrimaryRepository(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	posted := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/primary":
			_, _ = w.Write([]byte(`{"name":"primary","mirror":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/silent":
			_, _ = w.Write([]byte(`{"name":"silent"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/gone":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			posted++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"Repository is not a mirror"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/bifrost/primary/git/commits/"+sha:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := &Service{
		policy:    &actuationpolicy.Policy{Mirrors: actuationpolicy.Mirrors{Repos: []string{"primary", "silent", "gone"}}},
		giteaBase: srv.URL,
		mirrorCreds: func(context.Context) (string, string, error) {
			return "user", "secret", nil
		},
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
	got, err := s.SyncMirrors(context.Background(), []string{"primary"}, map[string]string{"primary": sha})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Repos) != 1 || !got.Repos[0].Present || !got.Repos[0].Primary || got.Repos[0].SyncRequested || posted != 0 {
		t.Fatalf("primary: result = %+v, mirror-sync posts %d", got, posted)
	}
	for _, repo := range []string{"silent", "gone"} {
		got, err = s.SyncMirrors(context.Background(), []string{repo}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.OK || got.Repos[0].Present || got.Repos[0].Error == "" || posted != 0 {
			t.Fatalf("%s: result = %+v, mirror-sync posts %d", repo, got, posted)
		}
	}
}
