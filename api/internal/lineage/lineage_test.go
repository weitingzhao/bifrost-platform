package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	cidX = "I1111111111111111111111111111111111111111"
	cidY = "I2222222222222222222222222222222222222222"
	cidZ = "I3333333333333333333333333333333333333333"
	cidG = "I4444444444444444444444444444444444444444"
	cidB = "I5555555555555555555555555555555555555555"
)

func TestParseMessage(t *testing.T) {
	msg := "fix: thing\n\nbody line\nNot-A-Trailer here: x\n\n" +
		"Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>\n" +
		"Claude-Session: local_a\nClaude-Session: local_b\nClaude-Transcript: t1\nChange-Id: " + cidX + "\n"
	subject, tr := parseMessage(msg)
	if subject != "fix: thing" {
		t.Fatalf("subject = %q", subject)
	}
	if tr.session != "local_a" || tr.transcript != "t1" || tr.changeID != cidX || !tr.agent {
		t.Fatalf("trailers = %+v (first session must win)", tr)
	}
	if _, tr := parseMessage("Claude-Session: local_x"); tr.session != "" {
		t.Fatalf("a one-line message has no trailer block, got %+v", tr)
	}
	if _, tr := parseMessage("subject\n\nCo-Authored-By: Someone <a@b>"); tr.agent {
		t.Fatal("non-Claude co-author counted as agent")
	}
}

func TestChangeIDsAnywhere(t *testing.T) {
	msg := "batch\n\n* one\n\nChange-Id: " + cidY + "\n\n* two\nChange-Id: " + cidZ + "\n\nChange-Id: " + cidB
	got := changeIDsAnywhere(msg)
	if strings.Join(got, ",") != cidY+","+cidZ+","+cidB {
		t.Fatalf("got %v", got)
	}
}

type fakeCommit struct {
	sha, msg string
	at       time.Time
}

func fakeGitea(t *testing.T, now time.Time) *httptest.Server {
	t.Helper()
	day := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	A := fakeCommit{"aaa", "feat: a (0.50.0)\n\nClaude-Session: local_s1\nClaude-Transcript: t1\nChange-Id: " + cidX, day(1)}
	B := fakeCommit{"bbb", "batch: squash\n\n* fix: e\n\nChange-Id: " + cidY + "\n\nCo-Authored-By: Claude Opus 5.5 <x>\nChange-Id: " + cidB, day(2)}
	C := fakeCommit{"ccc", "chore: c\n\nCo-Authored-By: Claude Opus 5.5 <x>", day(3)}
	G := fakeCommit{"ggg", "cursor: g\n\nChange-Id: " + cidG, day(4)}
	Old := fakeCommit{"old", "old\n\nClaude-Session: local_s9", day(40)}
	D := fakeCommit{"ddd", "feat: a (0.48.0)\n\nClaude-Session: local_s1\nChange-Id: " + cidX, day(5)}
	E := fakeCommit{"eee", "fix: e\n\nClaude-Session: local_s1\nClaude-Transcript: t2\nChange-Id: " + cidY, day(6)}
	F := fakeCommit{"fff", "feat: f\n\nClaude-Session: local_s2\nChange-Id: " + cidZ, day(2)}
	H := fakeCommit{"hhh", "chore: c\n\nClaude-Session: https://claude.ai/code/session_x", day(3)}
	history := map[string][]fakeCommit{
		"main":   {A, B, C, G, Old},
		"lane-1": {D, E, A, B, C, G, Old},
		"lane-2": {F, A, B, C, G, Old},
		"lane-3": {H, A, B, C, G, Old},
		"stale":  {Old},
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/orgs/bifrost/repos", func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "tok" || p != "pw" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		write(w, []map[string]any{
			{"name": "r1", "default_branch": "main"},
			{"name": "gone", "archived": true},
		})
	})
	mux.HandleFunc("/api/v1/repos/bifrost/r1/branches", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		for _, b := range []string{"main", "lane-1", "lane-2", "lane-3", "stale"} {
			out = append(out, map[string]any{"name": b, "commit": map[string]any{"timestamp": history[b][0].at}})
		}
		write(w, out)
	})
	mux.HandleFunc("/api/v1/repos/bifrost/r1/commits", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		if r.URL.Query().Get("page") == "1" {
			for _, c := range history[r.URL.Query().Get("sha")] {
				out = append(out, map[string]any{"sha": c.sha, "commit": map[string]any{
					"message": c.msg, "committer": map[string]any{"date": c.at}}})
			}
		}
		write(w, out)
	})
	return httptest.NewServer(mux)
}

func newTestService(t *testing.T) (*Service, time.Time) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	srv := fakeGitea(t, now)
	t.Cleanup(srv.Close)
	s := NewService(func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost", User: "tok", Pass: "pw"}, nil
	})
	s.now = func() time.Time { return now }
	return s, now
}

