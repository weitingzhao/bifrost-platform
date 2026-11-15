package rules

import (
	"errors"
	"fmt"
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
	case KindLanding:
		return landingTightens(prev.Landing, next.Landing)
	case KindEnvelope:
		return envelopeTightens(prev.Envelope, next.Envelope)
	case KindBudget:
		p, n := prev.Budget, next.Budget
		if err := budgetTightens(p.Mission, n.Mission); err != nil {
			return err
		}
		if n.AttemptLimitSec > p.AttemptLimitSec {
			return errors.New("attempt_hard_limit_s goes up")
		}
		// What the platform may add on its own is budget too; a rule that
		// states none has the default cap.
		if n.AutoGrant.MaxAttempts > p.AutoGrant.MaxAttempts || n.AutoGrant.MaxWallHours > p.AutoGrant.MaxWallHours {
			return errors.New("auto_grant goes up")
		}
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
	if err := subset(n.Hosts, p.Hosts, "hosts"); err != nil {
		return err
	}
	return externalHostsTighten(p.ExternalHosts, n.ExternalHosts)
}
