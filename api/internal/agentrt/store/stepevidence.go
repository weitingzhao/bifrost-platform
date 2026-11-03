package store

import (
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// The step's own work as evidence (DESIGN-module-progress-2026-10-31 D2,
// D3). step_tasks, step_landed and step_stg_settled name no record in
// advance: inside the evaluation's transaction they read the step's tasks
// (task.plan_node_id = step), the authoritative fold of each task's
// repository landing stream (LandingState) and the B5 STG fold
// (StgReleaseState). Nothing outside the database is read, so the controller
// keeps no watch for them: the rules loop reads them on every pass.

var _ plan.StepFacts = (*stepFacts)(nil)

// StepTasks: at least min of the step's own tasks are in state. A
// verification or review sits on its subject's step and is accepted too, so
// only tasks that check no other task count: one deliverable counts once.
func (f *stepFacts) StepTasks(state string, min int64) plan.Tri {
	if f.err != nil {
		return plan.Unknown
	}
	var n int64
	f.err = f.tx.QueryRow(f.ctx, `SELECT count(*) FROM agentrt.task WHERE plan_node_id = $1 AND subject_task_id IS NULL AND state = $2`,
		f.step, state).Scan(&n)
	switch {
	case f.err != nil:
		return plan.Unknown
	case n >= min:
		return plan.True
	}
	return plan.False
}

// StepLanded: the step has an accepted implementation that writes code
// (effect_class branch), and every such implementation landed on main
// through the runtime. Each one must land (review 630): one with no real
// landing evidence (a repository the runtime does not land, a rehearsal
// only, no operation and outside the handoff) is False, never left out, so
// one landed task cannot mask another. False too when there is none, when
// one ended without its change on main (failed, reverted) and while one is
// still on its way (claimed, parked, awaiting a grant, due a replay, or in
// the handoff and not claimed yet): the fold records each of these, so it
// is a known no. Unknown only while a push to main has no settled answer
// (main_intent, main_result other than non_ff): the fold itself says
// main's answer is not known yet. An implementation that writes no code
// (effect_class none or external) has nothing to land and is not read.
//
// One the Owner declined (failed{declined}) whose same change landed from
// another root of the step does not hold it back (R5:5.6-1): the controller
// recorded that (landing_delivered_by), and the latest such note names a
// root landed in this very read. A root truly landed is still needed.
func (f *stepFacts) StepLanded() plan.Tri {
	if l := f.landing(); l != nil && f.err == nil {
		return l.value
	}
	return plan.Unknown
}

// StepSTGSettled: step_landed holds and B5 verified every operation that
// landed on STG. Settled implies landed: work still to land, or landed and
// lost, keeps it from holding. Unknown while a batch holding one has not
// settled; False for one never admitted, released, exhausted or withheld.
// B5 releases bifrost-platform only (StgReleaseStream): work landed in any
// other repository has none of B5's; one with no STG release at all
// (noSTGRelease: bifrost-trade-infra) counts as settled once landed, and a
// step with only such work holds with step_landed (R5:5.5-9, Owner
// 2026-11-02: a documents-only step is done when it lands; before, it
// stayed False for ever).
func (f *stepFacts) StepSTGSettled() plan.Tri {
	l := f.landing()
	switch {
	case l == nil || f.err != nil:
		return plan.Unknown
	case l.value != plan.True:
		return l.value
	case l.stgRead:
		return l.stg
	}
	l.stg, l.stgRead = plan.True, true
	var stg *StgState
	for _, r := range l.landed {
		if "landing:"+r.repo != StgReleaseStream {
			if noSTGRelease[r.repo] {
				continue // landed is all there is
			}
			l.stg = plan.False
			return l.stg
		}
		if stg == nil {
			s, err := f.stgFold()
			if err != nil {
				f.err, l.stgRead = err, false
				return plan.Unknown
			}
			stg = s
		}
		switch stgSettled(*stg, r.key) {
		case plan.False:
			l.stg = plan.False
			return l.stg
		case plan.Unknown:
			l.stg = plan.Unknown
		}
	}
	return l.stg
}

// landingFold is repo's landing fold: read here, or once for every step of
// an explanation (f.repos, R5:5.5-19 §5).
func (f *stepFacts) landingFold(repo string) (landing.State, error) {
	if f.repos == nil {
		return LandingState(f.ctx, f.tx, "landing:"+repo)
	}
	ex, err := f.explainRepo(repo)
	if err != nil {
		return landing.State{}, err
	}
	return ex.fold, nil
}

// stgFold is B5's fold, shared the same way.
func (f *stepFacts) stgFold() (*StgState, error) {
	if f.repos == nil {
		s, err := StgReleaseState(f.ctx, f.tx)
		return &s, err
	}
	ex, err := f.explainRepo(stgReleaseRepo)
	if err == nil && ex.stg == nil {
		err = ex.readSTG(f)
	}
	if err != nil {
		return nil, err
	}
	return ex.stg, nil
}

// noSTGRelease names the repositories the runtime lands whose work has no
// STG release at all, so it is settled once landed (R5:5.5-9). Any other
// repository that is not B5's may have a release of its own that no step
// evidence reads yet, so its landed work stays False until it is named here
// or read (review of 24642337: fail closed).
var noSTGRelease = map[string]bool{"bifrost-trade-infra": true}

// stepLanding is what the step's accepted implementations' landing comes
// to, read once per evaluation.
type stepLanding struct {
	value   plan.Tri
	landed  []landedRoot
	stg     plan.Tri
	stgRead bool
	// roots is each implementation with its final status, in task order:
	// what the explanation reads (R5:5.5-19 §4.2), so it can never judge a
	// root differently from the value.
	roots []implRoot
	// siblings are the landed siblings read for each declined root that
	// stayed a no: the explanation's rework set is this same read.
	siblings map[string][]ReworkSibling
}

// implRoot is one accepted implementation and where its landing stands.
type implRoot struct {
	task, repo string
	status     int
	op         string
	o          landing.Operation
	has        bool // the fold has op
	accepted   *time.Time
}

// landedRoot is a root whose latest operation landed: its repository and
// its STG member identity (root, operation, landed R).
type landedRoot struct {
	repo string
	key  StgMemberKey
}

// Where one accepted implementation's landing stands.
const (
	rootNone = iota // no real landing evidence: it cannot land as things are
	rootLanded
	rootPending
	rootUnknown
	rootFailed
	// rootDelivered: declined by the Owner, its same change landed from
	// another root of the step (landing_delivered_by): neither a no nor a
	// landing of its own.
	rootDelivered
)

// landing reads, once, each accepted implementation of the step that writes
// code and where its landing stands in its repository's fold; nil after a
// read error.
func (f *stepFacts) landing() *stepLanding {
	if f.err != nil || f.landed != nil {
		return f.landed
	}
	type root struct {
		task, repo string
		created    time.Time
		noMerge    bool
		accepted   *time.Time
	}
	rows, err := f.tx.Query(f.ctx, `SELECT t.task_id, COALESCE(t.repo, ''), m.created_at, COALESCE(m.scope_json->>'no_merge', '') <> '',
	    (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted')
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  WHERE t.plan_node_id = $1 AND t.role = 'implement' AND t.state = 'accepted' AND t.effect_class = 'branch'
	  ORDER BY t.task_id`, f.step)
	if err != nil {
		f.err = err
		return nil
	}
	roots, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (root, error) {
		var r root
		return r, row.Scan(&r.task, &r.repo, &r.created, &r.noMerge, &r.accepted)
	})
	if err != nil {
		f.err = err
		return nil
	}
	folds := map[string]landing.State{}
	statuses := make([]int, 0, len(roots))
	l := &stepLanding{}
	type declined struct {
		i              int
		repo, task, op string
	}
	var declinedRoots []declined
	for _, r := range roots {
		if !landing.RepoSupported(r.repo) {
			statuses = append(statuses, rootNone)
			l.roots = append(l.roots, implRoot{task: r.task, repo: r.repo, status: rootNone, accepted: r.accepted})
			continue
		}
		s, ok := folds[r.repo]
		if !ok {
			if s, err = f.landingFold(r.repo); err != nil {
				f.err = err
				return nil
			}
			folds[r.repo] = s
		}
		status, op, o := rootLanding(s, r.task, landingHandoff(s.Fence(), r.accepted, r.created, r.noMerge))
		if status == rootFailed && o.Phase == "failed" && o.Outcome == "declined" {
			declinedRoots = append(declinedRoots, declined{len(statuses), r.repo, r.task, op})
		}
		statuses = append(statuses, status)
		_, has := s.Operations[op]
		l.roots = append(l.roots, implRoot{task: r.task, repo: r.repo, status: status, op: op, o: o, has: has, accepted: r.accepted})
		if status == rootLanded {
			l.landed = append(l.landed, landedRoot{repo: r.repo, key: StgMemberKey{Root: r.task, Op: op, R: o.R}})
		}
	}
	// A declined root steps aside only for a delivery this read sees landed
	// (DESIGN-step-landed-superseded-2026-11-01 §6 item 4): a by moved off
	// the step, reverted, or never landed leaves it a no.
	landed := map[string]bool{}
	for _, x := range l.landed {
		landed[x.key.Root] = true
	}
	for _, d := range declinedRoots {
		by, err := landingDeliveredBy(f.ctx, f.tx, "landing:"+d.repo, d.task, d.op)
		if err != nil {
			f.err = err
			return nil
		}
		if landed[by] {
			statuses[d.i] = rootDelivered
			continue
		}
		// Or the Owner counted its work delivered by reworked siblings
		// that are all still landed on the step (R5:5.5-18, reworkExcuse).
		// It is not added to the landed roots: it has no STG of its own.
		sibs, err := landedSiblings(f.ctx, f.tx, folds[d.repo], d.repo, f.step, d.task)
		if err != nil {
			f.err = err
			return nil
		}
		if l.siblings == nil {
			l.siblings = map[string][]ReworkSibling{}
		}
		l.siblings[d.task] = sibs
		ok, err := reworkExcuse(f.ctx, f.tx, "landing:"+d.repo, d.task, d.op, sibs)
		if err != nil {
			f.err = err
			return nil
		}
		if ok {
			statuses[d.i] = rootDelivered
		}
	}
	for i := range l.roots {
		l.roots[i].status = statuses[i]
	}
	l.value = landingValue(statuses)
	f.landed = l
	return l
}