func TestBuildGroupsThreadsAndLanding(t *testing.T) {
	s, _ := newTestService(t)
	resp := s.Build(context.Background(), 14)
	if resp.Reachability != "ok" || len(resp.Errors) != 0 {
		t.Fatalf("reach=%s errors=%v", resp.Reachability, resp.Errors)
	}
	if len(resp.Threads) != 4 {
		t.Fatalf("threads = %d, want s1, s2, the cloud session and the no-session bucket: %+v", len(resp.Threads), resp.Threads)
	}
	byID := map[string]Thread{}
	for _, th := range resp.Threads {
		byID[th.Session] = th
	}
	if resp.Threads[3].Session != "" {
		t.Fatalf("no-session bucket must sort last, got %q", resp.Threads[3].Session)
	}

	s1 := byID["local_s1"]
	// A on main; D is the same Change-Id re-landed (0.48.0 -> 0.50.0) so it is folded into A;
	// E landed inside squash B.
	if s1.CommitCount != 2 || s1.Landed != 2 {
		t.Fatalf("s1 = %d commits / %d landed: %+v", s1.CommitCount, s1.Landed, s1.Repos)
	}
	if s1.Link != "claude://claude.ai/epitaxy/local_s1" || strings.Join(s1.Transcripts, ",") != "t1,t2" {
		t.Fatalf("s1 link/transcripts = %q %v", s1.Link, s1.Transcripts)
	}
	for _, c := range s1.Repos[0].Commits {
		switch c.SHA {
		case "aaa":
			if c.Ref != "main" || !c.Landed || c.LandedSHA != "" || c.LandedBy != "sha" {
				t.Fatalf("A = %+v", c)
			}
		case "eee":
			if c.Ref != "lane-1" || !c.Landed || c.LandedSHA != "bbb" || c.LandedBy != "change_id" {
				t.Fatalf("E should land via squash bbb: %+v", c)
			}
		default:
			t.Fatalf("unexpected commit in s1: %+v", c)
		}
	}

	s2 := byID["local_s2"]
	if s2.CommitCount != 1 || s2.Landed != 0 || s2.Repos[0].Commits[0].Ref != "lane-2" {
		t.Fatalf("s2 = %+v", s2)
	}
	cloud := byID["https://claude.ai/code/session_x"]
	if cloud.Link != cloud.Session || cloud.CommitCount != 1 {
		t.Fatalf("cloud session = %+v", cloud)
	}
	if h := cloud.Repos[0].Commits[0]; !h.Landed || h.LandedBy != "subject" || h.LandedSHA != "ccc" {
		t.Fatalf("pre-trailer commit should land by subject: %+v", h)
	}
	none := byID[""]
	if none.CommitCount != 2 || none.Link != "" { // G (Cursor) and B (squash with its own Change-Id)
		t.Fatalf("no-session bucket = %+v", none)
	}

	cov := resp.Coverage[0]
	// main in window: A B C G (Old is outside); stale branch moved before the window
	if cov.Repo != "r1" || cov.MainCommits != 4 || cov.WithSession != 1 || cov.WithChangeID != 3 ||
		cov.AgentNoLineage != 2 || cov.Branches != 3 {
		t.Fatalf("coverage = %+v", cov)
	}
}

func TestFilterByChangeIDAndSession(t *testing.T) {
	s, _ := newTestService(t)
	resp := s.Build(context.Background(), 14)

	got := filter(resp, "", cidZ)
	if len(got.Threads) != 1 || got.Threads[0].Session != "local_s2" || got.Threads[0].Landed != 0 {
		t.Fatalf("change_id filter = %+v", got.Threads)
	}
	got = filter(resp, "local_s1", "")
	if len(got.Threads) != 1 || got.Threads[0].CommitCount != 2 {
		t.Fatalf("session filter = %+v", got.Threads)
	}
	if len(resp.Threads) != 4 {
		t.Fatal("filter mutated the cached response")
	}
}

func TestBuildReportsAccessFailure(t *testing.T) {
	s := NewService(func(context.Context) (Access, error) {
		return Access{}, context.DeadlineExceeded
	})
	resp := s.Build(context.Background(), 0)
	if resp.Reachability != "fail" || resp.Days != defaultDays || len(resp.Errors) != 1 || resp.Threads == nil {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandlerServesJSON(t *testing.T) {
	s, _ := newTestService(t)
	h := NewHandler(s)
	rec := httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/api/v1/lineage?days=500&session=local_s2", nil))
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Days != maxDays || len(resp.Threads) != 1 || resp.Threads[0].Session != "local_s2" {
		t.Fatalf("resp = %+v", resp)
	}
}
