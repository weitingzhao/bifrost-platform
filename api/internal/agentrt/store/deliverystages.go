package store

import (
	"slices"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Delivery stages (goal 3): one read gives a change's way from handed in to
// on PROD, with when each stage was entered and left, who acted and what it
// waits on. FoldDelivery is pure: the caller reads the attempt, the landing
// stream, the grant, the release cycles and the deploy events, and the fold
// only orders what they say. A source the caller did not give (a nil slice)
// makes the stages that need it unknown, never inferred.

// The seven stages, always in this order.
const (
	StageHandedIn    = "handed_in"
	StageLandingCI   = "landing_ci"
	StageOwnerWait   = "owner_wait"
	StageOnMain      = "on_main"
	StageSTG         = "stg"
	StageProdRelease = "prod_release"
	StageOnProd      = "on_prod"
)

// DeliveryStageOrder is the stages FoldDelivery returns, in order.
var DeliveryStageOrder = []string{StageHandedIn, StageLandingCI, StageOwnerWait, StageOnMain, StageSTG, StageProdRelease, StageOnProd}

// A stage's state.
const (
	StageDone       = "done"
	StageCurrent    = "current"
	StageNotReached = "not_reached"
	StageUnknown    = "unknown"
)

// DeliveryStage is one stage of a change's delivery.
type DeliveryStage struct {
	Stage     string    `json:"stage"`
	State     string    `json:"state"`
	EnteredAt time.Time `json:"entered_at,omitzero"`
	LeftAt    time.Time `json:"left_at,omitzero"`
	Actor     string    `json:"actor,omitempty"`
	// ActorRole: executor, approver, pusher or releaser.
	ActorRole string `json:"actor_role,omitempty"`
	WaitsOn   string `json:"waits_on,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Source    string `json:"source,omitempty"`
}

// ReleaseCycle is a PROD release cycle as the fold reads it. It is defined
// here because store does not import promote: the API layer maps
// promote.ReleaseCycleRecord into it.
type ReleaseCycle struct {
	Revision              string    `json:"revision"`
	ProdDeployStartedAt   time.Time `json:"prod_deploy_started_at,omitzero"`
	ProdDeployCompletedAt time.Time `json:"prod_deploy_completed_at,omitzero"`
	ProdDeployResult      string    `json:"prod_deploy_result,omitempty"`
	ProdGatePassedAt      time.Time `json:"prod_gate_passed_at,omitzero"`
	TriggeredBy           string    `json:"triggered_by,omitempty"`
}

// DeliveryAttempt is the attempt that handed the change in.
type DeliveryAttempt struct {
	TaskID      string    `json:"task_id"`
	AttemptID   string    `json:"attempt_id"`
	Executor    string    `json:"executor"`
	SubmittedAt time.Time `json:"submitted_at,omitzero"`
	AcceptedAt  time.Time `json:"accepted_at,omitzero"`
}

// DeliveryGrant is the grant that ended the Owner's wait: who approved it
// and when (GrantEvidence.ApprovedAt).
type DeliveryGrant struct {
	Approver   string    `json:"approver"`
	ApprovedAt time.Time `json:"approved_at"`
}

// DeliveryRevision is a revision P that STG verified, and when.
type DeliveryRevision struct {
	P  string    `json:"p"`
	At time.Time `json:"at"`
}

// DeliveryDeploySeen is a deploy_seen event of env prod.
type DeliveryDeploySeen struct {
	Commit string    `json:"commit"`
	At     time.Time `json:"at"`
}

// DeliveryProduction is a current release record validated against matching
// STG build digests and settled PROD pods. Reason means the read is unknown.
// It is a read result, not a fabricated deploy_seen event or release gate.
type DeliveryProduction struct {
	Commit                 string
	StartedAt, CompletedAt time.Time
	Reason                 string
}

// DeliveryInput is everything FoldDelivery reads.
type DeliveryInput struct {
	Attempt DeliveryAttempt
	// Entries are the landing records of the change's operation (and of the
	// STG operation that carried its R) in stream order.
	Entries []landing.Entry
	// Grant is nil when no grant evidence was found.
	Grant *DeliveryGrant
	// Pusher is the actor of the main_result{ok} record.
	Pusher string
	// Verified, Cycles and DeploySeen: nil when the source was not read; an
	// empty non-nil slice when it was read and holds nothing.
	Verified []DeliveryRevision
	// Coverage is a validated STG covers record for this exact landed result.
	Coverage   *DeliveryRevision
	Cycles     []ReleaseCycle
	DeploySeen []DeliveryDeploySeen
	Production *DeliveryProduction
}

// deliveryFacts is what the landing entries say.
type deliveryFacts struct {
	claimed, kicked, ciGreen time.Time
	awaiting, waitEnded      time.Time
	landed                   time.Time
	stgPending, stgVerified  time.Time
	carried                  string // the STG-verified P that carried R
	blocked                  string // why the operation stopped, "" when it did not
	hasAwaiting, reachedMain bool
}

func deliveryFactsOf(entries []landing.Entry) deliveryFacts {
	var f deliveryFacts
	first := func(t *time.Time, at time.Time) {
		if t.IsZero() {
			*t = at
		}
	}
	snapshotP := ""
	for _, e := range entries {
		r := e.Record
		kind := r.String("kind")
		if f.hasAwaiting && f.waitEnded.IsZero() && !slices.Contains([]string{"awaiting_grant", "ci_seen", "failed", "parked"}, kind) {
			f.waitEnded = e.At
		}
		switch kind {
		case "claimed":
			first(&f.claimed, e.At)
		case "ci_kick":
			first(&f.kicked, e.At)
		case "ci_seen":
			if r.String("status") == "Succeeded" {
				first(&f.ciGreen, e.At)
			}
		case "awaiting_grant":
			f.hasAwaiting = true
			first(&f.awaiting, e.At)
		case "main_intent":
			f.reachedMain = true
		case "landed":
			f.reachedMain = true
			first(&f.landed, e.At)
		case "stg_pending":
			first(&f.stgPending, e.At)
		case "stg_snapshot":
			snapshotP = r.String("P")
		case "stg_verified":
			first(&f.stgVerified, e.At)
			if f.carried == "" {
				f.carried = r.String("P")
			}
		}
		switch kind {
		case "failed", "stg_failed":
			f.blocked = "failed: " + r.String("why")
		case "parked":
			f.blocked = "parked: " + r.String("why")
		case "unparked":
			f.blocked = ""
		}
	}
	if f.carried == "" && !f.stgVerified.IsZero() {
		f.carried = snapshotP
	}
	return f
}

// FoldDelivery gives the seven stages of in's change, in order.
func FoldDelivery(in DeliveryInput) []DeliveryStage {
	f := deliveryFactsOf(in.Entries)
	if in.Coverage != nil && f.stgVerified.IsZero() {
		f.stgVerified, f.carried = in.Coverage.At, in.Coverage.P
	}
	st := make([]DeliveryStage, len(DeliveryStageOrder))
	for i, name := range DeliveryStageOrder {
		st[i] = DeliveryStage{Stage: name}
	}
	// reached: the stage is done; unknown: why its source is missing.
	reached := make([]bool, len(st))
	unknown := make([]string, len(st))

	h := &st[0]
	h.EnteredAt, h.LeftAt, h.Actor, h.ActorRole, h.Source = in.Attempt.SubmittedAt, f.claimed, in.Attempt.Executor, "executor", "attempt"
	h.WaitsOn = "landing claim"
	reached[0] = !f.claimed.IsZero()

	c := &st[1]
	c.EnteredAt, c.LeftAt, c.WaitsOn, c.Source = f.kicked, f.ciGreen, "landing CI", "landing"
	if c.EnteredAt.IsZero() {
		c.EnteredAt = f.claimed
	}
	reached[1] = !f.ciGreen.IsZero()

	o := &st[2]
	skipOwner := !f.hasAwaiting
	if skipOwner {
		if f.reachedMain {
			o.Reason = "no grant needed"
		}
	} else {
		o.EnteredAt, o.WaitsOn, o.Source = f.awaiting, "owner grant", "landing"
		switch {
		case in.Grant != nil:
			o.LeftAt, o.Actor, o.ActorRole, o.Source = in.Grant.ApprovedAt, in.Grant.Approver, "approver", "grant"
		default:
			o.LeftAt = f.waitEnded
		}
		reached[2] = !o.LeftAt.IsZero()
	}

	m := &st[3]
	m.EnteredAt, m.Source, m.WaitsOn = f.landed, "landing", "push to main"
	if !f.landed.IsZero() {
		m.Actor, m.ActorRole, m.WaitsOn = in.Pusher, "pusher", "STG release"
	}

	s := &st[4]
	s.EnteredAt, s.LeftAt, s.WaitsOn, s.Source = f.stgPending, f.stgVerified, "STG verification", "landing"
	if in.Coverage != nil && s.EnteredAt.IsZero() {
		s.EnteredAt, s.Source = in.Coverage.At, "stg_coverage"
	}
	reached[4] = !f.stgVerified.IsZero()
	// on_main ends when STG takes the change up.
	reached[3] = !f.landed.IsZero() && (!f.stgPending.IsZero() || reached[4])

	// The revisions that carry R: the verified P that carried it, and every
	// P verified after it.
	var carriers []string
	switch {
	case in.Verified == nil:
		unknown[5] = "no STG-verified revisions given"
	case f.carried == "":
		unknown[5] = "the STG-verified revision that carried R is not on the landing stream"
	default:
		carriers = []string{f.carried}
		if i := slices.IndexFunc(in.Verified, func(v DeliveryRevision) bool { return v.P == f.carried }); i >= 0 {
			for _, v := range in.Verified[i+1:] {
				carriers = append(carriers, v.P)
			}
		}
	}

	p := &st[5]
	p.WaitsOn, p.Source = "PROD release", "release_cycle"
	var cycle *ReleaseCycle
	if unknown[5] == "" {
		if in.Cycles == nil {
			unknown[5] = "no release cycles given"
		} else {
			cycle = deliveryCycle(in.Cycles, carriers)
		}
	}
	if cycle != nil {
		p.EnteredAt, p.LeftAt, p.Actor = deliveryCycleStart(in.Cycles, carriers), cycle.ProdDeployCompletedAt, cycle.TriggeredBy
		if p.Actor != "" {
			p.ActorRole = "releaser"
		}
		if cycle.ProdDeployResult == "failed" && cycle.ProdGatePassedAt.IsZero() {
			p.Reason = "prod deploy failed"
			p.LeftAt = time.Time{}
		}
		reached[5] = !p.LeftAt.IsZero()
	}

	q := &st[6]
	q.Source = "release_cycle"
	if cycle != nil && !cycle.ProdGatePassedAt.IsZero() {
		q.EnteredAt = cycle.ProdGatePassedAt
	} else if carriers != nil {
		for _, d := range in.DeploySeen {
			if slices.Contains(carriers, d.Commit) && (q.EnteredAt.IsZero() || d.At.Before(q.EnteredAt)) {
				q.EnteredAt, q.Source = d.At, "deploy_seen"
			}
		}
	}
	switch {
	case !q.EnteredAt.IsZero():
		reached[6] = true
	case carriers == nil:
		unknown[6] = unknown[5]
	case in.Cycles == nil && in.DeploySeen == nil:
		unknown[6] = "no release cycles or PROD deploy events given"
	}

	if observed := in.Production; observed != nil {
		why := observed.Reason
		if why == "" && (observed.StartedAt.IsZero() || observed.CompletedAt.IsZero() || observed.CompletedAt.Before(observed.StartedAt)) {
			why = "current PROD release evidence has incomplete times"
		}
		if why == "" && !slices.Contains(carriers, observed.Commit) {
			why = "current PROD revision is not verified as carrying this change"
		}
		if why != "" {
			*p = DeliveryStage{Stage: p.Stage, Source: "release_record"}
			*q = DeliveryStage{Stage: q.Stage, Source: "release_record"}
			unknown[5], unknown[6] = why, why
			reached[5], reached[6] = false, false
		} else {
			unknown[5], unknown[6] = "", ""
			p.EnteredAt, p.LeftAt, p.Source, p.Reason = observed.StartedAt, observed.CompletedAt, "release_record", ""
			q.EnteredAt, q.Source = observed.CompletedAt, "release_record"
			reached[5], reached[6] = true, true
		}
	}

	// left_at falls back to the next present stage's entered_at: a stage
	// the change went past is done. An unknown stage stays unknown.
	for i := range st {
		if !st[i].LeftAt.IsZero() || i == 6 || (i == 2 && skipOwner) || unknown[i] != "" || st[i].Reason != "" {
			continue
		}
		for j := i + 1; j < len(st); j++ {
			if j == 2 && skipOwner {
				continue
			}
			if !st[j].EnteredAt.IsZero() {
				st[i].LeftAt, reached[i] = st[j].EnteredAt, true
				break
			}
		}
	}

	// States: done up to the first stage not done; that one is current
	// (with the stop as its reason), the rest not reached. A stage whose
	// source is missing is unknown and does not stop the walk.
	current := false
	for i := range st {
		x := &st[i]
		switch {
		case i == 2 && skipOwner:
			x.State = StageNotReached
		case current:
			x.State = StageNotReached
			x.WaitsOn = ""
		case reached[i]:
			x.State = StageDone
			x.WaitsOn = ""
		case unknown[i] != "":
			x.State, x.Reason, x.WaitsOn = StageUnknown, unknown[i], ""
		default:
			x.State, current = StageCurrent, true
			if f.blocked != "" {
				x.Reason = f.blocked
			}
		}
	}
	return st
}

// deliveryCycle is the cycle that decides the change's PROD release: the
// first one of a carrying revision whose gate passed, else the first that
// completed without failing, else the last one started.
func deliveryCycle(cycles []ReleaseCycle, carriers []string) *ReleaseCycle {
	var started, completed, passed *ReleaseCycle
	for i := range cycles {
		c := &cycles[i]
		if !slices.Contains(carriers, c.Revision) || c.ProdDeployStartedAt.IsZero() {
			continue
		}
		if passed == nil && !c.ProdGatePassedAt.IsZero() {
			passed = c
		}
		if completed == nil && !c.ProdDeployCompletedAt.IsZero() && c.ProdDeployResult != "failed" {
			completed = c
		}
		if started == nil || c.ProdDeployStartedAt.After(started.ProdDeployStartedAt) {
			started = c
		}
	}
	switch {
	case passed != nil:
		return passed
	case completed != nil:
		return completed
	}
	return started
}

// deliveryCycleStart is when the first cycle of a carrying revision started.
func deliveryCycleStart(cycles []ReleaseCycle, carriers []string) time.Time {
	var t time.Time
	for _, c := range cycles {
		if slices.Contains(carriers, c.Revision) && !c.ProdDeployStartedAt.IsZero() && (t.IsZero() || c.ProdDeployStartedAt.Before(t)) {
			t = c.ProdDeployStartedAt
		}
	}
	return t
}
