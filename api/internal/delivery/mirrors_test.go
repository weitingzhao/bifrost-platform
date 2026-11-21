package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// allowRepos is a MirrorPolicy that allows the named repositories, with the
// refusal the owner's allow-list gives. The tests take it instead of the
// owner's actuationpolicy (2026-11-21: tests reach other domains only through
// delivery's own port; the real policy is bound by the server).
type allowRepos []string

func (a allowRepos) MirrorAllowed(repo string) error {
	for _, r := range a {
		if r == repo {
			return nil
		}
	}
	return errors.New("repo is not in the mirror allow-list")
}

func TestSyncMirrorsRejectsRepoOutsideAllowList(t *testing.T) {
	s := &Service{policy: allowRepos([]string{"allowed-repo"})}
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
		policy:    allowRepos([]string{"allowed-repo"}),
		giteaBase: srv.URL,
		readerCreds: func(context.Context) (string, string, error) {
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
		policy:    allowRepos([]string{"primary", "silent", "gone"}),
		giteaBase: srv.URL,
		readerCreds: func(context.Context) (string, string, error) {
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

// Origin: 2026-11-21 decoupling line: delivery reaches owner through a port instead of importing actuationpolicy.
// Unbound (no MirrorPolicy handed over), every sync is refused with the
// allow-list's words, before Gitea is asked anything.
func TestSyncMirrorsWithoutPolicyRefuses(t *testing.T) {
	s := &Service{}
	s.SetPolicy(nil)
	if _, err := s.SyncMirrors(context.Background(), []string{"allowed-repo"}, nil); err == nil || err.Error() != "repo is not in the mirror allow-list" {
		t.Fatalf("unbound policy: %v", err)
	}
}
