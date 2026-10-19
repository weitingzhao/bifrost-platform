package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const someTask = "t_01M57YSQN6B43ZFNESD9VQ2HA9"

// T11: what names a task's workspace, its output or agentd's root is found,
// relative or not; a task named by its id, a sibling repository and raw
// output are not.
func TestWorkspaceRef(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"read ~/agent-work/agentd/tasks/" + someTask + "/a1/api/x.go",
		`{"text":"from tasks\/` + someTask + `\/a1"}`,
		"see tasks/" + someTask + "/bifrost-ui for the console",
		"cat ../a2.out/verify.json",
		"diff against ../a1/api",
		someTask + "/a3/report.md",
		`{"text":"copy from /home/u/agent-work/agentd/tasks/` + someTask + `/a1/x"}`,
	} {
		if _, found := store.WorkspaceRef("task_spec", []byte(in)); !found {
			t.Errorf("WorkspaceRef(%q) found nothing", in)
		}
	}
	for _, in := range []string{
		"# task\n", "build on the report of " + someTask, someTask + "/api is not a path we know",
		"../bifrost-ui/src/x.ts", "./console/src/a.ts", "a1b2 ../a1x", "agentd's root is $HOME/agent-work/agentd",
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
		{Ref: "a", PlanNode: s1, Role: "implement", Class: "go", Title: "a", SpecArtifact: clean, Effect: "branch"},
		{Ref: "b", PlanNode: s1, Role: "implement", Class: "go", Title: "b", SpecArtifact: leaky, Effect: "branch"},
	})
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("a batch with a spec naming a workspace: %v", err)
	}
	if after := w.lastSeq(t, "mission:M-1"); after != before {
		t.Fatalf("the refused batch wrote %d events", after-before)
	}
	if _, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k2", "M-1", []store.TaskProposal{
		{Ref: "a", PlanNode: s1, Role: "implement", Class: "go", Title: "a", SpecArtifact: clean, Effect: "branch"},
	}); err != nil {
		t.Fatalf("a spec naming a task by its id: %v", err)
	}
	_, err = store.SendMessage(ctx, w.rt, wr("thread:s1"), store.Seat, "ask", []byte("look at ../a1.out/report.md"))
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a message naming a workspace: %v", err)
	}
}
