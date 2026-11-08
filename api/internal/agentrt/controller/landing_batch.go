package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Batch landing (R5:5.7 goal 2, landing/batch.go): on PROD, the
// bifrost-platform roots waiting to land are stacked into one batch that
// runs one landing CI. The claim step forms it (claimBatch): the stack is
// replayed in one workspace first, member k on member k-1's commit, then
// each member is claimed; a candidate that does not replay on the stack
// stays out for the next batch. The batch's members are prepared and pushed
// together (landingBatchBranch), all with the batch's M0 and R. The lead
// alone kicks, reruns or parks for CI (LandingDrillCI); the others follow
// it. Green, the batch gate writes every member's main_intent and R is
// pushed to main once (landBatch); red, the members the failure blames go
// back for rework and the others land in a later batch, or the batch is
// split in half (failBatchRed). The reconciler then lands each member as
// any operation. Infra, every other repository and the STG drill keep the
// one-operation path.

// BatchWorkspace is what a batch needs of a workspace
// (landinggit.Workspace): main, each member's result fetched and replayed
// on the one before, and the land branches pushed.
type BatchWorkspace interface {
	Main(ctx context.Context) (string, error)
	Fetch(ctx context.Context, commits ...string) error
	Replay(ctx context.Context, in landinggit.ReplayInput) (string, error)
	Push(ctx context.Context, ref, sha string) error
	Close() error
}

// batching: this controller lands repo in batches.
func (c *Controller) batching(repo string) bool {
	return c.Env == "prod" && repo == "bifrost-platform"
}

