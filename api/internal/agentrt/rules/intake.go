package rules

import (
	"errors"
	"fmt"
	"slices"
)

// The intake gates (bifrost-trade-infra agent-config/work/crew/
// DESIGN-crew-intake-2026-10-22.md §4.3, §7.2; the Owner chose I-D1 and I-D4
// A on 2026-10-23): which items of the take predicate an issue of each risk
// class must pass before take_issue makes its mission, how many issues of one
// product may have live work at once, and which of the two standing
// exceptions are open. The platform computes the items; what each class means
// to the business stays in the products' profiles (§4.5). Without this rule
// in force and readable nothing is taken (item 8).
//
//	rule_id: intake-gates
//	classes:
//	  small:  [brief_confirmed, classified, owner_priority, quota, rule_in_force]
//	  medium: [brief_confirmed, classified, owner_priority, design, review, quota, rule_in_force]
//	  large:  [brief_confirmed, classified, owner_priority, design, review, owner_decisions, quota, rule_in_force]
//	max_live_per_product: 1
//	exceptions: [answer_only, fault_small]
const KindIntake = "intake-gates"

// The items of the take predicate (§4.3), in its order.
const (
	IntakeBriefConfirmed = "brief_confirmed" // 1: the current brief is confirmed by the person asked
	IntakeClassified     = "classified"      // 2: the newest classification is of the current brief
	IntakeOwnerPriority  = "owner_priority"  // 3: the Owner himself set a priority for the current brief
	IntakeDesign         = "design"          // 4: there is a design
	IntakeReview         = "review"          // 5: an independent review of the current brief and design passed
	IntakeOwnerDecisions = "owner_decisions" // 6: the Owner chose to go on at each point the design lists
	IntakeQuota          = "quota"           // 7: the product's live work is below max_live_per_product
	IntakeRuleInForce    = "rule_in_force"   // 8: this rule is in force and reads
)

// IntakeItems are the items in the take predicate's order.
var IntakeItems = []string{IntakeBriefConfirmed, IntakeClassified, IntakeOwnerPriority, IntakeDesign, IntakeReview,
	IntakeOwnerDecisions, IntakeQuota, IntakeRuleInForce}

// intakeAlways are the items every class lists: the standing exceptions skip
// only item 3, "still 1, 2, 7, 8" (§4.3), so no class may drop these.
var intakeAlways = []string{IntakeBriefConfirmed, IntakeClassified, IntakeQuota, IntakeRuleInForce}

// The standing exceptions (§4.3, §7.4 ③): an issue only answered is closed
// already_possible without the Owner's priority; a small fault in what she
// uses every day is taken without it, its mission inside the dev standing
// envelope.
const (
	IntakeAnswerOnly = "answer_only"
	IntakeFaultSmall = "fault_small"
)

var intakeExceptions = []string{IntakeAnswerOnly, IntakeFaultSmall}

// IntakeClasses are the risk classes, lowest first.
var IntakeClasses = []string{"small", "medium", "large"}

// MaxLivePerProduct bounds max_live_per_product, whatever the Owner signs.
const MaxLivePerProduct = 20

// IntakeGates is an intake-gates rule's part.
type IntakeGates struct {
	Classes           map[string][]string // class -> items, in the predicate's order
	MaxLivePerProduct int
	Exceptions        []string // sorted
}

// Requires says whether an issue of class must pass item.
func (g *IntakeGates) Requires(class, item string) bool {
	return slices.Contains(g.Classes[class], item)
}

// Allows says whether a standing exception is open.
func (g *IntakeGates) Allows(exception string) bool { return slices.Contains(g.Exceptions, exception) }

// intake reads the classes, each listing items of the predicate once and every
// item a lower class lists (a larger risk never passes fewer gates), the
// bound and the exceptions.
func (r *Rule) intake(o *object) error {
	g := &IntakeGates{Classes: map[string][]string{}}
	cls, err := o.object("classes")
	if err != nil {
		return err
	}
	var lower []string
	for _, c := range IntakeClasses {
		items, err := cls.strings(c, false)
		if err != nil {
			return err
		}
		for _, it := range items {
			if !slices.Contains(IntakeItems, it) {
				return fmt.Errorf("%s.%s: %q is none of %v", cls.where, c, it, IntakeItems)
			}
		}
		for _, it := range intakeAlways {
			if !slices.Contains(items, it) {
				return fmt.Errorf("%s.%s lists %s: every class passes items 1, 2, 7 and 8 (%v)", cls.where, c, it, intakeAlways)
			}
		}
		for _, it := range lower {
			if !slices.Contains(items, it) {
				return fmt.Errorf("%s.%s lists %s, as the classes below it do: a larger risk passes no fewer gates", cls.where, c, it)
			}
		}
		g.Classes[c] = inOrder(items)
		lower = g.Classes[c]
	}
	if err := cls.done(); err != nil {
		return err
	}
	if g.MaxLivePerProduct, err = o.integer("max_live_per_product", 0, MaxLivePerProduct); err != nil {
		return err
	}
	if g.Exceptions, err = o.strings("exceptions", false); err != nil {
		return err
	}
	for _, e := range g.Exceptions {
		if !slices.Contains(intakeExceptions, e) {
			return fmt.Errorf("%s.exceptions: %q is none of %v", o.where, e, intakeExceptions)
		}
	}
	if g.Exceptions == nil {
		g.Exceptions = []string{}
	}
	slices.Sort(g.Exceptions)
	r.Intake = g
	return nil
}

// inOrder is items in the predicate's order.
func inOrder(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range IntakeItems {
		if slices.Contains(items, it) {
			out = append(out, it)
		}
	}
	return out
}

func (g *IntakeGates) doc() map[string]any {
	classes := map[string]any{}
	for c, items := range g.Classes {
		classes[c] = anyList(items)
	}
	return map[string]any{"classes": classes, "max_live_per_product": g.MaxLivePerProduct, "exceptions": anyList(g.Exceptions)}
}

// intakeTightens: each class passes every gate it did, the bound only comes
// down, an exception is only closed (§4.6).
func intakeTightens(p, n *IntakeGates) error {
	for _, c := range IntakeClasses {
		for _, it := range p.Classes[c] {
			if !slices.Contains(n.Classes[c], it) {
				return fmt.Errorf("the %s class no longer passes %s", c, it)
			}
		}
	}
	if n.MaxLivePerProduct > p.MaxLivePerProduct {
		return errors.New("max_live_per_product goes up")
	}
	return subset(n.Exceptions, p.Exceptions, "exceptions")
}
