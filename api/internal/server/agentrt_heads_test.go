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

var agentsRepoBody = map[string]string{"bifrost-agents/bifrost-platform": `{"full_name":"bifrost-agents/bifrost-platform"}`}

func branchBody(name, commit string) string {
	return `{"name":"` + name + `","commit":{"id":"` + commit + `"}}`
}

// fakeGitea answers the two reads the branch reader makes, by escaped path.
type fakeGitea struct {
	mu       sync.Mutex
	branches map[string]string // escaped path after /branches/ -> body
	repos    map[string]string // "org/repo" -> the body of its 200
	redirect map[string]string // escaped path -> where a 302 sends it
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
	if to, ok := f.redirect[r.URL.EscapedPath()]; ok {
		http.Redirect(w, r, to, http.StatusFound)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), "/api/v1/repos/")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	repo, branch, isBranch := strings.Cut(rest, "/branches/")
	if !isBranch {
		if body, ok := f.repos[rest]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if _, ok := f.repos[repo]; !ok {
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
		repos:    agentsRepoBody,
		branches: map[string]string{"agent/T-7/a1": branchBody("agent/T-7/a1", headSHA)},
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
		repos:    agentsRepoBody,
		branches: map[string]string{"agent/T%3F7/a1": branchBody("agent/T?7/a1", headSHA)},
	}
	if got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T?7/a1"); err != nil || got != headSHA {
		t.Fatalf("head = %q, %v", got, err)
	}
}

func TestGiteaBranchHeadRefusesAMissingBranch(t *testing.T) {
	f := &fakeGitea{repos: agentsRepoBody, branches: map[string]string{}}
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
		"unseen repository": {repos: map[string]string{}, branches: map[string]string{}},
		"gitea failing":     {status: http.StatusBadGateway},
		"no commit id": {
			repos:    agentsRepoBody,
			branches: map[string]string{"agent/T-7/a1": branchBody("agent/T-7/a1", "main")},
		},
		"unreadable answer": {
			repos:    agentsRepoBody,
			branches: map[string]string{"agent/T-7/a1": `{"commit":`},
		},
		// Review 194 A1: only that repository, read and named, refuses.
		"unreadable repository answer": {
			repos:    map[string]string{"bifrost-agents/bifrost-platform": `<html>unreadable</html>`},
			branches: map[string]string{},
		},
		"another repository's answer": {
			repos:    map[string]string{"bifrost-agents/bifrost-platform": `{"full_name":"bifrost/bifrost-platform"}`},
			branches: map[string]string{},
		},
		// Review 194 A2: a redirect is not followed to another branch.
		"a redirect to another branch": {
			repos:    agentsRepoBody,
			branches: map[string]string{"agent/T-7/a10": branchBody("agent/T-7/a10", headSHA)},
			redirect: map[string]string{
				"/api/v1/repos/bifrost-agents/bifrost-platform/branches/agent/T-7/a1": "/api/v1/repos/bifrost-agents/bifrost-platform/branches/agent/T-7/a10",
			},
		},
		"a redirect to another repository": {
			repos: map[string]string{
				"bifrost-agents/bifrost-platform": `{"full_name":"bifrost-agents/bifrost-platform"}`,
				"bifrost/bifrost-platform":        `{"full_name":"bifrost/bifrost-platform"}`,
			},
			branches: map[string]string{"agent/T-7/a1": branchBody("agent/T-7/a1", headSHA)},
			redirect: map[string]string{
				"/api/v1/repos/bifrost-agents/bifrost-platform/branches/agent/T-7/a1": "/api/v1/repos/bifrost/bifrost-platform/branches/agent/T-7/a1",
			},
		},
		"the answer names another branch": {
			repos:    agentsRepoBody,
			branches: map[string]string{"agent/T-7/a1": branchBody("agent/T-7/a10", headSHA)},
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

// a1 is one branch: a10 next to it is not it.
func TestGiteaBranchHeadReadsOneExactBranch(t *testing.T) {
	f := &fakeGitea{repos: agentsRepoBody, branches: map[string]string{"agent/T-7/a10": branchBody("agent/T-7/a10", headSHA)}}
	if _, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); !errors.Is(err, store.ErrState) {
		t.Fatalf("err = %v, want ErrState", err)
	}
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
