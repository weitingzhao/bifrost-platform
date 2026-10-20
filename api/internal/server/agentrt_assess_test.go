package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// fixedApprovals are the approvals waiting, as Waiting gives them.
type fixedApprovals []approvals.Approval

func (f fixedApprovals) Waiting() ([]approvals.Approval, error) { return f, nil }

// TD-391: what is assessed is the runtime's open cards, read from the real
// question store as the Owner sees them (another requester's question is
// not), and the approvals waiting, each with the others beside it and the
// repositories its key params name; an approval without a number is not.
func TestAssessableFromTheStores(t *testing.T) {
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	task := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	id, err := rt.agentRuntimeAsk()(ctx, store.Card{Key: strings.Repeat("cd", 16), Title: "Blocked in M-3: STG walk",
		Body: "Recommended: Close mission M-3 — why", Options: []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-3"},
		Recommended: 3, RecommendedWhy: "the mission is 30 hours old", Meanwhile: "the task stays blocked", OwnerDuty: "goal", TaskID: task,
		Urgency: "when you can", SameByKey: true})
	if err != nil {
		t.Fatal(err)
	}
	thread, _ := json.Marshal(map[string]any{"idempotency_key": "thread-question-2", "title": "A thread asks", "urgency": "u", "meanwhile": "m",
		"options": []map[string]string{{"label": "Yes"}, {"label": "No"}}, "recommended": 1})
	if res := rt.svc.Create(ctx, "thread-a", "reporter", "reporter", thread); res.Code != 201 {
		t.Fatalf("a thread's question: %d %v", res.Code, res.Body)
	}
	now := time.Now()
	waiting := fixedApprovals{
		{ID: "a1", Number: 7, Action: "deliver_platform", Tier: "C", Status: approvals.StatusPending, Summary: "Deliver platform to PROD",
			Reason: "STG runs 8f30a9f6", Requester: "thread-x", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Env: "prod",
			KeyParams: map[string]string{"repo": "bifrost-platform", "sha": "8f30a9f6"}},
		{ID: "a2", Number: 8, Action: "cordon_node", Status: approvals.StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "a3", Action: "old_record", Status: approvals.StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	}
	items, err := assessable(rt.agentRuntimeOpenCards(), waiting)(ctx)
	if err != nil || len(items) != 3 {
		t.Fatalf("assessable: %d %v", len(items), err)
	}
	c := items[0]
	if c.Kind != store.AssessCard || c.ID != id || c.TaskID != task || c.Recommended != "c" || len(c.Options) != 3 ||
		c.Options[2].Key != "c" || c.Options[2].Detail == "" || !strings.HasPrefix(c.Body, "Recommended: Close mission M-3") {
		t.Fatalf("the card: %+v", c)
	}
	a := items[1]
	if a.Kind != store.AssessApproval || a.ID != "7" || a.Title != "Deliver platform to PROD" || len(a.Repos) != 1 || a.Repos[0] != "bifrost-platform" ||
		len(a.Others) != 1 || a.Others[0].Number != 8 || len(a.Options) != 2 || a.Options[0].Key != "approve" || a.Record["env"] != "prod" {
		t.Fatalf("the approval: %+v", a)
	}
	if items[2].ID != "8" {
		t.Fatalf("the other approval: %+v", items[2])
	}
}

// evidenceGitea serves a main's commits and a ledger as Gitea's API does, and
// counts the ledger's reads.
func evidenceGitea(t *testing.T, commits map[string][]map[string]any, ledger string, raws *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/repos/bifrost/"
		repo, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix), "/")
		switch {
		case rest == "commits" && commits[repo] != nil:
			// Gitea's paging: limit per page, pages from 1.
			all := commits[repo]
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			from, to := min((page-1)*limit, len(all)), min(page*limit, len(all))
			_ = json.NewEncoder(w).Encode(all[from:to])
		case repo == "bifrost-trade-infra" && rest == "raw/agent-config/TECH_DEBT.md" && r.URL.Query().Get("ref") == "main":
			raws.Add(1)
			_, _ = w.Write([]byte(ledger))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func giteaCommit(sha, msg string, at time.Time) map[string]any {
	return map[string]any{"sha": sha, "commit": map[string]any{"message": msg, "committer": map[string]any{"date": at.UTC().Format(time.RFC3339)}}}
}

// An assessment's evidence (TD-391) is the commits on main that tie to it:
// a result of one of its tasks, one it names, one whose Work trailer or
// subject names its item; not one of a programme (an item more than eight
// commits name), nor an unrelated one. Its ledger items are read from the
// ledger's main, kept ten minutes. What cannot be read is said.
func TestEvidenceFromGitea(t *testing.T) {
	task := store.NewTaskID()
	now := time.Now()
	sha := func(n int) string { return fmt.Sprintf("%040x", n) }
	main := []map[string]any{
		giteaCommit(sha(1), "agentrt: a result\n\nBody.\n\nAgentrt-Task: "+task+"\nWork: W-31", now.Add(-time.Hour)),
		giteaCommit(sha(2), "agentrt: the infra check runs it (TD-378)\n\nChange-Id: I"+strings.Repeat("a", 40)+"\nWork: TD-378", now.Add(-2*time.Hour)),
		giteaCommit("8f30a9f6"+strings.Repeat("0", 32), "agentrt: review 444", now.Add(-3*time.Hour)),
		giteaCommit(sha(4), "docs: unrelated", now.Add(-4*time.Hour)),
	}
	for i := range 10 {
		main = append(main, giteaCommit(sha(100+i), "agentrt: programme work\n\nWork: W-31", now.Add(-time.Duration(5+i)*time.Hour)))
	}
	var raws atomic.Int32
	ledger := "# Ledger\n\n### TD-391\n\n**P2 · not asked**\n\n- **状态**：未开始\n\n### TD-378\n\n**P2 · agentrt · the check leaves a file out**\n\n" +
		"- **状态**：待你签收（its ratchet is in platform 6b950732）\n- **Claim**: more\n\n## 已删\n"
	g := evidenceGitea(t, map[string][]map[string]any{"bifrost-platform": main}, ledger, &raws)
	access := func(context.Context) (lineage.Access, error) { return lineage.Access{Base: g.URL, Org: "bifrost"}, nil }
	read := evidenceReader(access, g.Client(), "bifrost-trade-infra", "agent-config/TECH_DEBT.md")
	q := store.EvidenceQuery{Repos: []string{"bifrost-platform", "bifrost-gone"}, Tasks: []string{task}, Items: []string{"TD-378", "W-31"},
		Commits: []string{"8f30a9f6"}, Since: now.Add(-48 * time.Hour)}
	ev := read(context.Background(), q)
	var got []string
	for _, c := range ev.Commits {
		got = append(got, c.SHA[:8])
	}
	if strings.Join(got, ",") != sha(1)[:8]+","+sha(2)[:8]+",8f30a9f6" || ev.Org != "bifrost" || ev.Commits[1].ChangeID == "" ||
		ev.Commits[0].Task != task || strings.Join(ev.Commits[1].Work, ",") != "TD-378" {
		t.Fatalf("the commits: %v %+v", got, ev.Commits)
	}
	if len(ev.Ledger) != 1 || ev.Ledger[0].ID != "TD-378" || ev.Ledger[0].Title != "P2 · agentrt · the check leaves a file out" ||
		ev.Ledger[0].State != "状态：待你签收（its ratchet is in platform 6b950732）" {
		t.Fatalf("the ledger: %+v", ev.Ledger)
	}
	if len(ev.NotRead) != 1 || !strings.HasPrefix(ev.NotRead[0], "main of bifrost-gone (") {
		t.Fatalf("not read: %v", ev.NotRead)
	}
	read(context.Background(), q)
	if raws.Load() != 1 {
		t.Fatalf("the ledger was read %d times", raws.Load())
	}
	ev = evidenceReader(access, g.Client(), "", "")(context.Background(), q)
	if len(ev.Ledger) != 0 || ev.NotRead[len(ev.NotRead)-1] != "the ledger (AGENTRT_LEDGER is not set on this platform)" {
		t.Fatalf("without a ledger: %+v", ev)
	}
	t.Setenv("AGENTRT_LEDGER", "bifrost-trade-infra/agent-config/TECH_DEBT.md")
	if repo, path := ledgerFromEnv(); repo != "bifrost-trade-infra" || path != "agent-config/TECH_DEBT.md" {
		t.Fatalf("AGENTRT_LEDGER: %q %q", repo, path)
	}
}

// Review 448 C2: a main whose pages run out before the window's start says
// so, with the oldest commit it read.
func TestEvidenceSaysWhereMainWasCut(t *testing.T) {
	now := time.Now()
	var main []map[string]any
	for i := range evidencePages*50 + 5 {
		main = append(main, giteaCommit(fmt.Sprintf("%040x", i+1), "work", now.Add(-time.Duration(i)*time.Minute)))
	}
	var raws atomic.Int32
	g := evidenceGitea(t, map[string][]map[string]any{"bifrost-platform": main}, "", &raws)
	access := func(context.Context) (lineage.Access, error) { return lineage.Access{Base: g.URL, Org: "bifrost"}, nil }
	ev := evidenceReader(access, g.Client(), "", "")(context.Background(), store.EvidenceQuery{Repos: []string{"bifrost-platform"},
		Since: now.Add(-48 * time.Hour)})
	want := fmt.Sprintf("main of bifrost-platform before %s (only its latest %d commits are read)",
		now.Add(-time.Duration(evidencePages*50-1)*time.Minute).UTC().Format(time.RFC3339), evidencePages*50)
	if len(ev.NotRead) != 2 || ev.NotRead[0] != want {
		t.Fatalf("not read: %q; want %q first", ev.NotRead, want)
	}
}
