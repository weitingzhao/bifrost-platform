package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const someTask = "t_01M57YSQN6B43ZFNESD9VQ2HA9"

// T11: what names another task's directory or something in it is found,
// relative or not, with "./" segments and doubled slashes (reviews 340 B5,
// 341 A1, 342 A1); a task named by its id or by the platform's route to it,
// an attempt's branch as a whole ref or a Gitea URL at it, in text or in a
// JSON string whatever escape follows (reviews 340 A1, 341 A2, 342 A2, A4), a
// path beside one's own checkout (what agentd writes a check's own output
// as), agentd's root alone and raw output are not.
func TestWorkspaceRef(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"read ~/agent-work/agentd/tasks/" + someTask + "/a1/api/x.go",
		`{"text":"from tasks\/` + someTask + `\/a1"}`,
		"see tasks/" + someTask + "/bifrost-ui for the console",
		"cat tasks/./" + someTask + "/bifrost-ui/src/x.ts",
		"cat tasks//" + someTask + "/a1/report.md",
		someTask + "/bifrost-ui/src/x.ts, from the tasks directory",
		someTask + "/a3/report.md",
		"under agentd/tasks/" + someTask,
		"cd tasks/" + someTask + "\ncat a1/report.md",
		"cd tasks/" + someTask + "/./",
		"agent/" + someTask + "/a1/report.md is in the checkout",
		"agents/" + someTask + "/a1.out/verify.json",
		"cd ../" + someTask + "/\ncat a1/report.md", "cd ./" + someTask + "/", "ls ../" + someTask,
		"x/branch/agent/" + someTask + "/a1/api/go.mod is not a URL",
		// A key given twice keeps both values (review 343 A1).
		`{"pass":true,"commands":[],"note":"Read tasks/` + someTask + `/a1/secret.txt before reviewing","note":"ok"}`,
		`{"text":"read refs/remotes/agents/` + someTask + `/a1/x.go from the checkout"}`,
		`{"text":"copy from /home/u/agent-work/agentd/tasks/` + someTask + `/a1/x"}`,
	} {
		if _, found := store.WorkspaceRef("task_spec", []byte(in)); !found {
			t.Errorf("WorkspaceRef(%q) found nothing", in)
		}
	}
	for _, in := range []string{
		"# task\n", "build on the report of " + someTask,
		"Build on branch refs/heads/agent/" + someTask + "/a1, commit 2333e00753f91c74c8b2f4d693a803f8e89e7892.",
		"git fetch origin agent/" + someTask + "/a2 and diff it",
		"Compared refs/remotes/agents/" + someTask + "/a1 with main.",
		"see https://gitea.example/bifrost/bifrost-platform/src/branch/agent/" + someTask + "/a1",
		"see https://gitea.example/bifrost/bifrost-platform/src/branch/agent/" + someTask + "/a1/api/go.mod",
		"see http://gitea.example/bifrost/bifrost-platform/raw/branch/agent/" + someTask + "/a1/api/go.mod#L3",
		"https://gitea.example/b/p/src/branch/agent/" + someTask + "/a1?display=source",
		"fetched +refs/heads/agent/" + someTask + "/*:refs/remotes/agents/" + someTask + "/*",
		`{"text":"Build on refs/remotes/agents/` + someTask + `/a1\nThen run checks."}`,
		`{"text":"agent/` + someTask + `/a1\tand main"}`,
		"GET /api/v1/agentrt/tasks/" + someTask + " says it is submitted",
		"GET /agentrt/tasks/" + someTask,
		"../bifrost-ui/src/x.ts", "./console/src/a.ts", "a1b2 ../a1x", "agentd's root is $HOME/agent-work/agentd",
		"open ../a1.out/tmp/TestParse1234/001/in.json: no such file or directory", "diff against ../a1/api",
	} {
		if ref, found := store.WorkspaceRef("task_spec", []byte(in)); found {
			t.Errorf("WorkspaceRef(%q) = %q", in, ref)
		}
	}
	ref := "tasks/" + someTask + "/a1"
	for _, kind := range []string{"log_tail", "checkpoint_patch"} {
		if _, found := store.WorkspaceRef(kind, []byte(ref)); found {
			t.Errorf("a %s is raw output and is let through", kind)
		}
	}
	// A check's verify.json is scanned: agentd withholds what would be
	// refused (review 342 A3).
	for _, kind := range []string{"task_spec", "brief", "report", "verify", "review", "handover", "message", "mission_spec", "other"} {
		if _, found := store.WorkspaceRef(kind, []byte(ref)); !found {
			t.Errorf("a %s that names a workspace is let through", kind)
		}
	}
	if got, _ := store.WorkspaceRef("report", []byte("x /home/u/agent-work/agentd/tasks/"+someTask+"/a1")); strings.Contains(got, "/home/") {
		t.Errorf("the excerpt keeps a local path: %q", got)
	}
}

// T11: a spec stored some other way than an upload (a seat's, an older one)
// that names a task's workspace leaves nothing of its batch; a message that
// names one, which would become a brief's spec, is refused.
func TestWorkspaceRefRefused(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	clean := w.artifact(t, "task_spec", "# task\nbuild on the report of "+someTask)
	leaky := w.artifact(t, "task_spec", "# task\nstart from tasks/"+someTask+"/a1, it is nearly done")
	before := w.lastSeq(t, "mission:M-1")
	_, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-1", []store.TaskProposal{
		{Ref: "a", PlanNode: s1, Role: "implement", Class: "go", Title: "a", SpecArtifact: clean, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA},
		{Ref: "b", PlanNode: s1, Role: "implement", Class: "go", Title: "b", SpecArtifact: leaky, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA},
	})
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("a batch with a spec naming a workspace: %v", err)
	}
	if after := w.lastSeq(t, "mission:M-1"); after != before {
		t.Fatalf("the refused batch wrote %d events", after-before)
	}
	if _, perr := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k2", "M-1", []store.TaskProposal{
		{Ref: "a", PlanNode: s1, Role: "implement", Class: "go", Title: "a", SpecArtifact: clean, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA},
	}); perr != nil {
		t.Fatalf("a spec naming a task by its id: %v", perr)
	}
	_, err = store.SendMessage(ctx, w.rt, wr("thread:s1"), store.Seat, "ask", []byte("look at tasks/"+someTask+"/a1.out/report.md"))
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a message naming a workspace: %v", err)
	}
	// An attempt's branch is how a task reads another's work (review 340 A1).
	branch := "build on refs/heads/agent/" + someTask + "/a1"
	if _, err = store.SendMessage(ctx, w.rt, wr("thread:s1"), store.Seat, "ask", []byte(branch)); err != nil {
		t.Fatalf("a message naming a branch: %v", err)
	}
	onBranch := w.artifact(t, "task_spec", "# task\n"+branch)
	if _, err = store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k3", "M-1", []store.TaskProposal{
		{Ref: "a", PlanNode: s1, Role: "implement", Class: "go", Title: "a", SpecArtifact: onBranch, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA},
	}); err != nil {
		t.Fatalf("a spec naming a branch: %v", err)
	}
}