// landingHandoff: a root with no operation yet is in the stream's first-claim
// handoff (CUT §7.7): accepted no earlier than the fence, in a mission
// created no earlier than it and not no_merge. The claim loop takes it or
// the drill pass ends it (SettleUnclaimed); close holds a mission for it.
func landingHandoff(fence time.Time, accepted *time.Time, created time.Time, noMerge bool) bool {
	return !fence.IsZero() && accepted != nil && !accepted.Before(fence) && !created.Before(fence) && !noMerge
}

// rootLanding is where root's landing stands in its repository's fold, by
// its latest operation in claim order (LatestOperation, as close and the
// drill read it), with that operation.
func rootLanding(s landing.State, root string, handoff bool) (int, string, landing.Operation) {
	op := s.LatestOperation(root)
	o, has := s.Operations[op]
	switch {
	case !has && handoff:
		return rootPending, op, o
	case !has || o.DryRun: // a rehearsal never moves main
		return rootNone, op, o
	case o.Phase == "landed":
		return rootLanded, op, o
	case o.AtMain():
		return rootUnknown, op, o
	case !o.Terminal() || s.AwaitsNext(root):
		return rootPending, op, o
	}
	return rootFailed, op, o
}

// landingValue is step_landed over the roots' statuses (Kleene: a no
// outweighs an unknown). A delivered root counts neither way, so a step
// with no root truly landed stays a no.
func landingValue(statuses []int) plan.Tri {
	landed, unknown := false, false
	for _, s := range statuses {
		switch s {
		case rootNone, rootFailed, rootPending:
			return plan.False
		case rootUnknown:
			unknown = true
		case rootLanded:
			landed = true
		}
	}
	switch {
	case unknown:
		return plan.Unknown
	case landed:
		return plan.True
	}
	return plan.False
}

// stgSettled is whether B5 verified member m on STG: True once its batch
// settled it delivered (delivery needs a verified execution, applyMember)
// or trusted evidence covers it (covers; classifyStgRoot counts both as
// delivery); Unknown while the batch holding it has not settled; False when
// it was never admitted, or was released, exhausted, disposed or withheld.
func stgSettled(s StgState, m StgMemberKey) plan.Tri {
	if s.Withheld[m].Seq > 0 {
		return plan.False
	}
	member, admitted := s.Members[m]
	if admitted {
		if b := s.Batches[member.Batch]; b == nil || b.Phase != "CLOSED" {
			return plan.Unknown
		}
	}
	if member.To == "delivered" || s.covered(m) {
		return plan.True
	}
	return plan.False
}
