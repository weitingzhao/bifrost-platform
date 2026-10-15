package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

const headSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeGitea answers the two reads the branch reader makes, by escaped path.
type fakeGitea struct {
	mu       sync.Mutex
	branches map[string]string // escaped path after /branches/ -> body
	repos    map[string]int    // "org/repo" -> status
	status   int               // when set, every request gets it
	paths    []string
	auth     []string
}

func (f *fakeGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.EscapedPath())
	u, p, _ := r.BasicAuth()
	f.auth = append(f.auth, u+":"+p)
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), "/api/v1/repos/")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	repo, branch, isBranch := strings.Cut(rest, "/branches/")
	if !isBranch {
		if code, ok := f.repos[rest]; ok {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if f.repos[repo] != http.StatusOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, ok := f.branches[branch]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(body))
}

func headReader(t *testing.T, f *fakeGitea) store.BranchHead {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	access := func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL + "/", Org: "bifrost", User: "reader", Pass: "secret"}, nil
	}
	return giteaBranchHead(access, "bifrost-agents", srv.Client())
}

func TestGiteaBranchHeadReadsTheAgentOrganization(t *testing.T) {
	f := &fakeGitea{
		repos:    map[string]int{"bifrost-agents/bifrost-platform": http.StatusOK},
		branches: map[string]string{"agent/T-7/a1": `{"name":"agent/T-7/a1","commit":{"id":"` + headSHA + `"}}`},
	}
	got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
	if err != nil || got != headSHA {
		t.Fatalf("head = %q, %v; want %s", got, err, headSHA)
	}
	// The agent organization, not the delivery one (a pull mirror), and the
	// delivery service's credential.
	if want := "/api/v1/repos/bifrost-agents/bifrost-platform/branches/agent/T-7/a1"; len(f.paths) != 1 || f.paths[0] != want {
		t.Fatalf("paths = %v, want [%s]", f.paths, want)
	}
	if f.auth[0] != "reader:secret" {
		t.Fatalf("auth = %q", f.auth[0])
	}
}

func TestGiteaBranchHeadEscapesEachSegment(t *testing.T) {
	f := &fakeGitea{
		repos:    map[string]int{"bifrost-agents/bifrost-platform": http.StatusOK},
		branches: map[string]string{"agent/T%3F7/a1": `{"commit":{"id":"` + headSHA + `"}}`},
	}
	if got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T?7/a1"); err != nil || got != headSHA {
		t.Fatalf("head = %q, %v", got, err)
	}
}

func TestGiteaBranchHeadRefusesAMissingBranch(t *testing.T) {
	f := &fakeGitea{repos: map[string]int{"bifrost-agents/bifrost-platform": http.StatusOK}, branches: map[string]string{}}
	_, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
	if !errors.Is(err, store.ErrState) {
		t.Fatalf("err = %v, want ErrState", err)
	}
	if len(f.paths) != 2 {
		t.Fatalf("paths = %v: the repository read tells a missing branch from an unseen repository", f.paths)
	}
}

// A repository the credential does not see, or Gitea failing, is the
// platform's problem: not ErrState, so agentd retries and keeps the result.
func TestGiteaBranchHeadRetriesWhatIsNotTheAgents(t *testing.T) {
	cases := map[string]*fakeGitea{
		"unseen repository": {repos: map[string]int{}, branches: map[string]string{}},
		"gitea failing":     {status: http.StatusBadGateway},
		"no commit id": {
			repos:    map[string]int{"bifrost-agents/bifrost-platform": http.StatusOK},
			branches: map[string]string{"agent/T-7/a1": `{"commit":{"id":"main"}}`},
		},
		"unreadable answer": {
			repos:    map[string]int{"bifrost-agents/bifrost-platform": http.StatusOK},
			branches: map[string]string{"agent/T-7/a1": `{"commit":`},
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
			if err == nil || errors.Is(err, store.ErrState) {
				t.Fatalf("err = %v, want an error that is not ErrState", err)
			}
		})
	}
	t.Run("access fails", func(t *testing.T) {
		head := giteaBranchHead(func(context.Context) (delivery.GiteaAccess, error) {
			return delivery.GiteaAccess{}, errors.New("no cluster")
		}, "bifrost-agents", http.DefaultClient)
		if _, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err == nil || errors.Is(err, store.ErrState) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGiteaBranchHeadRefusesWhatIsNotABranchOfARepository(t *testing.T) {
	f := &fakeGitea{status: http.StatusOK}
	head := headReader(t, f)
	for _, c := range []struct{ repo, ref string }{
		{"bifrost-platform", "agent/T-7/a1"},
		{"bifrost-platform", "refs/tags/v1"},
		{"bifrost-platform", "refs/heads/"},
		{"bifrost-platform", "refs/heads/agent//a1"},
		{"bifrost-platform", "refs/heads/agent/../main"},
		{"bifrost/other", "refs/heads/agent/T-7/a1"},
		{"bifrost-platform.git", "refs/heads/agent/T-7/a1"},
		{"", "refs/heads/agent/T-7/a1"},
	} {
		if _, err := head(context.Background(), c.repo, c.ref); !errors.Is(err, store.ErrState) {
			t.Fatalf("%q %q: err = %v, want ErrState", c.repo, c.ref, err)
		}
	}
	if len(f.paths) != 0 {
		t.Fatalf("paths = %v: nothing is asked of Gitea for these", f.paths)
	}
}

func TestAgentRuntimeHeadsNeedsTheOrganization(t *testing.T) {
	s := &Server{delivery: delivery.NewHandlerForTest(&delivery.Service{}, nil)}
	t.Setenv("AGENTRT_GIT_ORG", "")
	if s.agentRuntimeHeads() != nil {
		t.Fatal("a reader without AGENTRT_GIT_ORG")
	}
	t.Setenv("AGENTRT_GIT_ORG", " bifrost-agents ")
	if s.agentRuntimeHeads() == nil {
		t.Fatal("no reader with AGENTRT_GIT_ORG")
	}
	if (&Server{}).agentRuntimeHeads() != nil {
		t.Fatal("a reader without the delivery service")
	}
}
