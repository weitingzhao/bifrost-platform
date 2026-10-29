package store

import (
	"fmt"
	"slices"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// StgLeaseObservation is a bounded read of the window, including its CAS
// identity. Generation is parsed by the lease owner from the window reason.
type StgLeaseObservation struct {
	Who, What, Env, ResourceVersion string
	Generation                      int64
	Expires                         time.Time
}

// StgGateObservation contains only trusted adapter reads, taken before any
// database lock. Commits is the COMPLETE base..P first-parent range. Workloads
// is ReadWorkloads' strict PodDigests result, never Deployed("stg").
type StgGateObservation struct {
	Main, BaseP string
	Commits     []landing.ReleaseCommit
	CI          StgCIObservation
	Workloads   map[string]any
	Lease       StgLeaseObservation
	ReadAt      time.Time
}

// StgCIObservation is the adapter's selection, including unsuccessful latest
// runs. Accepted contains ReleaseEvidence.Record() only on success. Keeping
// this DTO in store avoids a cycle with landingci's grant evidence reader.
type StgCIObservation struct {
	Status, UID, Reason string
	CreatedAt, ReadAt   time.Time
	Accepted            map[string]any
}

func (c StgCIObservation) Record() (map[string]any, error) {
	if c.Status != "Succeeded" || c.Reason != "" || !stgCI(c.Accepted) || StgRecord(c.Accepted).String("repo_url") != landing.LandingRepos["bifrost-platform"].Remote || c.UID != StgRecord(c.Accepted).String("uid") || !c.ReadAt.Equal(StgRecord(c.Accepted).Time("read_at")) {
		return nil, fmt.Errorf("release CI is not accepted evidence")
	}
	return c.Accepted, nil
}

// StgGateDecision describes a refusal without pretending it is a permanent
// disposition. Handoff asks B5-e to settle/reopen; Block names a durable
// interval. Only HardFloor members are eligible for permanent withholding.
type StgGateDecision struct {
	Allowed, Handoff     bool
	Why, Block, Evidence string
	Set, HardFloor       []StgMemberKey
	NotBefore            time.Time
}

type stgBinding struct {
	Root, Tenant, Step, Mission, Repo, StepState string
	NoMerge                                      bool
}
type stgLandingAuthority struct {
	RuleVersion int64
	UsedGrant   bool
	Grant       GrantEvidence
	HasGrant    bool
}
type stgGateFacts struct {
	Bindings    map[string]stgBinding
	Rules       map[string]*rules.Rule
	Authority   map[string]stgLandingAuthority
	Live        bool
	Environment string
	Generation  int64
}

func stgRuleEnabled(r *rules.Rule, at time.Time) bool {
	if r == nil || r.Kind != rules.KindLanding || r.Landing == nil || r.Version < 1 || at.Before(r.Effective) || !at.Before(r.Expires) {
		return false
	}
	repo, ok := r.Landing.Repo("bifrost-platform")
	return ok && repo.STGRelease == "bifrost-deliver-platform"
}

// StgTrustedBaseline selects by deployment observation time, not insertion
// order. Tied contradictory evidence is unknown; an equal digest on another
// run is never silently substituted for the frozen evidence identity.
func StgTrustedBaseline(s StgState) (StgRecord, bool) {
	var latest StgEvent
	ambiguous := false
	for id, e := range s.Evidence {
		check := s.deploymentObservation(id)
		deployed := check.Record.object("deployed")
		if deployed["pin_sync_ok"] != true || len(deployed.object("workloads")) == 0 {
			continue
		}
		at := deployed.Time("at")
		oldAt := s.deploymentObservation(latest.Record.String("evidence")).Record.object("deployed").Time("at")
		if latest.Seq == 0 || at.After(oldAt) {
			latest, ambiguous = e, false
		} else if at.Equal(oldAt) {
			if e.Record.String("P") != latest.Record.String("P") || !stgSame(e.Record["content"], latest.Record["content"]) {
				ambiguous = true
			}
			if e.Record.String("evidence") < latest.Record.String("evidence") {
				latest = e
			}
		}
	}
	if latest.Seq == 0 || ambiguous {
		return nil, false
	}
	r := latest.Record
	return StgRecord{"P": r["P"], "api_digest": r.object("content")["api_digest"], "console_digest": r.object("content")["console_digest"], "evidence": r["evidence"]}, true
}

func stgWorkloadsMatch(workloads map[string]any, base StgRecord) bool {
	if len(workloads) != 3 {
		return false
	}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		w, ok := workloads[name].(map[string]any)
		if !ok || StgRecord(w).String("uid") == "" || StgRecord(w).Int("generation") < 1 {
			return false
		}
		want := base.String("api_digest")
		if name == "platform-console" {
			want = base.String("console_digest")
		}
		if !stgDigest.MatchString(want) || w["digest"] != want {
			return false
		}
	}
	return true
}

