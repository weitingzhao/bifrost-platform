package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7-8: an implementation on a branch is proposed only with a spec agentd
// can set up and check, agentd's JSON task spec with verify and, for a
// repository with setup, setup; the refusal names the task and what is
// missing and leaves nothing. A plan or a brief keeps its markdown spec.
func TestProposeRefusesAnImplementSpecWithoutChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for name, c := range map[string]struct{ spec, has string }{
		"markdown":      {"# implement it\n", "lacks verify"},
		"no verify":     {`{"text":"implement it"}`, "lacks verify"},
		"unknown field": {`{"text":"implement it","verify":[["true"]],"setup":[["true"]],"steps":[]}`, "lacks verify"},
		"empty command": {`{"text":"implement it","verify":[[]],"setup":[["true"]]}`, "lacks verify"},
		"no setup":      {`{"text":"implement it","verify":[["go","test","./..."]]}`, "lacks setup"},
	} {
		spec := w.artifact(t, "task_spec", c.spec)
		_, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "r-"+name, "M-1",
			[]store.TaskProposal{proposal("ok", w.impl), proposal("a", spec)})
		if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "task a: spec "+spec+" "+c.has) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.task WHERE created_by_actor_id = 'thread:s1'`); n != 0 {
		t.Fatalf("refused batches wrote %d tasks", n)
	}

	markdown := w.artifact(t, "task_spec", "# think it over\n")
	plan := offPlan(proposal("plan", markdown))
	plan.Role, plan.Effect = "plan", "none"
	brief := offPlan(proposal("brief", markdown))
	brief.Role, brief.Effect, brief.Repo, brief.BaseCommit = "brief", "none", "", ""
	// w.impl is store.ImplementSpec's.
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-1", []store.TaskProposal{proposal("a", w.impl), plan, brief})
	ok(t, err)
	if len(taskIDs(out)) != 3 {
		t.Fatalf("answer %v", out)
	}
}
