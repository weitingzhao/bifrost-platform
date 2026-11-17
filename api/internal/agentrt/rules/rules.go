// Package rules reads the runtime's rules (design v5 §5.2 「规则」; Owner
// 2026-10-14 「规则 A」, PROPOSAL-agentrt-rules-2026-10-14.md): YAML written
// like the release policy and signed with `ssh-keygen -Y sign -n
// bifrost-agentrt-rule`. What runs is what Parse reads from the signed text;
// the stored body_json is derived from it (Rule.BodyJSON). The parser is
// closed: a field it does not list, a value of another type, an anchor, an
// alias or a merge key is an error, and the caller keeps the strictest
// default.
package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Namespace is the runtime's own signature namespace: a release-policy
// signature (releasepolicy.NamespacePolicy, NamespaceUnfreeze) is no rule
// signature.
const Namespace = "bifrost-agentrt-rule"

// The kinds of rule, by the rule_id's first part.
const (
	KindPlan     = "plan-standing"
	KindCardAuto = "card-auto-answer"
)

// The kinds of card a card-auto-answer rule may list (store/cardauto.go):
// a blocked task's card (store/blockedcard.go), a planner's question
// (store/planresult.go), a plan change's, a plan import's and a recorder
// import's card (planproposal.go, planimportcard.go, recorderimport.go), and
// historical mission cards. TD-393
// (the Owner 2026-10-20 「STG 上所有种类的运行时卡都归规则管」) added all but the
// first two; a rule answers a card with its recommendation, or with the
// option that changes nothing when it has none or its AI assessment differs
// (before assessment: veto, a card with none took its assessment's option).
var cardKinds = []string{"blocked", "mission", "plan_change", "plan_import", "plan_question", "recorder_import"}

// Bounds of a card-auto-answer rule's wait: after_seconds from 0 to a week
// (a card expires before then; TD-393); after_minutes, the form of the
// first versions, from five minutes to a week.
const (
	MinCardWaitMinutes = 5
	MaxCardWaitMinutes = 7 * 24 * 60
	MaxCardWaitSeconds = 7 * 24 * 3600
)

// Directions as the rule table has them.
const (
	Initial = "initial"
	Loosen  = "loosen"
	Tighten = "tighten"
)

// Header is the five fields the signature covers, written at the top of
// every rule and compared with the row's columns.
type Header struct {
	ID        string
	Version   int
	Direction string
	Effective time.Time
	Expires   time.Time
}

// PlanStanding is what the plan's standing authorization adds to its two
// built-in cases (a step added to an item in progress, the title of a step
// not started; store/planchange.go): DoneRules lets a done rule be set on a
// step not yet done that has none, when the rule names no manual atom;
// ActiveOnLease lets the controller move a planned step to active once a
// task on it has been leased (CUT-plan-import-2026-10-19.md §7).
type PlanStanding struct {
	DoneRules, ActiveOnLease bool
}

// CardAuto lets the platform answer a card the runtime asked, once it has
// been open AfterSeconds unanswered, when the card is of one of Kinds and
// its mission does not need the Owner (TD-390, the Owner 2026-10-20 「STG 用
// A」: STG only; PROD has no such rule; TD-393: in seconds, every kind).
type CardAuto struct {
	AfterSeconds int
	Kinds        []string
	// AssessmentVeto: the rule says assessment: veto (review 489 A1 on T12,
	// the coordinating thread 2026-10-22 for the Owner: A1). A card's AI
	// assessment may then only keep the rule from the card's recommendation,
	// never give it an option of its own: a card that recommends nothing is
	// answered with the option that changes nothing, and left to the Owner
	// when it has none (store.ruleChoice). Without it (versions before),
	// such a card takes the option of an assessment that has come.
	AssessmentVeto bool
	// minutes: the rule says after_minutes (versions before TD-393), and
	// its canonical body says so again.
	minutes bool
}

// Rule is a parsed rule: its header and the one part its kind has.
type Rule struct {
	Header
	Kind     string
	Plan     *PlanStanding
	CardAuto *CardAuto
	// Tuning is a tuning rule's part (tuning.go).
	Tuning *Tuning
}

// KindOf is the kind a rule_id names, and the tenant or job it is for.
func KindOf(id string) (kind, arg string, err error) {
	kind, arg, _ = strings.Cut(id, ":")
	switch kind {
	case KindPlan, KindCardAuto, KindTuning:
		if arg == "" && !strings.Contains(id, ":") {
			return kind, "", nil
		}
	}
	return "", "", fmt.Errorf("rule_id %q is none of plan-standing, card-auto-answer, tuning", id)
}

