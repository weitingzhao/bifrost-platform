package lineage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Exercise the real HTTP reader with the existing Gitea handler, without
// needing a listening socket (also runnable in the implementation sandbox).
type planHistoryTransport struct{ http.Handler }

func (s planHistoryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Result(), nil
}

func TestPV2FallbackRequiresBoundIdentity(t *testing.T) {
	t.Parallel()
	a, b, base := sha40("a"), sha40("b"), sha40("0")
	const task = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	line := "(cherry picked from commit " + a + ")"
	trailers := "Agentrt-Task: " + task + "\nChange-Id: I" + b
	for _, tt := range []struct {
		name, task, message string
		want                landing.History
	}{
		{"quoted body and no result", "", "documentation\n\nDo not use this old marker: " + line + "\n\nChange-Id: I" + b, landing.Unknown},
		{"no bound task", "", "result\n\n" + trailers + "\n" + line, landing.Unknown},
		{"missing task trailer", task, "result\n\nChange-Id: I" + b + "\n" + line, landing.Unknown},
		{"wrong task", "other", "result\n\n" + trailers + "\n" + line, landing.Unknown},
		{"duplicate task", task, "result\n\n" + trailers + "\nAgentrt-Task: " + task + "\n" + line, landing.Unknown},
		{"malformed second task", task, "result\n\n" + trailers + "\nAgentrt-Task: two values\n" + line, landing.Unknown},
		{"continued task", task, "result\n\nAgentrt-Task: " + task + "\n extra\nChange-Id: I" + b + "\n" + line, landing.Unknown},
		{"quoted provenance with bound task", task, "result\n\nExample: " + line + "\n\n" + trailers, landing.Unknown},
		{"duplicate provenance", task, "result\n\n" + trailers + "\n" + line + "\n" + line, landing.Unknown},
		{"exact task and provenance", task, "result\n\n" + trailers + "\n" + line, landing.Effective},
		{"bound task without Change-Id", task, "result\n\nAgentrt-Task: " + task + "\n" + line, landing.Effective},
		{"separate provenance paragraph", task, "result\n\n" + trailers + "\n\n" + line, landing.Effective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &planGitea{commits: map[string]giteaCommit{}, results: map[string]string{}, requests: map[string]int{}, t: t}
			f.add(base, "base")
			f.add(b, tt.message, base)
			r := &PlanHistoryReader{Access: func(context.Context) (Access, error) {
				return Access{Base: "http://history.invalid", Org: "bifrost", AgentOrg: "agents"}, nil
			}, Client: &http.Client{Transport: planHistoryTransport{f}}, MaxPages: 10}
			h, err := r.History(context.Background(), "repo", tt.task, a, "")
			if h.State != tt.want || (err != nil) != (tt.want == landing.Unknown) {
				t.Fatalf("state=%s error=%v; want %s", h.State, err, tt.want)
			}
			if f.count("result") != 1 || f.count("result-delivery") != 1 {
				t.Fatal("fallback did not try both result organizations")
			}
		})
	}
}

func TestPV2FallbackDoesNotBorrowCandidateChangeID(t *testing.T) {
	t.Parallel()
	a, b, c, rv, base := sha40("a"), sha40("b"), sha40("c"), sha40("d"), sha40("0")
	const task = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	f := &planGitea{commits: map[string]giteaCommit{}, results: map[string]string{}, requests: map[string]int{}, t: t}
	f.add(base, "base")
	f.add(b, "result\n\nAgentrt-Task: "+task+"\nChange-Id: I"+b+"\n(cherry picked from commit "+a+")", base)
	f.add(rv, "Revert \"result\"\n\nThis reverts commit "+b+".", b)
	// This Change-Id belongs to the candidate. It tells us nothing about the
	// missing queried result; importing it would falsely make this ambiguous.
	f.add(c, "other attempt\n\nChange-Id: I"+b, rv)
	r := &PlanHistoryReader{Access: func(context.Context) (Access, error) {
		return Access{Base: "http://history.invalid", Org: "bifrost", AgentOrg: "agents"}, nil
	}, Client: &http.Client{Transport: planHistoryTransport{f}}, MaxPages: 10}
	h, err := r.History(context.Background(), "repo", task, a, "")
	if err != nil || h.State != landing.Reverted || h.Commit != b {
		t.Fatalf("fallback imported candidate identity: %+v %v", h, err)
	}
}