func stgEvaluateGate(s StgState, batch string, obs StgGateObservation, facts stgGateFacts, at time.Time, resend bool) StgGateDecision {
	d := StgGateDecision{}
	refuse := func(why string) StgGateDecision { d.Why = why; return d }
	b := s.Batches[batch]
	if b == nil || b.Phase == "CLOSED" || b.Tries[b.Try] == nil {
		return refuse("not_ready")
	}
	t := b.Tries[b.Try]
	if !resend && (b.Phase != "SNAP" || t.Started.Seq > 0) || resend && (!t.Occupied() || t.Bound.Seq > 0 || t.IsQuarantined() || at.Before(t.Permit.Record.Time("deadline"))) {
		return refuse("not_ready")
	}
	if facts.Environment != "prod" || !facts.Live {
		return refuse("disabled")
	}
	// This is deliberately before Git/CI policy. none never grants a permit,
	// including an already-debited recovery, but does not gate observation.
	for _, binding := range facts.Bindings {
		if !stgRuleEnabled(facts.Rules[binding.Tenant], at) || facts.Rules[binding.Tenant].Landing.Tenant != binding.Tenant {
			return refuse("rule")
		}
	}
	if len(facts.Bindings) == 0 {
		return refuse("rule")
	}
	snap := t.Snapshot.Record
	base, p := snap.object("base"), snap.object("inputs").String("revision")
	if obs.ReadAt.IsZero() || obs.ReadAt.After(at) || at.Sub(obs.ReadAt) > 2*time.Minute {
		return refuse("observation_expired")
	}
	trusted, ok := StgTrustedBaseline(s)
	if !ok {
		return refuse("baseline_unknown")
	}
	if !stgSame(map[string]any(trusted), map[string]any(base)) {
		d.Handoff, d.Evidence = true, trusted.String("evidence")
		return refuse("baseline_moved")
	}
	if obs.BaseP != base.String("P") {
		return refuse("baseline_unknown")
	}
	if obs.Main != p {
		d.Handoff = true
		return refuse("inputs_changed")
	}
	parent := obs.BaseP
	for _, c := range obs.Commits {
		if len(c.Parents) != 1 {
			return refuse("non_linear")
		}
		if !landing.ValidSHA(c.SHA) || c.Parents[0] != parent {
			return refuse("baseline_moved")
		}
		parent = c.SHA
	}
	if parent != p {
		return refuse("baseline_moved")
	}
	// A failed preceding try may have partially applied this same immutable
	// input. It must be terminal and its recorded effect exact, not a caller
	// supplied "retry" boolean. The authorization base is never shortened.
	workloadsOK := stgWorkloadsMatch(obs.Workloads, base)
	if !workloadsOK && b.Try > 1 {
		prev := b.Tries[b.Try-1]
		workloadsOK = prev != nil && prev.RunEvidence.Seq > 0 && prev.Verify.Record.String("outcome") == "failed" && prev.Effect.Record.String("class") == "this_try" && stgSame(obs.Workloads, prev.Effect.Record["workloads"])
	}
	if !workloadsOK {
		return refuse("baseline_moved")
	}
	ci, err := obs.CI.Record()
	if err != nil {
		if obs.CI.Status == "Failed" {
			return refuse("ci_red")
		}
		if obs.CI.Reason == "" && (obs.CI.Status == "Running" || obs.CI.Status == "Missing" || obs.CI.Status == "Unknown") {
			return refuse("ci_waiting")
		}
		return refuse("ci_results")
	}
	in := snap.object("inputs")
	if ci["platform_commit"] != p || ci["ui_commit"] != in["uiRevision"] || ci["infra_commit"] != in["infraRevision"] {
		d.Handoff = true
		return refuse("wrong_input")
	}
	if obs.CI.ReadAt.After(at) {
		return refuse("observation_expired")
	}
	// Authorize the entire range before deriving its debit set. A disposed
	// original still needs authorization, as does its recorded reversal.
	authorized := map[string]int{}
	members := make([]StgMemberKey, 0, len(obs.Commits))
	for i, c := range obs.Commits {
		op := landinggit.LandedOperation(c.Message)
		o, ok := s.Legacy.Operations[op]
		if op == "" || !ok || !o.EverLanded || o.R != c.SHA || o.DryRun || !landinggit.HasTaskTrailer(c.Message, o.Task) {
			return refuse("diff_unlanded")
		}
		m := StgMemberKey{Root: o.Task, Op: op, R: c.SHA}
		binding, exists := facts.Bindings[o.Task]
		if !exists || binding.Repo != "bifrost-platform" {
			return refuse("binding_changed")
		}
		if binding.NoMerge || binding.Step != "" && !slices.Contains([]string{"planned", "active", "done"}, binding.StepState) {
			return refuse("no_merge_after_landing")
		}
		if len(c.Changes) == 0 {
			return refuse("rule")
		}
		if _, hard := rules.LandingAbsoluteViolation("bifrost-platform", c.Changes); hard {
			d.HardFloor = append(d.HardFloor, m)
			return refuse("hard_floor")
		}
		if _, structural := rules.StructuralViolation(c.Changes); structural {
			return refuse("rule")
		}
		a := facts.Authority[op]
		if a.UsedGrant {
			g := a.Grant
			if !a.HasGrant || !g.STGRelease || g.Purpose != "landing_main" || g.GrantID == "" || g.ApprovalID == "" || g.IntentSeq < 1 || g.R != c.SHA || g.Task != o.Task || g.Attempt != o.Attempt || g.A != o.A || g.M0 != o.M0 {
				return refuse("rule")
			}
		} else if a.RuleVersion < 1 || facts.Rules[binding.Tenant].Landing.CheckChanges("bifrost-platform", c.Changes) != nil {
			return refuse("rule")
		}
		authorized[c.SHA] = i
		members = append(members, m)
	}
	for i, m := range members {
		old := s.Members[m]
		if old.To == "disposed" {
			ref, _ := cardNumber(old.Ref)
			proof := s.Records[ref].Record
			reversal, inRange := authorized[proof.String("by")]
			if proof.String("kind") != "reverted" || proof.String("op") != m.Op || proof.String("R") != m.R || !inRange || reversal <= i {
				return refuse("diff_unlanded")
			}
			continue
		}
		if old.To != "delivered" && old.To != "covered" && !s.covered(m) {
			d.Set = append(d.Set, m)
		}
	}
	sortStgMembers(d.Set)
	for _, m := range d.Set {
		gen, member := b.Members[m]
		if !member {
			d.Handoff = true
			return refuse("set_changed")
		}
		used := s.Budget[StgBudgetKey{m.R, gen}]
		// A resend owns its previous debit, even when it was the third one.
		if resend {
			used--
		}
		if _, withheld := s.Withheld[m]; withheld || gen != s.generation(m.R) || used >= 3 {
			return refuse("budget_exhausted")
		}
		if old := s.Members[m]; old.To != "" {
			return refuse("budget_exhausted")
		}
		if s.Debits[m.R].NotBefore.After(d.NotBefore) {
			d.NotBefore = s.Debits[m.R].NotBefore
		}
	}
	set := map[StgMemberKey]int64{}
	for _, m := range d.Set {
		set[m] = 0
	}
	if !stgSameMembers(snap.members("set"), set) {
		d.Handoff = true
		return refuse("set_changed")
	}
	if snap.Time("not_before").After(d.NotBefore) {
		d.NotBefore = snap.Time("not_before")
	}
	if at.Before(d.NotBefore) {
		return refuse("backoff")
	}
	if err := s.checkReleaseBlocks(b, "started"); err != nil {
		return refuse("blocked")
	}
	w := obs.Lease
	who := fmt.Sprintf("stgrel-bifrost-platform-%d-%d", stgBatchNumber(batch), b.Try)
	if w.Who != who || w.What != "bifrost-platform" || w.Env != "stg" || w.ResourceVersion == "" || w.Generation < 1 || w.Generation != facts.Generation || !at.Before(w.Expires) {
		return refuse("window")
	}
	d.Allowed = true
	return d
}

func sortStgMembers(ms []StgMemberKey) {
	slices.SortFunc(ms, func(a, b StgMemberKey) int {
		if a.Root != b.Root {
			return cmpStg(a.Root, b.Root)
		}
		if a.Op != b.Op {
			return cmpStg(a.Op, b.Op)
		}
		return cmpStg(a.R, b.R)
	})
}
func cmpStg(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