// Parse reads a rule's text. Nothing it does not list passes.
func Parse(text []byte) (Rule, error) {
	o, err := document(text)
	if err != nil {
		return Rule{}, err
	}
	var r Rule
	if r.ID, err = o.str("rule_id", false); err != nil {
		return Rule{}, err
	}
	kind, _, err := KindOf(r.ID)
	if err != nil {
		return Rule{}, err
	}
	r.Kind = kind
	if r.Version, err = o.integer("rule_version", 1, 1<<30); err != nil {
		return Rule{}, err
	}
	if r.Direction, err = o.str("direction", false); err != nil {
		return Rule{}, err
	}
	switch {
	case r.Direction != Initial && r.Direction != Loosen && r.Direction != Tighten:
		return Rule{}, fmt.Errorf("direction is initial, loosen or tighten, not %q", r.Direction)
	case (r.Direction == Initial) != (r.Version == 1):
		return Rule{}, errors.New("version 1 is the initial rule, and only version 1")
	}
	if r.Effective, err = o.at("effective_at"); err != nil {
		return Rule{}, err
	}
	if r.Expires, err = o.at("expires_at"); err != nil {
		return Rule{}, err
	}
	if !r.Expires.After(r.Effective) {
		return Rule{}, errors.New("expires_at is after effective_at")
	}
	switch kind {
	case KindPlan:
		err = r.plan(o)
	case KindCardAuto:
		err = r.cardAuto(o)
	case KindTuning:
		err = r.tuning(o)
	}
	if err != nil {
		return Rule{}, err
	}
	return r, o.done()
}

// cardAuto reads a card-auto-answer rule: after_seconds (or, as the first
// versions say it, after_minutes; one of them), kinds, a list of the card
// kinds it answers, at least one, none twice (kept sorted), and, optional,
// assessment, which says veto or is absent.
func (r *Rule) cardAuto(o *object) error {
	c := &CardAuto{}
	var err error
	_, seconds := o.fields["after_seconds"]
	_, minutes := o.fields["after_minutes"]
	switch {
	case seconds && minutes:
		return fmt.Errorf("%s gives after_seconds or after_minutes, not both", o.where)
	case seconds:
		if c.AfterSeconds, err = o.integer("after_seconds", 0, MaxCardWaitSeconds); err != nil {
			return err
		}
	default:
		var m int
		if m, err = o.integer("after_minutes", MinCardWaitMinutes, MaxCardWaitMinutes); err != nil {
			return err
		}
		c.AfterSeconds, c.minutes = m*60, true
	}
	if c.Kinds, err = o.strings("kinds", false); err != nil {
		return err
	}
	if len(c.Kinds) == 0 {
		return fmt.Errorf("%s.kinds lists at least one of %v", o.where, cardKinds)
	}
	for _, k := range c.Kinds {
		if !slices.Contains(cardKinds, k) {
			return fmt.Errorf("%s.kinds: %q is none of %v", o.where, k, cardKinds)
		}
	}
	slices.Sort(c.Kinds)
	if _, given := o.fields["assessment"]; given {
		a, err := o.str("assessment", false)
		if err != nil {
			return err
		}
		if a != CardAssessmentVeto {
			return fmt.Errorf("%s.assessment says %q; the one value it takes is %q", o.where, a, CardAssessmentVeto)
		}
		c.AssessmentVeto = true
	}
	r.CardAuto = c
	return nil
}

// CardAssessmentVeto is the one value of a card-auto-answer rule's
// assessment (CardAuto.AssessmentVeto).
const CardAssessmentVeto = "veto"

func (r *Rule) plan(o *object) error {
	p := &PlanStanding{}
	for key, at := range map[string]*bool{"done_rules": &p.DoneRules, "active_on_lease": &p.ActiveOnLease} {
		v, err := o.flag(key)
		if err != nil {
			return err
		}
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("%s.%s is true or false", o.where, key)
		}
		*at = b
	}
	r.Plan = p
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func anyList(s []string) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}

// Doc is the rule as a JSON document, keys as the text has them.
func (r Rule) Doc() map[string]any {
	doc := map[string]any{"rule_id": r.ID, "rule_version": r.Version, "direction": r.Direction,
		"effective_at": stamp(r.Effective), "expires_at": stamp(r.Expires)}
	switch r.Kind {
	case KindPlan:
		doc["done_rules"], doc["active_on_lease"] = r.Plan.DoneRules, r.Plan.ActiveOnLease
	case KindCardAuto:
		if r.CardAuto.minutes {
			doc["after_minutes"] = r.CardAuto.AfterSeconds / 60
		} else {
			doc["after_seconds"] = r.CardAuto.AfterSeconds
		}
		doc["kinds"] = anyList(r.CardAuto.Kinds)
		if r.CardAuto.AssessmentVeto {
			doc["assessment"] = CardAssessmentVeto
		}
	case KindTuning:
		for k, v := range r.Tuning.doc() {
			doc[k] = v
		}
	}
	return doc
}

// BodyJSON is the canonical JSON of the rule: what body_json must equal.
func (r Rule) BodyJSON() ([]byte, error) { return canon.Encode(r.Doc()) }
