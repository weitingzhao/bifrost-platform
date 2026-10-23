package rules

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Tightens says whether next only tightens prev (design v5 §5.2; proposal
// §2): an unsigned rule takes effect only then. It never trusts the
// direction next declares; it compares the two. nil means next grants
// nothing prev did not: limits only come down, lists only shrink, a
// candidate only drops in status, facts only gain cannot and must_not, and
// the validity only shortens. Anything it cannot show is loosening.
func Tightens(prev, next Rule) error {
	switch {
	case prev.ID != next.ID || prev.Kind != next.Kind:
		return errors.New("a different rule")
	case next.Expires.After(prev.Expires):
		return errors.New("it expires later")
	case next.Effective.Before(prev.Effective):
		return errors.New("it takes effect earlier")
	}
	switch next.Kind {
	case KindEnvelope:
		return envelopeTightens(prev.Envelope, next.Envelope)
	case KindConcurrency:
		if *next.PerHost > *prev.PerHost {
			return errors.New("per_host goes up")
		}
	case KindBudget:
		p, n := prev.Budget, next.Budget
		if err := budgetTightens(p.Mission, n.Mission); err != nil {
			return err
		}
		if n.AttemptLimitSec > p.AttemptLimitSec {
			return errors.New("attempt_hard_limit_s goes up")
		}
	case KindJob:
		return jobTightens(prev.Job, next.Job)
	case KindFacts:
		return factsTighten(prev.Facts, next.Facts)
	case KindPlan:
		// Each grant only goes from true to false.
		if next.Plan.DoneRules && !prev.Plan.DoneRules || next.Plan.ActiveOnLease && !prev.Plan.ActiveOnLease {
			return errors.New("the plan's standing authorization grants more")
		}
	case KindCardAuto:
		// It waits no less long, answers no new kind of card, and keeps an
		// AI assessment to a veto once it has (a card's assessment choosing
		// the option again is more the rule does without the Owner).
		if next.CardAuto.AfterSeconds < prev.CardAuto.AfterSeconds {
			return errors.New("the wait goes down")
		}
		if prev.CardAuto.AssessmentVeto && !next.CardAuto.AssessmentVeto {
			return errors.New("an AI assessment may choose the option again")
		}
		return subset(next.CardAuto.Kinds, prev.CardAuto.Kinds, "kinds")
	case KindRouting:
		if next.Routing.QuotaStopPercent > prev.Routing.QuotaStopPercent {
			return errors.New("quota_stop_percent goes up")
		}
		// A threshold excludes some candidates and, by its sample count,
		// spares others: any change of it is the Owner's to sign.
		if p, n := prev.Routing.Competence, next.Routing.Competence; (p == nil) != (n == nil) || (p != nil && *p != *n) {
			return errors.New("the competence threshold changes")
		}
	case KindIntake:
		return intakeTightens(prev.Intake, next.Intake)
	default:
		// A kind with no comparison here cannot be shown to tighten: it
		// needs the Owner's signature, never an unsigned "tighten" (§4.6;
		// before, a kind not listed fell through to nil).
		return fmt.Errorf("a %s rule has no tightening comparison: it needs the Owner's signature", next.Kind)
	}
	return nil
}

func budgetTightens(p, n Budget) error {
	if n.MaxAttempts > p.MaxAttempts || n.MaxWallHours > p.MaxWallHours {
		return errors.New("the budget goes up")
	}
	return nil
}

func subset(n, p []string, what string) error {
	for _, v := range n {
		if !slices.Contains(p, v) {
			return fmt.Errorf("%s gain %s", what, v)
		}
	}
	return nil
}

func envelopeTightens(p, n *Envelope) error {
	if slices.Index(actions, n.MaxAction) > slices.Index(actions, p.MaxAction) {
		return errors.New("max_action goes up")
	}
	if err := budgetTightens(p.Budget, n.Budget); err != nil {
		return err
	}
	if err := subset(n.Executors, p.Executors, "executors"); err != nil {
		return err
	}
	return subset(n.Hosts, p.Hosts, "hosts")
}

// jobTightens: the brief unchanged, no new candidate, none higher in status.
// A candidate left out counts as retired.
func jobTightens(p, n *Job) error {
	if n.Tenant != p.Tenant {
		return errors.New("the tenant changes")
	}
	if n.Text != p.Text {
		return errors.New("the brief changes, which is not shown to be tighter")
	}
	before := map[string]string{}
	for _, c := range p.Candidates {
		before[c.key()] = c.Status
	}
	for _, c := range n.Candidates {
		was, ok := before[c.key()]
		if !ok {
			return fmt.Errorf("candidate %s is new", c.key())
		}
		if slices.Index(statuses, c.Status) > slices.Index(statuses, was) {
			return fmt.Errorf("candidate %s goes from %s up to %s", c.key(), was, c.Status)
		}
	}
	return nil
}

// factsTighten: every fact stays as it was; new ones only forbid.
func factsTighten(p, n []Fact) error {
	now := map[string]Fact{}
	for _, f := range n {
		now[f.key()] = f
	}
	for _, f := range p {
		g, ok := now[f.key()]
		if !ok {
			return fmt.Errorf("fact %s is dropped", f.key())
		}
		if !maps.Equal(f.Scope, g.Scope) {
			return fmt.Errorf("fact %s changes its scope", f.key())
		}
		delete(now, f.key())
	}
	for k, f := range now {
		if f.Kind != "cannot" && f.Kind != "must_not" {
			return fmt.Errorf("new fact %s is %s: only cannot and must_not tighten", k, f.Kind)
		}
	}
	return nil
}
