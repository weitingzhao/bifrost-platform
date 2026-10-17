package lineage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// cidOpenX is the Change-Id of an agent commit the tests below land or not.
const cidOpenX = "I9999999999999999999999999999999999999999"

// The agent organization (T36): agentd's branches are found by their task,
// read against the delivery main of the same repository, and nothing on that
// main is listed twice.
func TestAgentOrgBranches(t *testing.T) {
	const (
		taskOpen   = "t_01M50M1CZSDN8PNHRH8C2241RS"
		taskLanded = "t_01M50FZ523HZTGYJA0VB6BNE7N"
		cidOpen    = "I7777777777777777777777777777777777777777"
	)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	delivery := fakeGitea(t, now)
	t.Cleanup(delivery.Close)
	// the delivery main, as fakeGitea has it
	A := fakeCommit{"aaa", "feat: a (0.50.0)\n\nClaude-Session: local_s1\nClaude-Transcript: t1\nChange-Id: " + cidX, day(1)}
	B := fakeCommit{"bbb", "batch: squash", day(2)}
	open := fakeCommit{"k2", "test: the UniFi client\n\nAgentrt-Task: " + taskOpen + "\nChange-Id: " + cidOpen + "\nWork: W-31", day(1)}
	landed := fakeCommit{"l3", "fix: e, agentd's\n\nAgentrt-Task: " + taskLanded + "\nChange-Id: " + cidY + "\nWork: W-31", day(1)}
	openLane, landedLane := "agent/"+taskOpen+"/a1", "agent/"+taskLanded+"/a1"
	const taskLone = "t_01M50P8BBTT3PRZ9WT4RGP2784"
	loneLane := "agent/" + taskLone + "/a1"
	lone := fakeCommit{"l9", "docs: a lone repository's\n\nAgentrt-Task: " + taskLone + "\nChange-Id: I8888888888888888888888888888888888888888\nWork: W-31", day(1)}
	history := map[string][]fakeCommit{
		openLane:                                {open, A, B},
		landedLane:                              {landed, A, B},
		"agent/t_01M4XW5Z7SC6YN6KEN3JXF8CFV/a1": {{"zz", "old\n\nAgentrt-Task: t_01M4XW5Z7SC6YN6KEN3JXF8CFV", day(40)}},
	}
	var failRepos bool
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/orgs/bifrost-agents/repos", func(w http.ResponseWriter, r *http.Request) {
		if failRepos {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		// an agent repository's default branch is whatever was pushed first;
		// r9 has no delivery repository of its name
		write(w, []map[string]any{{"name": "r1", "default_branch": openLane}, {"name": "r9", "default_branch": loneLane}})
	})
	agentRepos := map[string]map[string][]fakeCommit{"r1": history, "r9": {loneLane: {lone}}}
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if u, p, _ := r.BasicAuth(); u != "tok" || p != "pw" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/api/v1/repos/bifrost-agents/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost-agents/")
		repo, what, _ := strings.Cut(rest, "/")
		hist := agentRepos[repo]
		var out []map[string]any
		switch what {
		case "branches":
			for name, h := range hist {
				out = append(out, map[string]any{"name": name, "commit": map[string]any{"id": h[0].sha, "timestamp": h[0].at}})
			}
		case "commits":
			h, ok := hist[r.URL.Query().Get("sha")]
			if !ok || r.URL.Query().Get("page") != "1" {
				break
			}

			for i, c := range h {
				out = append(out, map[string]any{"sha": c.sha, "parents": parentOf(h, i), "commit": map[string]any{
					"message": c.msg, "committer": map[string]any{"date": c.at}}})
			}
		}
		write(w, out)
	})
	// everything else is the delivery organization's
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest(r.Method, delivery.URL+r.URL.RequestURI(), nil)
		req.Header = r.Header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	service := func(agentOrg string) *Service {
		s := NewService(func(context.Context) (Access, error) {
			return Access{Base: srv.URL, Org: "bifrost", User: "tok", Pass: "pw", AgentOrg: agentOrg}, nil
		})
		s.now = func() time.Time { return now }
		return s
	}

	resp := service("bifrost-agents").Build(context.Background(), 14)
	if len(resp.Errors) != 0 || resp.Reachability != "ok" {
		t.Fatalf("errors %v, reachability %s", resp.Errors, resp.Reachability)
	}
	byTask := func(r Response, task string) []Commit {
		var out []Commit
		for _, th := range filter(r, "", "", task).Threads {
			for _, rc := range th.Repos {
				out = append(out, rc.Commits...)
			}
		}
		return out
	}
	if got := byTask(resp, taskOpen); len(got) != 1 || got[0].SHA != "k2" || got[0].Repo != "bifrost-agents/r1" ||
		got[0].Ref != openLane || got[0].Landed {
		t.Fatalf("the open agent commit = %+v", got)
	}
	if got := byTask(resp, taskLanded); len(got) != 1 || !got[0].Landed || got[0].LandedBy != "change_id" || got[0].LandedSHA != "bbb" {
		t.Fatalf("the landed agent commit = %+v", got)
	}
	// nothing of the delivery main is listed again under the agent repository
	agentCommits := 0
	for _, th := range resp.Threads {
		for _, rc := range th.Repos {
			if rc.Repo == "bifrost-agents/r1" {
				agentCommits += len(rc.Commits)
			}
		}
	}
	if agentCommits != 2 {
		t.Fatalf("%d commits under the agent repository, want the two agent ones", agentCommits)
	}
	// an agent repository with no delivery repository of its name: listed, not landed (review 210 B2)
	if got := byTask(resp, taskLone); len(got) != 1 || got[0].Repo != "bifrost-agents/r9" || got[0].Landed {
		t.Fatalf("the lone repository's commit = %+v", got)
	}
	var cov *RepoCoverage
	for i := range resp.Coverage {
		if resp.Coverage[i].Repo == "bifrost-agents/r1" {
			cov = &resp.Coverage[i]
		}
	}
	if cov == nil || cov.MainCommits != 0 || cov.Branches != 2 {
		t.Fatalf("agent coverage = %+v (the branch older than the window is not read)", cov)
	}

	// no agent organization, or the delivery one named again: nothing more is read
	for _, org := range []string{"", "bifrost"} {
		r := service(org).Build(context.Background(), 14)
		if len(byTask(r, taskOpen)) != 0 {
			t.Fatalf("agent org %q: the agent commit was read", org)
		}
		for _, c := range r.Coverage {
			if strings.HasPrefix(c.Repo, "bifrost-agents") {
				t.Fatalf("agent org %q: coverage %+v", org, c)
			}
		}
	}
	// the agent organization unreadable: said so, the rest still served
	failRepos = true
	r := service("bifrost-agents").Build(context.Background(), 14)
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "bifrost-agents") || r.Reachability != "degraded" || len(r.Threads) == 0 {
		t.Fatalf("agent org down: errors %v, reachability %s, %d threads", r.Errors, r.Reachability, len(r.Threads))
	}
}

