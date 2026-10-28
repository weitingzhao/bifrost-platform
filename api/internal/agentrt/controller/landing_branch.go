package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// DrillGit exposes only the STG Git adapter operations needed for recovery.
type DrillGit interface {
	Open(context.Context, string) (*landinggit.Workspace, error)
	Ref(context.Context, string) (string, error)
	Delete(context.Context, string, string) error
}

// DrillGitFacts supplies fresh complete Git evidence to the shared store
// reader. Every temporary clone is removed, including on an error.
func DrillGitFacts(g DrillGit) store.LandingSource {
	return store.LandingSource{
		Result: func(ctx context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
			if repo != "bifrost-platform" {
				return false, nil, fmt.Errorf("unsupported drill repo")
			}
			w, e := g.Open(ctx, a)
			if e != nil {
				return false, nil, e
			}
			defer w.Close()
			return w.Result(ctx, task, base, a)
		},
		History: func(ctx context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
			if repo != "bifrost-platform" {
				return landing.MainHistory{}, fmt.Errorf("unsupported drill repo")
			}
			w, e := g.Open(ctx, a)
			if e != nil {
				return landing.MainHistory{}, e
			}
			defer w.Close()
			return w.History(ctx, task, a, snapshot)
		},
	}
}

// LandingDrillBranch prepares or reconciles a claimed operation. A successful
// push followed by a crash is resolved by ls-remote, never a new SHA or branch.
// It has no production path and cannot record main_intent/landed.
func (c *Controller) LandingDrillBranch(ctx context.Context, g DrillGit, op string, now time.Time) error {
	if c.Env != "stg" || g == nil {
		return fmt.Errorf("drill requires stg git")
	}
	var s landing.State
	if err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		s, e = store.LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	}); err != nil {
		return err
	}
	o, ok := s.Operations[op]
	if !ok {
		return fmt.Errorf("unknown operation")
	}
	put := func(kind string, fields map[string]any) error {
		fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
		return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
			_, e := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "STG landing drill: " + kind, "landing": fields}})
			return e
		})
	}
	if o.Terminal() {
		if o.Ref != "" {
			return g.Delete(ctx, o.Ref, o.R)
		}
		return nil
	}
	if o.Phase != "claimed" && o.Phase != "prepared" && o.Phase != "branch_intent" {
		return nil
	}
	first, n := landing.RootBudget(s, o.Task)
	if landing.Bound(first, n-1, store.DecisionAt()) { // CUT §9
		return put("failed", map[string]any{"why": "bound"})
	}
	// Reconcile a completed push even after a rule expires. If absent, all
	// eligibility conditions still apply before retrying the external action.
	if o.Phase == "branch_intent" {
		tip, e := g.Ref(ctx, o.Ref)
		if e != nil {
			return e
		}
		if tip == o.R {
			return put("branch_pushed", map[string]any{"ref": o.Ref, "R": o.R})
		}
		if tip != "" {
			_, n := landing.RootBudget(s, o.Task)
			if n >= 8 {
				return put("failed", map[string]any{"why": "bound"})
			}
			return put("discarded", map[string]any{"why": "branch_collision"})
		}
	}
	f, d, e := c.LandingDecision(ctx, DrillGitFacts(g), o.Task, op, "claim", now)
	if e != nil {
		return e
	}
	// The decision read Git: the window is checked again as of now, before a
	// new branch intent (review 657 R657-2).
	at := store.DecisionAt()
	if landing.Bound(first, n-1, at) {
		return put("failed", map[string]any{"why": "bound"})
	}
	if !d.Allowed {
		if d.Reason == "rule" || d.Reason == "limit" {
			// v1 does not permit branch_intent -> parked. Preserve intent and wait;
			// reconciliation above still runs even without a current authorization.
			if o.Phase == "branch_intent" {
				return nil
			}
			return put("parked", map[string]any{"why": d.Reason, "retry_at": at.Add(time.Minute).Format(time.RFC3339Nano)})
		}
		return put("failed", map[string]any{"why": "stale"})
	}
	if f.A != o.A || f.Attempt != o.Attempt {
		return put("failed", map[string]any{"why": "stale"})
	}
	// Before each new intent and the push, after the Git reads before it:
	// the window and the authorization as of now (review 659 R659-1). Out of
	// either, nothing new is written this pass; the next pass parks or
	// fails it on its own reads.
	recheck := func() (bool, error) {
		at := store.DecisionAt()
		if landing.Bound(first, n-1, at) {
			return true, put("failed", map[string]any{"why": "bound"})
		}
		now, err := store.RefreshLandingFacts(ctx, c.DB, DrillGitFacts(g), c.Env, mode(c.Env), o.Task, op, f, at)
		if err != nil {
			return true, err
		}
		return !landing.EligibleToClaim(now, at).Allowed, nil
	}
	w, e := g.Open(ctx, o.A)
	if e != nil {
		return e
	}
	defer w.Close()
	m0, date := o.M0, o.CommitterDate
	if o.Phase == "claimed" {
		m0, e = w.Main(ctx)
		if e != nil {
			return e
		}
		date = now.UTC().Truncate(time.Second)
	}
	r, e := w.Replay(ctx, landinggit.ReplayInput{A: o.A, Base: f.Base, M0: m0, Task: o.Task, Attempt: o.Attempt, Mission: f.Mission, Reviewers: f.Reviewers, Operation: op, RuleVersion: o.RuleVersion, AcceptedAt: f.AcceptedAt, CommitterDate: date})
	if e != nil {
		why := ""
		switch {
		case errors.Is(e, landinggit.ErrOverlap):
			why = "overlap"
		case errors.Is(e, landinggit.ErrConflict):
			why = "conflict"
		case errors.Is(e, landinggit.ErrPatchChanged):
			why = "patch_changed"
		}
		if why != "" {
			return put("failed", map[string]any{"why": why})
		}
		return e
	}
	if stop, e := recheck(); stop || e != nil {
		return e
	}
	if o.Phase == "claimed" {
		if e = put("prepared", map[string]any{"R": r, "M0": m0, "committer_date": date.Format(time.RFC3339Nano), "dry_run": true}); e != nil {
			return e
		}
	} else if r != o.R {
		return put("failed", map[string]any{"why": "patch_changed"})
	}
	parts := strings.Split(op, ":")
	k, e := strconv.Atoi(parts[len(parts)-1])
	if e != nil || k < 1 {
		return fmt.Errorf("invalid operation number")
	}
	ref := "refs/heads/land/stg/" + o.Task + "/" + strconv.Itoa(k)
	if o.Phase != "branch_intent" {
		if e = put("branch_intent", map[string]any{"ref": ref, "R": r}); e != nil {
			return e
		}
	}
	tip, e := g.Ref(ctx, ref)
	if e != nil {
		return e
	}
	if tip != "" && tip != r {
		if n >= 8 {
			return put("failed", map[string]any{"why": "bound"})
		}
		return put("discarded", map[string]any{"why": "branch_collision"})
	}
	if tip == "" {
		if stop, e := recheck(); stop || e != nil {
			return e
		}
		if e = w.Push(ctx, ref, r); e != nil {
			return e
		}
	}
	return put("branch_pushed", map[string]any{"ref": ref, "R": r})
}
