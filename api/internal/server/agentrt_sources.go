package server

import (
	"context"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The delivery lane and repository whose deployments the runtime's standing
// conditions (gates_passed) read. Round 2's work is the platform itself
// (W-31); release records are per lane and per repository, and the rule atom
// names only the environment. Trade joins when business work starts.
const (
	agentrtDeployLane = "platform"
	agentrtDeployRepo = "bifrost-platform"
)

// agentRuntimeSources reads the records the runtime does not own from the
// platform's own stores (design v5 §5.2 外部事实), each without side effects:
// approvals and questions by Lookup (no sweep), CI runs from Tekton, and
// deployments from the release records. A read that fails, or a record not
// there, is ok=false: the controller records nothing for it and a standing
// condition on it stays unknown.
// The readers agentRuntimeSources takes; a nil one reads nothing.
type (
	approvalReader interface {
		Lookup(ref string) (approvals.Approval, bool, error)
	}
	ciReader interface {
		CIRun(ctx context.Context, repo, sha string) (status, run string, err error)
	}
	releaseLister interface {
		List(ctx context.Context) ([]releases.Record, error)
	}
)

// question is the question reader of questions_wire.go (the one server file
// that names the questions package).
func agentRuntimeSources(ap approvalReader, question func(context.Context, string) (store.Question, bool), ci ciReader, rel releaseLister) controller.Sources {
	src := controller.Sources{
		Approval: func(context.Context, int64) (string, bool) { return "", false },
		Question: func(context.Context, string) (store.Question, bool) { return store.Question{}, false },
		CI:       func(context.Context, string, string) (string, string, bool) { return "", "", false },
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false },
	}
	if ap != nil {
		src.Approval = func(ctx context.Context, number int64) (string, bool) {
			a, ok, err := within(ctx, func() (approvals.Approval, bool, error) { return ap.Lookup(strconv.FormatInt(number, 10)) })
			if err != nil || !ok || a.Status == "" {
				return "", false
			}
			return a.Status, true
		}
	}
	if question != nil {
		src.Question = question
	}
	if ci != nil {
		src.CI = func(ctx context.Context, repo, commit string) (string, string, bool) {
			status, run, err := ci.CIRun(ctx, repo, commit)
			if err != nil || status == "" {
				return "", "", false
			}
			return status, run, true
		}
	}
	if rel != nil {
		src.Deployed = func(ctx context.Context, env string) (string, map[string]any, bool) {
			recs, err := rel.List(ctx)
			if err != nil {
				return "", nil, false
			}
			return deployedFrom(recs, env)
		}
	}
	return src
}

// questionFact is a question as the runtime records it. An answer the plan
// cannot use as a fact (no option chosen, or no one who answered) is not
// read: question_seen refuses a partial answer (review 151 B2), so it stays
// unknown rather than refused on every pass.
func questionFact(id, status string, answered bool, option, text, by string, at time.Time) (store.Question, bool) {
	f := store.Question{ID: id, Status: status}
	if !answered {
		return f, true
	}
	if option == "" || by == "" || at.IsZero() {
		return store.Question{}, false
	}
	f.Option, f.Text, f.AnsweredBy, f.AnsweredAt = option, text, by, at.UTC().Format(time.RFC3339)
	return f, true
}

// deployedFrom is what env runs now in the runtime's lane: the newest
// deploying record of that lane and environment (records come newest first)
// and its repository's commit. The reference is the delivery run alone: no
// image digest is recorded anywhere yet, and gates_passed compares the
// reference exactly, so it holds only what a gate check can read the same
// way. DEV has no deploying record.
func deployedFrom(recs []releases.Record, env string) (string, map[string]any, bool) {
	for _, r := range recs {
		if !r.Deploys || r.Lane != agentrtDeployLane || r.Env != env {
			continue
		}
		b, ok := r.Repos[agentrtDeployRepo]
		if !ok || len(b.SHA) != 40 {
			return "", nil, false
		}
		return b.SHA, map[string]any{"delivery_run": r.Run}, true
	}
	return "", nil, false
}

// within runs a read that does not take a context (the approval and question
// stores read their files with their own timeout) and gives up when ctx ends.
func within[T any](ctx context.Context, read func() (T, bool, error)) (T, bool, error) {
	type result struct {
		v   T
		ok  bool
		err error
	}
	out := make(chan result, 1)
	safego.Go("agentrt-source-read", func() {
		v, ok, err := read()
		out <- result{v, ok, err}
	})
	select {
	case r := <-out:
		return r.v, r.ok, r.err
	case <-ctx.Done():
		var zero T
		return zero, false, ctx.Err()
	}
}
