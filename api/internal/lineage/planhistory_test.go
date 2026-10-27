package lineage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// planGitea serves one repo's main in the delivery org "bifrost" and the
// result commits in the agent org "agents", counting requests by kind.
type planGitea struct {
	mu       sync.Mutex
	head     string
	commits  map[string]giteaCommit // main's commits
	results  map[string]string      // agent org: sha → message
	requests map[string]int
	dropped  string // a parent left out of every listing (a truncated history)
	t        *testing.T
}

func (f *planGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case p == "/api/v1/repos/bifrost/repo/branches/main":
		f.requests["head"]++
		_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"id": f.head}})
	case strings.HasPrefix(p, "/api/v1/repos/agents/repo/git/commits/"):
		f.requests["result"]++
		sha := strings.TrimPrefix(p, "/api/v1/repos/agents/repo/git/commits/")
		msg, ok := f.results[sha]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": sha, "commit": map[string]any{"message": msg}})
	case strings.HasPrefix(p, "/api/v1/repos/bifrost/repo/git/commits/"):
		f.requests["result-delivery"]++
		http.Error(w, "not found", http.StatusNotFound)
	case p == "/api/v1/repos/bifrost/repo/commits":
		f.requests["page"]++
		ref := r.URL.Query().Get("sha")
		if ref == "main" {
			f.t.Error("pagination did not pin head")
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		// git log order: reachable from ref, newest first (BFS is enough here).
		order := []string{}
		seen := map[string]bool{}
		for q := []string{ref}; len(q) > 0; q = q[1:] {
			c, ok := f.commits[q[0]]
			if !ok || seen[q[0]] || q[0] == f.dropped {
				continue
			}
			seen[q[0]] = true
			order = append(order, q[0])
			q = append(q, c.Parents...)
		}
		out := []map[string]any{}
		for i := (page - 1) * pageLimit; i < len(order) && i < page*pageLimit; i++ {
			c := f.commits[order[i]]
			parents := []map[string]any{}
			for _, x := range c.Parents {
				parents = append(parents, map[string]any{"sha": x})
			}
			out = append(out, map[string]any{"sha": c.SHA, "commit": map[string]any{"message": c.Msg, "committer": map[string]any{"date": time.Now()}}, "parents": parents})
		}
		_ = json.NewEncoder(w).Encode(out)
	default:
		http.Error(w, "unexpected "+p, http.StatusNotFound)
	}
}

func (f *planGitea) add(sha, msg string, parents ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits[sha] = giteaCommit{SHA: sha, Msg: msg, Parents: parents}
	f.head = sha
}

func (f *planGitea) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[kind]
}

func TestPlanHistoryReader(t *testing.T) {
	const task = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	a, base := sha40("a"), sha40("0")
	result := "result\n\nAgentrt-Task: " + task + "\nChange-Id: I" + a
	f := &planGitea{commits: map[string]giteaCommit{}, results: map[string]string{a: result}, requests: map[string]int{}, t: t}
	f.add(base, "base")
	// 60 commits: more than a page.
	prev := base
	for i := 1; i <= 60; i++ {
		sha := fmt.Sprintf("%040x", 0x1000+i)
		f.add(sha, "work "+strconv.Itoa(i), prev)
		prev = sha
	}
	landed := sha40("b")
	f.add(landed, result+"\n(cherry picked from commit "+a+")", prev)
	srv := httptest.NewServer(f)
	defer srv.Close()
	now := time.Unix(1_000_000, 0)
	r := &PlanHistoryReader{
		Access: func(context.Context) (Access, error) {
			return Access{Base: srv.URL, Org: "bifrost", AgentOrg: "agents"}, nil
		},
		Client:   srv.Client(),
		MaxPages: 10,
		Now:      func() time.Time { return now },
	}
	ctx := context.Background()

	// PV2 review blocker: the replayed result exists only in the agent org.
	h, err := r.History(ctx, "repo", task, a, "")
	if err != nil || h.State != landing.Effective || h.Commit != landed || h.Snapshot != landed {
		t.Fatalf("first read: %+v %v", h, err)
	}
	if f.count("result-delivery") != 0 {
		t.Fatal("result read from the delivery org although the agent org has it")
	}
	pages := f.count("page")

	// The same pass, another binding: no Gitea request at all.
	if h, err = r.History(ctx, "repo", task, a, ""); err != nil || h.State != landing.Effective {
		t.Fatalf("second read: %+v %v", h, err)
	}
	if f.count("page") != pages || f.count("head") != 1 || f.count("result") != 1 {
		t.Fatalf("cached read went to Gitea: %v", f.requests)
	}

	// main moves by a revert: one head read and one page, not the whole history.
	revert := sha40("c")
	f.add(revert, "Revert \"result\"\n\nThis reverts commit "+landed+".", landed)
	now = now.Add(time.Minute)
	if h, err = r.History(ctx, "repo", task, a, ""); err != nil || h.State != landing.Reverted {
		t.Fatalf("after revert: %+v %v", h, err)
	}
	if got := f.count("page") - pages; got != 1 {
		t.Fatalf("incremental read took %d pages", got)
	}

	// A history Gitea gives with a hole is Unknown, never Never or Effective.
	fresh := &PlanHistoryReader{Access: r.Access, Client: r.Client, MaxPages: 10, Now: r.Now}
	f.mu.Lock()
	f.dropped = base
	f.mu.Unlock()
	if h, err = fresh.History(ctx, "repo", task, a, ""); err == nil || h.State != landing.Unknown {
		t.Fatalf("truncated history became truth: %+v %v", h, err)
	}

	// PV2 re-review S1: the attempt branch is gone and its commit collected;
	// the replay on main still names it, so its identity is known.
	f.mu.Lock()
	f.dropped = ""
	delete(f.results, a)
	f.mu.Unlock()
	gone := &PlanHistoryReader{Access: r.Access, Client: r.Client, MaxPages: 10, Now: r.Now}
	if h, err = gone.History(ctx, "repo", task, a, ""); err != nil || h.State != landing.Reverted {
		t.Fatalf("result gone from Gitea: %+v %v", h, err)
	}

	// No result anywhere: Unknown.
	if h, err = fresh.History(ctx, "repo", task, sha40("e"), ""); err == nil || h.State != landing.Unknown {
		t.Fatalf("missing result: %+v %v", h, err)
	}
}