func (c *Controller) openBatch(ctx context.Context, repo, commit string) (BatchWorkspace, error) {
	if c.Drill != nil && c.Drill.OpenBatch != nil {
		return c.Drill.OpenBatch(ctx, repo, commit)
	}
	g := c.DrillGit[repo]
	if g == nil {
		return nil, fmt.Errorf("landing batch requires its git")
	}
	w, err := g.Open(ctx, commit)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// landingBatchFor is op's batch, when it is in one. A lone operation costs
// no read: only an operation sharing its R with another (a prepared batch),
// or claimed while another is still going, may be in a batch.
func (c *Controller) landingBatchFor(ctx context.Context, repo, op string, s landing.State) (store.LandingBatch, bool, error) {
	o, ok := s.Operations[op]
	if !ok || !c.batching(repo) {
		return store.LandingBatch{}, false, nil
	}
	maybe := false
	for id, x := range s.Operations {
		if id != op && x.Task != "" && (o.R != "" && x.R == o.R || o.R == "" && !x.Terminal()) {
			maybe = true
			break
		}
	}
	if !maybe {
		return store.LandingBatch{}, false, nil
	}
	return store.LandingBatchOf(ctx, c.DB, repo, op)
}

// claimOne claims task as the claim loop does; with names the operations
// already claimed for the batch being formed. A refusal is recorded (CUT
// §11.13) and backs the task off.
func (c *Controller) claimOne(ctx context.Context, repo, task string, now time.Time, with ...string) (string, error) {
	op, d, err := store.ClaimLandingInBatch(ctx, c.DB, c.Drill.Facts, c.Env, task, now, with)
	c.repoWaits(repo).record(task, err == nil && (op != "" || d.Reason == "changed"), c.clock())
	if err != nil {
		return "", err
	}
	if op == "" && d.Reason != "" {
		_, err = store.RecordClaimRefusal(ctx, c.DB, repo, task, d.Reason, d.Detail)
	}
	return op, err
}

// claimBatch is the claim step of a pass that lands repo in batches; false
// leaves it to the one-at-a-time loop. It reads each candidate's facts (a
// refused one is claimed alone, so its refusal is recorded as before), forms
// the batch (landing.FormBatch; a half of a split red batch first, store.
// PendingBatchHalf) and, with two members or more, replays, claims and
// prepares it (formBatch). A batch of one is today's single landing.
func (c *Controller) claimBatch(ctx context.Context, repo string, tasks []string, s landing.State, now time.Time) (bool, error) {
	if !c.batching(repo) || len(tasks) < 2 {
		return false, nil
	}
	// A landing in flight (a batch or one operation) refuses every claim
	// for the limit: the one-at-a-time loop records that, reading no more.
	if s.InFlight("") > 0 {
		return false, nil
	}
	roots, after, err := store.PendingBatchHalf(ctx, c.DB, repo)
	if err != nil {
		return true, err
	}
	if half := slices.DeleteFunc(slices.Clone(tasks), func(t string) bool { return !slices.Contains(roots, t) }); len(half) > 0 {
		tasks = half
	} else {
		after = ""
	}
	// Every candidate's facts are read at once, each with the whole claim
	// budget: one read is a Git fetch of a few seconds, and read one after
	// another (or on an even split of the pass) 24 candidates never fit, so
	// no batch formed (2026-11-06). The reads only read; the claims below
	// stay in candidate order.
	type read struct {
		f   landing.Facts
		d   landing.Decision
		err error
	}
	reads := make([]read, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		safego.Go("agentrt-landing-batch-read", func() {
			defer wg.Done()
			f, d, err := c.LandingDecision(ctx, c.Drill.Facts, task, "", "claim", now)
			reads[i] = read{f, d, err}
		})
	}
	wg.Wait()
	var errs []error
	var cands []landing.BatchCandidate
	for i, task := range tasks {
		if len(landing.FormBatch(cands, now)) >= landing.BatchMax {
			break
		}
		f, d, err := reads[i].f, reads[i].d, reads[i].err
		switch {
		case err != nil:
			c.repoWaits(repo).record(task, false, c.clock())
		case !d.Allowed:
			var op string
			if op, err = c.claimOne(ctx, repo, task, now); op != "" {
				return true, errors.Join(append(errs, err, c.LandingDrillBranch(ctx, c.DrillGit[repo], op, now, repo))...)
			}
		default:
			cands = append(cands, landing.BatchCandidate{Task: task, Facts: f})
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	members := landing.FormBatch(cands, now)
	if len(members) >= 2 {
		return true, errors.Join(append(errs, c.formBatch(ctx, repo, members, after, s, now))...)
	}
	// One member or none: the first that claims lands alone, as before.
	single := slices.Clone(members)
	for _, cand := range cands {
		if !slices.ContainsFunc(single, func(m landing.BatchCandidate) bool { return m.Task == cand.Task }) {
			single = append(single, cand)
		}
	}
	for _, cand := range single {
		op, err := c.claimOne(ctx, repo, cand.Task, now)
		if err != nil {
			errs = append(errs, err)
		}
		if op != "" {
			return true, errors.Join(append(errs, c.LandingDrillBranch(ctx, c.DrillGit[repo], op, now, repo))...)
		}
	}
	return true, errors.Join(errs...)
}

// batchSlot is a member replayed on the stack: the operation its claim will
// open and the commit its replay made.
type batchSlot struct {
	cand  landing.BatchCandidate
	op, r string
}

// formBatch replays members in one workspace, each on the one before (main
// for the first), with one committer date; one that does not replay on the
// stack stays out (the workspace is opened anew without it). Then it claims
// them in order, each next claim not counting the ones before as others in
// flight, and stops at the first refusal or a claim that is not the one
// replayed. Two members claimed or more: the batch is recorded (R the tip
// of the members claimed) and prepared; one claimed lands alone.
func (c *Controller) formBatch(ctx context.Context, repo string, members []landing.BatchCandidate, after string, s landing.State, now time.Time) error {
	date := now.UTC().Truncate(time.Second)
	out := map[string]bool{}
	var w BatchWorkspace
	defer func() {
		if w != nil {
			_ = w.Close()
		}
	}()
	var stack []batchSlot
	var m0 string
	for replayed := false; !replayed; {
		if w != nil {
			_ = w.Close()
			w = nil
		}
		stack, replayed = nil, true
		for _, cand := range members {
			if out[cand.Task] {
				continue
			}
			f := cand.Facts
			var err error
			if w == nil {
				if w, err = c.openBatch(ctx, repo, f.A); err != nil {
					w = nil
					return err
				}
				if m0, err = w.Main(ctx); err != nil {
					return err
				}
				// Every other member's result in one fetch, not one each.
				var rest []string
				for _, m := range members {
					if !out[m.Task] && m.Facts.A != f.A {
						rest = append(rest, m.Facts.A)
					}
				}
				if err = w.Fetch(ctx, rest...); err != nil {
					return err
				}
			}
			_, n := landing.RootBudget(s, cand.Task)
			op := fmt.Sprintf("land:%s:%d", cand.Task, n+1)
			on := m0
			if len(stack) > 0 {
				on = stack[len(stack)-1].r
			}
			r, err := w.Replay(ctx, landinggit.ReplayInput{A: f.A, Base: f.Base, M0: on, Task: cand.Task, Attempt: f.Attempt, Mission: f.Mission,
				Reviewers: f.Reviewers, Operation: op, RuleVersion: f.Rule.Version, AcceptedAt: f.AcceptedAt, CommitterDate: date, Env: c.Env})
			if replayRefusal(err) != "" {
				out[cand.Task], replayed = true, false
				break
			}
			if err != nil {
				return err
			}
			stack = append(stack, batchSlot{cand: cand, op: op, r: r})
		}
	}
	if len(stack) == 0 {
		return nil
	}
	if len(stack) == 1 {
		op, err := c.claimOne(ctx, repo, stack[0].cand.Task, now)
		if err != nil || op == "" {
			return err
		}
		return c.LandingDrillBranch(ctx, c.DrillGit[repo], op, now, repo)
	}
	var errs []error
	var claimed []string
	for _, slot := range stack {
		op, err := c.claimOne(ctx, repo, slot.cand.Task, now, claimed...)
		if err != nil {
			errs = append(errs, err)
		}
		if op == "" {
			break
		}
		claimed = append(claimed, op)
		if op != slot.op {
			break
		}
	}
	var state landing.State
	if err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		state, e = store.LandingState(ctx, tx, "landing:"+repo)
		return e
	}); err != nil {
		return errors.Join(append(errs, err)...)
	}
	n := 0
	for i, op := range claimed {
		o, f := state.Operations[op], stack[i].cand.Facts
		if op != stack[i].op || o.A != f.A || o.Attempt != f.Attempt || o.RuleVersion != f.Rule.Version {
			break
		}
		n++
	}
	if n < 2 {
		// What was claimed lands alone: its own replay on main.
		if len(claimed) > 0 {
			errs = append(errs, c.LandingDrillBranch(ctx, c.DrillGit[repo], claimed[0], now, repo))
		}
		return errors.Join(errs...)
	}
	b := store.LandingBatch{Batch: landing.BatchID(claimed[0]), Lead: claimed[0], Members: claimed[:n], M0: m0, R: stack[n-1].r,
		CommitterDate: date, After: after}
	if err := store.AppendLandingBatch(ctx, c.DB, repo, b); err != nil {
		return errors.Join(append(errs, err)...)
	}
	c.logf("agentrt landing: batch %s of %d formed on M0 %s, R %s", b.Batch, n, b.M0, b.R)
	return errors.Join(append(errs, c.landingBatchBranch(ctx, c.DrillGit[repo], c.Drill.Facts, b, now, repo, w))...)
}

// replayRefusal is the failed reason of a replay that refuses the result:
// only a real conflict of its three-way merge onto main.
func replayRefusal(err error) string {
	if errors.Is(err, landinggit.ErrConflict) {
		return "conflict"
	}
	return ""
}

// failConflict ends op failed{conflict} and, as a red CI does, sends its
// root back for its next round told the paths that did not merge
// (store.RecordLandingConflict).
func (c *Controller) failConflict(ctx context.Context, repo, op string, err error) error {
	m0, paths := "", []string{}
	var conflict *landinggit.ConflictError
	if errors.As(err, &conflict) {
		m0, paths = conflict.M0, conflict.Paths
	}
	reworked, err := store.RecordLandingConflict(ctx, c.DB, repo, op, landingText(c.Env)+"failed", m0, paths, c.Precheck)
	if err == nil && reworked {
		c.logf("landing %s: replay conflicts in %v; its root goes back for its next round", op, paths)
	}
	return err
}

// landingBatchBranch prepares and pushes a batch's members, all with the
// batch's M0 and R (LandingDrillBranch's batch half). Each member is judged
// as the claim judges it: a rule or limit refusal waits for a later pass,
// any other ends that member and releases the rest. Unless w already holds
// the stack (just formed), the stack is replayed again, as recorded (its
// M0, order and committer date), and must give R. Then prepared, branch
// intent, the push of each land branch (a branch someone else holds ends its
// member) and branch_pushed, member by member in order.
func (c *Controller) landingBatchBranch(ctx context.Context, g DrillGit, source store.LandingSource, b store.LandingBatch, now time.Time, repo string, w BatchWorkspace) error {
	if g == nil {
		return fmt.Errorf("landing batch requires its git")
	}
	s, err := c.landingState(ctx, repo)
	if err != nil {
		return err
	}
	if b.Broken(s) {
		return c.releaseBatch(ctx, b, "stale", repo)
	}
	if !slices.ContainsFunc(b.Members, func(op string) bool {
		return slices.Contains([]string{"claimed", "prepared", "branch_intent"}, s.Operations[op].Phase)
	}) {
		return nil
	}
	facts := map[string]landing.Facts{}
	for _, op := range b.Members {
		o := s.Operations[op]
		if why := landing.RootBound(s, o.Task, store.DecisionAt(), false); why != "" {
			_, err := store.SettleLandingOperationBound(ctx, c.DB, op, o, repo)
			return err
		}
		f, d, err := c.LandingDecision(ctx, source, o.Task, op, "claim", now)
		if err != nil {
			return err
		}
		if !d.Allowed {
			switch d.Reason {
			case "rule", "limit":
				return nil
			case "floor":
				return c.endBatchMember(ctx, b, s, op, "floor", repo)
			}
			return c.endBatchMember(ctx, b, s, op, "stale", repo)
		}
		if f.A != o.A || f.Attempt != o.Attempt {
			return c.endBatchMember(ctx, b, s, op, "stale", repo)
		}
		facts[op] = f
	}
	if w == nil {
		on := b.M0
		for _, op := range b.Members {
			o, f := s.Operations[op], facts[op]
			if w == nil {
				if w, err = c.openBatch(ctx, repo, o.A); err != nil {
					return err
				}
				defer w.Close()
				var rest []string
				for _, m := range b.Members[1:] {
					rest = append(rest, s.Operations[m].A)
				}
				if err = w.Fetch(ctx, rest...); err != nil {
					return err
				}
			}
			r, err := w.Replay(ctx, landinggit.ReplayInput{A: o.A, Base: f.Base, M0: on, Task: o.Task, Attempt: o.Attempt, Mission: f.Mission,
				Reviewers: f.Reviewers, Operation: op, RuleVersion: o.RuleVersion, AcceptedAt: f.AcceptedAt, CommitterDate: b.CommitterDate, Env: c.Env})
			if replayRefusal(err) != "" {
				if err = c.failConflict(ctx, repo, op, err); err != nil {
					return err
				}
				return c.releaseBatch(ctx, b, "stale", repo)
			}
			if err != nil {
				return err
			}
			on = r
		}
		if on != b.R {
			return c.releaseBatch(ctx, b, "stale", repo)
		}
	}
	// Before the new intents and pushes: each member as of now (review 659
	// R659-1). Out of it, nothing new is written; the next pass judges.
	at := store.DecisionAt()
	for _, op := range b.Members {
		f, err := store.RefreshLandingFacts(ctx, c.DB, source, c.Env, mode(c.Env), s.Operations[op].Task, op, facts[op], at)
		if err != nil {
			return err
		}
		if !landing.PreparationDecision(f, at).Allowed {
			return nil
		}
	}
	phase := map[string]string{}
	for _, op := range b.Members {
		phase[op] = s.Operations[op].Phase
	}
	step := func(from, to string, fields func(op string) map[string]any) error {
		for _, op := range b.Members {
			if phase[op] == from {
				if err := c.appendLanding(ctx, op, to, fields(op), repo); err != nil {
					return err
				}
				phase[op] = to
			}
		}
		return nil
	}
	if err = step("claimed", "prepared", func(string) map[string]any {
		return map[string]any{"R": b.R, "M0": b.M0, "committer_date": b.CommitterDate.UTC().Format(time.RFC3339Nano), "dry_run": c.Env != "prod"}
	}); err != nil {
		return err
	}
	ref := func(op string) string {
		k := op[strings.LastIndex(op, ":")+1:]
		return "refs/heads/land/" + c.Env + "/" + s.Operations[op].Task + "/" + k
	}
	if err = step("prepared", "branch_intent", func(op string) map[string]any { return map[string]any{"ref": ref(op), "R": b.R} }); err != nil {
		return err
	}
	for _, op := range b.Members {
		if phase[op] != "branch_intent" {
			continue
		}
		tip, err := g.Ref(ctx, ref(op))
		if err != nil {
			return err
		}
		if tip != "" && tip != b.R {
			return c.endBatchMember(ctx, b, s, op, "branch_collision", repo)
		}
		if tip == "" {
			if err = w.Push(ctx, ref(op), b.R); err != nil {
				return err
			}
		}
		if err = c.appendLanding(ctx, op, "branch_pushed", map[string]any{"ref": ref(op), "R": b.R}, repo); err != nil {
			return err
		}
	}
	return nil
}

// endBatchMember ends op, one member of b, and releases the others:
// failed{why}, or discarded{branch_collision} for a branch someone else
// holds (failed{bound} at its root's eighth operation).
func (c *Controller) endBatchMember(ctx context.Context, b store.LandingBatch, s landing.State, op, why, repo string) error {
	kind, fields := "failed", map[string]any{"why": why}
	if why == "branch_collision" {
		kind, fields = "discarded", map[string]any{"why": why}
		if _, n := landing.RootBudget(s, s.Operations[op].Task); n >= 8 {
			kind, fields = "failed", map[string]any{"why": "bound"}
		}
	}
	if err := c.appendLanding(ctx, op, kind, fields, repo); err != nil {
		return err
	}
	return c.releaseBatch(ctx, b, "stale", repo)
}

// releaseBatch ends b's members that have not reached main
// (store.ReleaseLandingBatch); each root's next claim joins a new batch.
func (c *Controller) releaseBatch(ctx context.Context, b store.LandingBatch, why, repo string) error {
	n, err := store.ReleaseLandingBatch(ctx, c.DB, repo, b.Batch, why, landingText(c.Env))
	if err == nil && n > 0 {
		c.logf("agentrt landing: batch %s released (%s): %d member(s) land in a later batch", b.Batch, why, n)
	}
	return err
}

// batchFollows: op's CI step does nothing of its own: its batch is broken
// (released now), op is not the lead (it follows the lead's CI), or a
// member is still being prepared (landingBatchBranch finishes it first).
func (c *Controller) batchFollows(ctx context.Context, b store.LandingBatch, s landing.State, op, repo string) (bool, error) {
	if b.Broken(s) {
		return true, c.releaseBatch(ctx, b, "stale", repo)
	}
	if op != b.Lead {
		return true, nil
	}
	for _, m := range b.Members {
		if p := s.Operations[m].Phase; p != "branch_pushed" && p != "parked" && p != "awaiting_grant" {
			return true, nil
		}
	}
	return false, nil
}

// batchPrepared is when b's last member was prepared: the batch's CI must
// be created after it, so the lead waits for it from then.
func batchPrepared(s landing.State, b store.LandingBatch) time.Time {
	var last time.Time
	for _, op := range b.Members {
		if at := s.Operations[op].PreparedAt; at.After(last) {
			last = at
		}
	}
	return last
}

// batchCISeen records the lead's run on R as ci_seen of every other member
// that has not seen it.
func (c *Controller) batchCISeen(ctx context.Context, b store.LandingBatch, s landing.State, ci landing.CIRun, at time.Time, repo string) error {
	for _, op := range b.Members {
		m := s.Operations[op]
		if op == b.Lead || m.Phase != "branch_pushed" || !landing.CIChanged(m.CI, ci) || ci.Run == "" || ci.R != m.R || !ci.CreatedAt.After(m.PreparedAt) || ci.CreatedAt.After(at) {
			continue
		}
		if err := c.appendLanding(ctx, op, "ci_seen", landing.CIRecord(op, ci), repo); err != nil {
			return err
		}
	}
	return nil
}

// failBatchRed ends a red batch (store.RecordBatchRed): the CI failure is
// read once, and blamed on the members whose changed files lie in the
// packages it names (landing.BlameRed).
func (c *Controller) failBatchRed(ctx context.Context, src DrillSources, repo string, b store.LandingBatch, ci landing.CIRun, now time.Time) error {
	failure := landing.CIFailure{Pipeline: landing.LandingRepos[repo].CIPipeline, Run: ci.Run}
	if src.CIFailure != nil && ci.Run != "" {
		read, err := src.CIFailure(ctx, ci.Run)
		if err != nil {
			failure.ReadError = err.Error()
		} else {
			failure = read
		}
	}
	s, err := c.landingState(ctx, repo)
	if err != nil {
		return err
	}
	var members []landing.BatchMember
	for _, op := range b.Members {
		o := s.Operations[op]
		m := landing.BatchMember{Op: op, Task: o.Task}
		// A result that cannot be read now is blamed on nothing.
		if f, _, err := c.LandingDecision(ctx, src.Facts, o.Task, op, "push", now); err == nil {
			for _, ch := range f.Changes {
				m.Paths = append(m.Paths, ch.Paths()...)
			}
		}
		members = append(members, m)
	}
	var blamed []string
	for _, m := range landing.BlameRed(failure, members) {
		blamed = append(blamed, m.Op)
	}
	reworked, err := store.RecordBatchRed(ctx, c.DB, repo, b.Batch, failure, blamed, landingText(c.Env)+"failed", c.Precheck)
	if err == nil {
		c.logf("agentrt landing: batch %s CI %s red; blamed %v, back for rework %v", b.Batch, ci.Run, blamed, reworked)
	}
	return err
}

// landBatch is landLiveWith for a batch: one workspace holding R, the batch
// gate for every member (store.AuthorizeBatchPush, or RecheckWaitingBatch
// for a waiting lead with no grant) reading main, CI and the window once,
// every intent still current just before, then R pushed to main once, never
// past the earliest intent's deadline, and its answer recorded for each.
func (c *Controller) landBatch(ctx context.Context, src DrillSources, b store.LandingBatch, recheck bool, decided *landing.Decision, repo string) error {
	var w LiveWorkspace
	var err error
	if c.OpenLive != nil {
		w, err = c.OpenLive(ctx, repo, b.R)
	} else {
		w, err = c.DrillGit[repo].Open(ctx, b.R)
	}
	if err != nil {
		return err
	}
	defer w.Close()
	gate := store.AuthorizeBatchPush
	if recheck {
		gate = store.RecheckWaitingBatch
	}
	var seen *store.LiveObservation
	auths, d, err := gate(ctx, c.DB, src.Facts, b.Members, func(ctx context.Context, o landing.Operation) (store.LiveObservation, error) {
		if seen != nil {
			return *seen, nil
		}
		main, err := src.Main(ctx, repo)
		if err != nil {
			return store.LiveObservation{}, err
		}
		ci, err := candidateCI(ctx, src, repo, o)
		if err != nil {
			return store.LiveObservation{}, err
		}
		blocked, err := src.WindowBlocked(ctx, repo)
		if err != nil {
			return store.LiveObservation{}, err
		}
		seen = &store.LiveObservation{Main: main, CI: ci, WindowBlocked: blocked}
		return *seen, nil
	}, repo)
	if decided != nil && err == nil {
		*decided = d
	}
	if err != nil {
		return err
	}
	if !d.Allowed {
		return c.batchRefused(ctx, b, d, repo)
	}
	deadline := time.Time{}
	for _, a := range auths {
		if a.R != b.R || a.M0 != b.M0 {
			return fmt.Errorf("landing authorization for another commit")
		}
		if current, err := store.AuthorizationCurrent(ctx, c.DB, a); err != nil || !current {
			return err
		}
		if deadline.IsZero() || a.Deadline.Before(deadline) {
			deadline = a.Deadline
		}
	}
	var out landinggit.PushOutcome
	if len(b.Members) == 1 {
		out = w.PushMain(context.WithoutCancel(ctx), b.R, b.M0, deadline)
	} else if batchWriter, ok := w.(interface {
		PushBatchMain(context.Context, string, string, time.Time, int) landinggit.PushOutcome
	}); ok {
		out = batchWriter.PushBatchMain(context.WithoutCancel(ctx), b.R, b.M0, deadline, len(b.Members))
	} else {
		out = landinggit.PushNotSent
	}
	outcome := string(out)
	if out == landinggit.PushNotSent {
		outcome = string(landinggit.PushUnknown)
	}
	// The identity the push went as is recorded beside its outcome, as a
	// single landing's is (landing_live.go).
	pusher := ""
	if p, ok := w.(interface{ Pusher() (string, error) }); ok {
		if name, err := p.Pusher(); err == nil {
			pusher = name
		}
	}
	rec, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	var errs []error
	for _, a := range auths {
		wrote, err := store.RecordPushResult(rec, c.DB, a, outcome, pusher)
		c.logf("agentrt landing %s (batch %s): push of main %s as %q (recorded %v)", a.Op, b.Batch, out, pusher, wrote)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// batchRefused answers the batch gate's refusal of a member other than the
// lead, when it will not pass by itself: a member whose root ended (stale,
// result, history, terminal, floor) ends, and the rest are released; one
// that now needs a grant or a rule (only a lead waits) releases the batch.
// Anything else (main, CI, window, limit) is tried again next pass.
func (c *Controller) batchRefused(ctx context.Context, b store.LandingBatch, d landing.Decision, repo string) error {
	op, _ := d.Detail["op"].(string)
	if op == "" || op == b.Lead {
		return nil
	}
	switch d.Reason {
	case "awaiting_grant", "rule":
		return c.releaseBatch(ctx, b, "stale", repo)
	case "floor", "stale", "result", "history", "terminal", "before_enable":
		s, err := c.landingState(ctx, repo)
		if err != nil {
			return err
		}
		why := "stale"
		if d.Reason == "floor" {
			why = "floor"
		}
		return c.endBatchMember(ctx, b, s, op, why, repo)
	}
	return nil
}

func (c *Controller) landingState(ctx context.Context, repo string) (landing.State, error) {
	var s landing.State
	err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		s, e = store.LandingState(ctx, tx, "landing:"+repo)
		return e
	})
	return s, err
}

// appendLanding appends one landing record of op on repo's stream.
func (c *Controller) appendLanding(ctx context.Context, op, kind string, fields map[string]any, repo string) error {
	fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
	return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "landing:" + repo, Type: "note", Actor: "controller", Body: map[string]any{"text": landingText(c.Env) + kind, "landing": fields}})
		return err
	})
}
