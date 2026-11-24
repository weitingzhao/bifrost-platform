package lineage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/mainhistory"
)

// Exercise the real HTTP reader with the existing Gitea handler, without
// needing a listening socket (also runnable in the implementation sandbox).
type planHistoryTransport struct{ http.Handler }

func (s planHistoryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Result(), nil
}

// The reader's own part of the PV2 fallback: a result gone from Gitea is
// recovered only for the independently bound task, from a candidate whose
// provenance names the result and that carries that task, after both result
// organizations were tried. The strict trailer parsing and the Change-Id
// rules are verification's (landinggit); the full PV2 table and the
// borrowed-Change-Id case run through the real rules bound by server, in
// server's TestPlanHistoryReadsThroughLandingRules (moved there 2026-11-20
// when lineage stopped importing landinggit, OWNER_LAW #95).
func TestPV2FallbackRequiresBoundIdentity(t *testing.T) {
	t.Parallel()
	a, b, base := sha40("a"), sha40("b"), sha40("0")
	const task = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	line := "(cherry picked from commit " + a + ")"
	trailers := "Agentrt-Task: " + task + "\nChange-Id: I" + b
	for _, tt := range []struct {
		name, task, message string
		want                mainhistory.History
	}{
		{"no bound task", "", "result\n\n" + trailers + "\n" + line, mainhistory.Unknown},
		{"missing task trailer", task, "result\n\nChange-Id: I" + b + "\n" + line, mainhistory.Unknown},
		{"wrong task", "other", "result\n\n" + trailers + "\n" + line, mainhistory.Unknown},
		{"no provenance", task, "result\n\n" + trailers, mainhistory.Unknown},
		{"exact task and provenance", task, "result\n\n" + trailers + "\n" + line, mainhistory.Effective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &planGitea{commits: map[string]giteaCommit{}, results: map[string]string{}, requests: map[string]int{}, t: t}
			f.add(base, "base")
			f.add(b, tt.message, base)
			r := &PlanHistoryReader{Access: func(context.Context) (Access, error) {
				return Access{Base: "http://history.invalid", Org: "bifrost", AgentOrg: "agents"}, nil
			}, Client: &http.Client{Transport: planHistoryTransport{f}}, MaxPages: 10, Rules: fakeRules()}
			h, err := r.History(context.Background(), "repo", tt.task, a, "")
			if h.State != tt.want || (err != nil) != (tt.want == mainhistory.Unknown) {
				t.Fatalf("state=%s error=%v; want %s", h.State, err, tt.want)
			}
			if f.count("result") != 1 || f.count("result-delivery") != 1 {
				t.Fatal("fallback did not try both result organizations")
			}
		})
	}
}

// Origin: 2026-11-20, domain wall (OWNER_LAW #95): the reader judged history
// with verification's landinggit imported directly; now it takes the rules
// through a port server binds. A reader nobody bound must answer Unknown with
// an error, never a state, and must not go to Gitea.
func TestPlanHistoryWithoutRulesIsUnknown(t *testing.T) {
	t.Parallel()
	f := &planGitea{commits: map[string]giteaCommit{}, results: map[string]string{}, requests: map[string]int{}, t: t}
	f.add(sha40("0"), "base")
	r := &PlanHistoryReader{Access: func(context.Context) (Access, error) {
		return Access{Base: "http://history.invalid", Org: "bifrost", AgentOrg: "agents"}, nil
	}, Client: &http.Client{Transport: planHistoryTransport{f}}, MaxPages: 10}
	h, err := r.History(context.Background(), "repo", "t", sha40("a"), "")
	if err == nil || h.State != mainhistory.Unknown {
		t.Fatalf("unbound rules gave %+v %v", h, err)
	}
	if len(f.requests) != 0 {
		t.Fatalf("unbound reader went to Gitea: %v", f.requests)
	}
}
