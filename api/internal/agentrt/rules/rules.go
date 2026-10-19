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
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Namespace is the runtime's own signature namespace: a release-policy
// signature (releasepolicy.NamespacePolicy, NamespaceUnfreeze) is no rule
// signature.
const Namespace = "bifrost-agentrt-rule"

// The kinds of rule, by the rule_id's first part.
const (
	KindEnvelope    = "standing-envelope"
	KindConcurrency = "concurrency"
	KindBudget      = "budget"
	KindJob         = "job"
	KindFacts       = "capability-facts"
	KindRouting     = "routing"
	KindPlan        = "plan-standing"
)

// Directions as the rule table has them.
const (
	Initial = "initial"
	Loosen  = "loosen"
	Tighten = "tighten"
)

// Bounds a rule may not pass, whatever the Owner signs.
const (
	MaxMissionAttempts  = 1000
	MaxMissionWallHours = 720
	MaxAttemptLimitS    = 86400
	// MaxPerHost bounds concurrency: at most three live attempts a host
	// (W-31 2.4 slice ③); how many run is the signed per_host.
	MaxPerHost = 3
	maxJobText = 16 << 10
)

var (
	tenants    = []string{"dev", "business"}
	actions    = []string{"none", "branch"} // weakest first; C-class actions are never in an envelope
	statuses   = []string{"retired", "demoted", "qualifying", "probation", "active"}
	factKinds  = []string{"cannot", "must_not", "only", "prefer"}
	routeOrder = []string{"capability", "quota"}
	jobID      = regexp.MustCompile(`^[a-z][a-z_]{1,39}$`)
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

// Budget is a mission's budget.
type Budget struct {
	MaxAttempts  int `json:"max_attempts"`
	MaxWallHours int `json:"max_wall_hours"`
}

// Envelope is a standing authorization: a mission inside it starts without
// the Owner.
type Envelope struct {
	Tenant    string
	MaxAction string
	Budget    Budget
	Executors []string
	Hosts     []string
}

// BudgetRule is a tenant's default mission budget and an attempt's hard time
// limit.
type BudgetRule struct {
	Tenant          string
	Mission         Budget
	AttemptLimitSec int
}

// Candidate is one executor and model a job may route to.
type Candidate struct {
	Executor, Provider, ModelPattern, Status string
}

// Job is a job's brief and its candidates.
type Job struct {
	JobID, Tenant, Text string
	Candidates          []Candidate
}

// Fact is a capability fact the Owner states. A scope value is a string or
// true / false.
type Fact struct {
	Executor, Provider, Kind, Subject string
	Scope                             map[string]any
}

// Routing is the order routing decides in, how full a vendor pool may be
// before it takes no more work (a percentage of its window) and, when the
// rule sets one, the competence threshold.
type Routing struct {
	Tenant           string
	Order            []string
	QuotaStopPercent int
	Competence       *Competence
}

// Competence is routing's step ⑤ (design v5 §5.1.2): a candidate whose
// in-job score over at least MinSamples attempts of the task's kind is below
// MinScorePercent is not routed to, and one on probation with fewer takes no
// core work. A rule without it routes as before.
type Competence struct {
	MinScorePercent, MinSamples int
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

// Rule is a parsed rule: its header and the one part its kind has.
type Rule struct {
	Header
	Kind     string
	Envelope *Envelope
	PerHost  *int
	Budget   *BudgetRule
	Job      *Job
	Facts    []Fact
	Routing  *Routing
	Plan     *PlanStanding
}

// KindOf is the kind a rule_id names, and the tenant or job it is for.
func KindOf(id string) (kind, arg string, err error) {
	kind, arg, _ = strings.Cut(id, ":")
	switch kind {
	case KindEnvelope, KindBudget, KindRouting:
		if slices.Contains(tenants, arg) {
			return kind, arg, nil
		}
	case KindJob:
		if jobID.MatchString(arg) {
			return kind, arg, nil
		}
	case KindConcurrency, KindFacts, KindPlan:
		if arg == "" && !strings.Contains(id, ":") {
			return kind, "", nil
		}
	}
	return "", "", fmt.Errorf("rule_id %q is none of standing-envelope:<tenant>, concurrency, budget:<tenant>, job:<job>, capability-facts, routing:<tenant>, plan-standing", id)
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
	kind, arg, err := KindOf(r.ID)
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
	case KindEnvelope:
		err = r.envelope(o, arg)
	case KindConcurrency:
		var n int
		if n, err = o.integer("per_host", 0, MaxPerHost); err == nil {
			r.PerHost = &n
		}
	case KindBudget:
		err = r.budget(o, arg)
	case KindJob:
		err = r.job(o, arg)
	case KindFacts:
		err = r.facts(o)
	case KindRouting:
		err = r.routing(o, arg)
	case KindPlan:
		err = r.plan(o)
	}
	if err != nil {
		return Rule{}, err
	}
	return r, o.done()
}

func tenant(o *object, want string) (string, error) {
	t, err := o.str("tenant", false)
	if err == nil && t != want {
		err = fmt.Errorf("tenant is %q, the rule_id's", want)
	}
	return t, err
}

func budgetOf(o *object, key string) (Budget, error) {
	b, err := o.object(key)
	if err != nil {
		return Budget{}, err
	}
	var out Budget
	if out.MaxAttempts, err = b.integer("max_attempts", 1, MaxMissionAttempts); err != nil {
		return Budget{}, err
	}
	if out.MaxWallHours, err = b.integer("max_wall_hours", 1, MaxMissionWallHours); err != nil {
		return Budget{}, err
	}
	return out, b.done()
}

func (r *Rule) envelope(o *object, arg string) error {
	e := &Envelope{}
	var err error
	if e.Tenant, err = tenant(o, arg); err != nil {
		return err
	}
	if e.MaxAction, err = o.str("max_action", false); err != nil {
		return err
	}
	if !slices.Contains(actions, e.MaxAction) {
		return fmt.Errorf("max_action is none or branch, not %q: anything more is never standing", e.MaxAction)
	}
	if e.Budget, err = budgetOf(o, "budget"); err != nil {
		return err
	}
	if e.Executors, err = o.strings("executors", false); err != nil {
		return err
	}
	if e.Hosts, err = o.strings("hosts", false); err != nil {
		return err
	}
	slices.Sort(e.Executors)
	slices.Sort(e.Hosts)
	r.Envelope = e
	return nil
}

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

func (r *Rule) budget(o *object, arg string) error {
	b := &BudgetRule{}
	var err error
	if b.Tenant, err = tenant(o, arg); err != nil {
		return err
	}
	if b.Mission, err = budgetOf(o, "mission"); err != nil {
		return err
	}
	if b.AttemptLimitSec, err = o.integer("attempt_hard_limit_s", 60, MaxAttemptLimitS); err != nil {
		return err
	}
	r.Budget = b
	return nil
}

func (r *Rule) job(o *object, arg string) error {
	j := &Job{JobID: arg}
	var err error
	if j.Tenant, err = o.str("tenant", false); err != nil {
		return err
	}
	if !slices.Contains(tenants, j.Tenant) {
		return fmt.Errorf("tenant is dev or business, not %q", j.Tenant)
	}
	if j.Text, err = o.str("text", false); err != nil {
		return err
	}
	if strings.TrimSpace(j.Text) == "" || len(j.Text) > maxJobText {
		return fmt.Errorf("text is the brief, not empty and at most %d bytes", maxJobText)
	}
	seen := map[string]bool{}
	err = o.list("candidates", false, func(n *yaml.Node, where string) error {
		c, cerr := mapping(n, where)
		if cerr != nil {
			return cerr
		}
		var cand Candidate
		if cand.Executor, cerr = c.str("executor", false); cerr != nil {
			return cerr
		}
		if cand.Provider, cerr = c.str("provider", false); cerr != nil {
			return cerr
		}
		if cand.ModelPattern, cerr = c.str("model_pattern", false); cerr != nil {
			return cerr
		}
		if cand.Status, cerr = c.str("status", false); cerr != nil {
			return cerr
		}
		if cand.Executor == "" || cand.Provider == "" || cand.ModelPattern == "" || !slices.Contains(statuses, cand.Status) {
			return fmt.Errorf("%s: executor, provider and model_pattern are not empty; status is one of %v", where, statuses)
		}
		if seen[cand.key()] {
			return fmt.Errorf("%s: this candidate is listed twice", where)
		}
		seen[cand.key()] = true
		j.Candidates = append(j.Candidates, cand)
		return c.done()
	})
	if err != nil {
		return err
	}
	slices.SortFunc(j.Candidates, func(a, b Candidate) int { return strings.Compare(a.key(), b.key()) })
	r.Job = j
	return nil
}

func (c Candidate) key() string { return c.Executor + "|" + c.Provider + "|" + c.ModelPattern }

func (r *Rule) facts(o *object) error {
	seen := map[string]bool{}
	r.Facts = []Fact{}
	err := o.list("facts", false, func(n *yaml.Node, where string) error {
		f, err := mapping(n, where)
		if err != nil {
			return err
		}
		var fact Fact
		if fact.Executor, err = f.str("executor", false); err != nil {
			return err
		}
		if fact.Provider, err = f.str("provider", true); err != nil {
			return err
		}
		if fact.Kind, err = f.str("kind", false); err != nil {
			return err
		}
		if fact.Subject, err = f.str("subject", false); err != nil {
			return err
		}
		if fact.Executor == "" || fact.Subject == "" || !slices.Contains(factKinds, fact.Kind) {
			return fmt.Errorf("%s: executor and subject are not empty; kind is one of %v", where, factKinds)
		}
		if scope, err := f.node("scope", true); err != nil {
			return err
		} else if scope != nil {
			s, err := mapping(scope, where+".scope")
			if err != nil {
				return err
			}
			fact.Scope = map[string]any{}
			for _, k := range s.order {
				if fact.Scope[k], err = s.flag(k); err != nil {
					return err
				}
			}
		}
		if seen[fact.key()] {
			return fmt.Errorf("%s: this fact is listed twice", where)
		}
		seen[fact.key()] = true
		r.Facts = append(r.Facts, fact)
		return f.done()
	})
	slices.SortFunc(r.Facts, func(a, b Fact) int { return strings.Compare(a.key(), b.key()) })
	return err
}

// key is a fact's identity: who, under which provider, what kind, about what.
func (f Fact) key() string { return f.Executor + "|" + f.Provider + "|" + f.Kind + "|" + f.Subject }

func (r *Rule) routing(o *object, arg string) error {
	rt := &Routing{}
	var err error
	if rt.Tenant, err = tenant(o, arg); err != nil {
		return err
	}
	if rt.Order, err = o.strings("order", false); err != nil {
		return err
	}
	if !slices.Equal(rt.Order, routeOrder) {
		return fmt.Errorf("order is %v: capability first, quota second (Owner 2026-10-13)", routeOrder)
	}
	if rt.QuotaStopPercent, err = o.integer("quota_stop_percent", 1, 100); err != nil {
		return err
	}
	_, score := o.fields["min_score_percent"]
	_, samples := o.fields["min_samples"]
	switch {
	case score && samples:
		c := &Competence{}
		if c.MinScorePercent, err = o.integer("min_score_percent", 0, 100); err != nil {
			return err
		}
		if c.MinSamples, err = o.integer("min_samples", 1, 10000); err != nil {
			return err
		}
		rt.Competence = c
	case score || samples:
		return fmt.Errorf("%s: min_score_percent and min_samples go together", o.where)
	}
	r.Routing = rt
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
	case KindEnvelope:
		e := r.Envelope
		doc["tenant"], doc["max_action"], doc["executors"], doc["hosts"] = e.Tenant, e.MaxAction, anyList(e.Executors), anyList(e.Hosts)
		doc["budget"] = map[string]any{"max_attempts": e.Budget.MaxAttempts, "max_wall_hours": e.Budget.MaxWallHours}
	case KindConcurrency:
		doc["per_host"] = *r.PerHost
	case KindBudget:
		b := r.Budget
		doc["tenant"], doc["attempt_hard_limit_s"] = b.Tenant, b.AttemptLimitSec
		doc["mission"] = map[string]any{"max_attempts": b.Mission.MaxAttempts, "max_wall_hours": b.Mission.MaxWallHours}
	case KindJob:
		cands := make([]any, 0, len(r.Job.Candidates))
		for _, c := range r.Job.Candidates {
			cands = append(cands, map[string]any{"executor": c.Executor, "provider": c.Provider, "model_pattern": c.ModelPattern, "status": c.Status})
		}
		doc["tenant"], doc["text"], doc["candidates"] = r.Job.Tenant, r.Job.Text, cands
	case KindFacts:
		facts := make([]any, 0, len(r.Facts))
		for _, f := range r.Facts {
			fd := map[string]any{"executor": f.Executor, "kind": f.Kind, "subject": f.Subject}
			if f.Provider != "" {
				fd["provider"] = f.Provider
			}
			if f.Scope != nil {
				scope := map[string]any{}
				for k, v := range f.Scope {
					scope[k] = v
				}
				fd["scope"] = scope
			}
			facts = append(facts, fd)
		}
		doc["facts"] = facts
	case KindPlan:
		doc["done_rules"], doc["active_on_lease"] = r.Plan.DoneRules, r.Plan.ActiveOnLease
	case KindRouting:
		doc["tenant"], doc["order"], doc["quota_stop_percent"] = r.Routing.Tenant, anyList(r.Routing.Order), r.Routing.QuotaStopPercent
		if c := r.Routing.Competence; c != nil {
			doc["min_score_percent"], doc["min_samples"] = c.MinScorePercent, c.MinSamples
		}
	}
	return doc
}

// BodyJSON is the canonical JSON of the rule: what body_json must equal.
func (r Rule) BodyJSON() ([]byte, error) { return canon.Encode(r.Doc()) }