// Review 210 A1 and B2: an agent commit whose Change-Id a main commit carries
// keeps its task, repository and branch, landed at that commit, whatever task
// the main commit names; a branch whose head is on main adds no agent row.
func TestAgentCommitSameChangeOnMain(t *testing.T) {
	const tAgent, tOther = "t_01M50M1CZSDN8PNHRH8C2241RS", "t_01M50FZ523HZTGYJA0VB6BNE7N"
	branch := "agent/" + tAgent + "/a1"
	agentMsg := "test: x\n\nAgentrt-Task: " + tAgent + "\nChange-Id: " + cidOpenX + "\nWork: W-31"
	for name, mainMsg := range map[string]string{
		"main has the same task": "test: x\n\nAgentrt-Task: " + tAgent + "\nChange-Id: " + cidOpenX,
		"main has no task":       "test: x (squashed)\n\nChange-Id: " + cidOpenX,
		"main has another task":  "test: x\n\nAgentrt-Task: " + tOther + "\nChange-Id: " + cidOpenX,
	} {
		t.Run(name, func(t *testing.T) {
			main := []giteaCommit{{SHA: "m1", Msg: mainMsg}}
			threads := map[string]*threadAcc{}
			collect(repoScan{repo: "r1", def: "main", main: main, branches: map[string][]giteaCommit{}}, threads)
			collect(repoScan{repo: "bifrost-agents/r1", agent: true, main: main,
				branches: map[string][]giteaCommit{branch: {{SHA: "b1", Msg: agentMsg}}}}, threads)
			resp := Response{Threads: finish(threads)}
			var agentRows []Commit
			for _, th := range filter(resp, "", "", tAgent).Threads {
				for _, rc := range th.Repos {
					for _, c := range rc.Commits {
						if rc.Repo == "bifrost-agents/r1" {
							agentRows = append(agentRows, c)
						}
					}
				}
			}
			if len(agentRows) != 1 || agentRows[0].SHA != "b1" || agentRows[0].Ref != branch || !agentRows[0].Landed ||
				agentRows[0].LandedBy != "change_id" || agentRows[0].LandedSHA != "m1" {
				t.Fatalf("agent rows = %+v", agentRows)
			}
		})
	}
	// Review 211 A1: landed at the commit whose trailer the id is, not at a
	// newer one that only names it in its body; a trailer in lower case counts.
	for name, main := range map[string][]giteaCommit{
		"a newer body names it": {
			{SHA: "n1", Msg: "batch: squash\n\n* test: x\n\nChange-Id: " + cidOpenX + "\n\nChange-Id: " + cidY},
			{SHA: "m1", Msg: "test: x\n\nChange-Id: " + cidOpenX},
		},
		"a lower-case trailer": {{SHA: "m1", Msg: "test: x\n\nchange-id: " + cidOpenX}},
	} {
		t.Run(name, func(t *testing.T) {
			threads := map[string]*threadAcc{}
			collect(repoScan{repo: "bifrost-agents/r1", agent: true, main: main,
				branches: map[string][]giteaCommit{branch: {{SHA: "b1", Msg: agentMsg}}}}, threads)
			got := filter(Response{Threads: finish(threads)}, "", "", tAgent)
			if len(got.Threads) != 1 || len(got.Threads[0].Repos) != 1 {
				t.Fatalf("threads = %+v", got.Threads)
			}
			if c := got.Threads[0].Repos[0].Commits[0]; c.SHA != "b1" || !c.Landed || c.LandedSHA != "m1" {
				t.Fatalf("agent row = %+v, want landed at m1", c)
			}
		})
	}
	// the agent branch's head is itself on main: the walk adds nothing; the
	// main row, with its task, is the one found
	main := []giteaCommit{{SHA: "m2", Msg: "test: y\n\nAgentrt-Task: " + tAgent + "\nChange-Id: " + cidOpenX}}
	threads := map[string]*threadAcc{}
	collect(repoScan{repo: "r1", def: "main", main: main, branches: map[string][]giteaCommit{}}, threads)
	collect(repoScan{repo: "bifrost-agents/r1", agent: true, main: main, branches: map[string][]giteaCommit{branch: {}}}, threads)
	got := filter(Response{Threads: finish(threads)}, "", "", tAgent)
	if len(got.Threads) != 1 || len(got.Threads[0].Repos) != 1 || got.Threads[0].Repos[0].Repo != "r1" {
		t.Fatalf("a head on main = %+v", got.Threads)
	}
}

// CUT-gitea-primary 0c: once Gitea is the primary, agent branches live in the
// delivery repository itself. An agent/ branch there keeps its task and is
// found landed by Change-Id, as one in the agent organization is; any other
// branch with the same change is still left to main's row.
func TestAgentBranchInTheDeliveryRepository(t *testing.T) {
	const tAgent = "t_01M50M1CZSDN8PNHRH8C2241RS"
	branch := "agent/" + tAgent + "/a1"
	main := []giteaCommit{{SHA: "m1", Msg: "test: x (squashed)\n\nChange-Id: " + cidOpenX}}
	threads := map[string]*threadAcc{}
	collect(repoScan{repo: "r1", def: "main", main: main, branches: map[string][]giteaCommit{
		branch:  {{SHA: "b1", Msg: "test: x\n\nAgentrt-Task: " + tAgent + "\nChange-Id: " + cidOpenX + "\nWork: W-31"}},
		"w31/x": {{SHA: "w1", Msg: "test: x\n\nChange-Id: " + cidOpenX}},
	}}, threads)
	var rows []Commit
	for _, th := range filter(Response{Threads: finish(threads)}, "", "", tAgent).Threads {
		for _, rc := range th.Repos {
			rows = append(rows, rc.Commits...)
		}
	}
	if len(rows) != 1 || rows[0].SHA != "b1" || rows[0].Ref != branch || rows[0].LandedBy != "change_id" || rows[0].LandedSHA != "m1" {
		t.Fatalf("agent rows = %+v", rows)
	}
	for _, th := range finish(threads) {
		for _, rc := range th.Repos {
			for _, c := range rc.Commits {
				if c.SHA == "w1" {
					t.Fatalf("a human branch's copy of a landed change was listed: %+v", c)
				}
			}
		}
	}
}

// CUT-gitea-primary 0c: a branch list that reaches its bound is reported
// (Coverage carries the error), and the branches read are still scanned.
func TestBranchListCutIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/branches") {
			_, _ = w.Write([]byte("[]"))
			return
		}
		page := r.URL.Query().Get("page")
		var b strings.Builder
		b.WriteString("[")
		for i := range pageLimit {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"name":"agent/t_%s_%d/a1","commit":{"id":"c","timestamp":"2026-10-17T00:00:00Z"}}`, page, i)
		}
		b.WriteString("]")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	g := &gitea{acc: Access{Base: srv.URL, Org: "bifrost"}, client: srv.Client()}
	brs, err := g.branches(context.Background(), "r1")
	if !errors.Is(err, errBranchesCut) || len(brs) != branchPages*pageLimit {
		t.Fatalf("branches = %d, %v", len(brs), err)
	}
	sc := scanRepo(context.Background(), g, giteaRepo{Name: "r1", Default: "main"}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if sc.err == nil || !errors.Is(sc.err, errBranchesCut) || len(sc.branches) == 0 {
		t.Fatalf("scan: err %v, branches %d", sc.err, len(sc.branches))
	}
}
