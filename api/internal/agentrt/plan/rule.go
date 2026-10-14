// Package plan holds the program-owned parts of the plan: the closed grammar
// of a step's done rule and the counting rules for items and rounds (design
// v5 §5.1.2). Nothing here is written by an LLM; the store calls it.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Tri is a three-valued truth: a fact that cannot be read is Unknown, and
// Unknown never becomes done.
type Tri int

const (
	False Tri = iota
	True
	Unknown
)

func (t Tri) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	}
	return "unknown"
}

// Facts answers the atoms of one step's rule. The store builds it from events
// and records bound to that step; GatePassed reads what is deployed now.
type Facts interface {
	TaskAccepted(taskID string) bool
	ApprovalExecuted(number int64) bool
	QuestionAnswered(question, option string) bool
	CISucceeded(repo, commit string) bool
	OwnerSignoff(ref string) bool
	ManualRecorded(note string) bool
	GatePassed(env, test, specSHA256 string) Tri
}

// Kind of reference a rule makes; the store checks each is bound to the step.
const (
	RefTask     = "task"
	RefApproval = "approval"
	RefQuestion = "question"
	RefSignoff  = "owner_signoff"
)

// Ref is an object a rule refers to.
type Ref struct {
	Kind string
	ID   string
}

// Rule is a parsed done rule. The zero Rule (from "{}") has no condition and
// is never satisfied by evaluation; such a step changes state only by event.
type Rule struct {
	node *node
}

// Result of evaluating a rule.
type Result struct {
	Value Tri
	// Manual is true when the rule holds only through a manual atom; Progress
	// counts and labels such steps separately.
	Manual bool
}

var (
	ErrRule  = errors.New("plan: invalid done rule")
	sha40    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha64    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	taskIDRe = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)
	testIDRe = regexp.MustCompile(`^T[0-9]{2}[a-z]?$`)
	envs     = map[string]bool{"dev": true, "stg": true, "prod": true}
)

type nodeKind int

const (
	kAll nodeKind = iota
	kAny
	kTask
	kApproval
	kQuestion
	kCI
	kSignoff
	kManual
	kGates
)

type gate struct{ test, spec string }

type node struct {
	kind     nodeKind
	children []*node
	s1, s2   string
	n        int64
	gates    []gate
}

// Standing reports whether the rule contains a standing condition, which is
// re-evaluated against each new deployment.
func (r Rule) Standing() bool { return r.node != nil && r.node.standing() }

// Empty reports whether the rule has no condition.
func (r Rule) Empty() bool { return r.node == nil }

func (n *node) standing() bool {
	if n.kind == kGates {
		return true
	}
	for _, c := range n.children {
		if c.standing() {
			return true
		}
	}
	return false
}

// Parse reads a done rule. The grammar is closed: an unknown key, an extra key
// or a malformed value is an error, so a rule the program does not understand
// is refused when written rather than judged later.
func Parse(doc []byte) (Rule, error) {
	v, err := canon.Parse(doc)
	if err != nil {
		return Rule{}, fmt.Errorf("%w: %v", ErrRule, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return Rule{}, fmt.Errorf("%w: not an object", ErrRule)
	}
	if len(obj) == 0 {
		return Rule{}, nil
	}
	n, err := parseNode(obj, 0)
	if err != nil {
		return Rule{}, err
	}
	return Rule{node: n}, nil
}

const maxDepth = 8

func parseNode(obj map[string]any, depth int) (*node, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: nested deeper than %d", ErrRule, maxDepth)
	}
	if g, ok := obj["gates_passed"]; ok {
		return parseGates(obj, g)
	}
	if len(obj) != 1 {
		return nil, fmt.Errorf("%w: an atom has exactly one key, got %d", ErrRule, len(obj))
	}
	for k, v := range obj {
		switch k {
		case "all", "any":
			arr, ok := v.([]any)
			if !ok || len(arr) == 0 {
				return nil, fmt.Errorf("%w: %s needs a non-empty list", ErrRule, k)
			}
			n := &node{kind: kAll}
			if k == "any" {
				n.kind = kAny
			}
			for _, e := range arr {
				eo, ok := e.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: %s holds objects only", ErrRule, k)
				}
				c, err := parseNode(eo, depth+1)
				if err != nil {
					return nil, err
				}
				n.children = append(n.children, c)
			}
			return n, nil
		case "task_accepted":
			s, ok := v.(string)
			if !ok || !taskIDRe.MatchString(s) {
				return nil, fmt.Errorf("%w: task_accepted needs a task id", ErrRule)
			}
			return &node{kind: kTask, s1: s}, nil
		case "approval_executed":
			num, ok := v.(json.Number)
			if !ok {
				return nil, fmt.Errorf("%w: approval_executed needs a number", ErrRule)
			}
			i, err := num.Int64()
			if err != nil || i <= 0 {
				return nil, fmt.Errorf("%w: approval_executed needs a positive integer", ErrRule)
			}
			return &node{kind: kApproval, n: i}, nil
		case "question_answered":
			q, ok := v.(map[string]any)
			if !ok || len(q) != 2 {
				return nil, fmt.Errorf("%w: question_answered needs question and option", ErrRule)
			}
			qs, ok1 := q["question"].(string)
			opt, ok2 := q["option"].(string)
			if !ok1 || !ok2 || qs == "" || opt == "" {
				return nil, fmt.Errorf("%w: question_answered needs question and option", ErrRule)
			}
			return &node{kind: kQuestion, s1: qs, s2: opt}, nil
		case "ci_succeeded":
			c, ok := v.(map[string]any)
			if !ok || len(c) != 2 {
				return nil, fmt.Errorf("%w: ci_succeeded needs repo and commit", ErrRule)
			}
			repo, ok1 := c["repo"].(string)
			commit, ok2 := c["commit"].(string)
			if !ok1 || !ok2 || repo == "" || !sha40.MatchString(commit) {
				return nil, fmt.Errorf("%w: ci_succeeded needs repo and a 40-character commit", ErrRule)
			}
			return &node{kind: kCI, s1: repo, s2: commit}, nil
		case "owner_signoff":
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("%w: owner_signoff needs a reference", ErrRule)
			}
			return &node{kind: kSignoff, s1: s}, nil
		case "manual":
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("%w: manual needs a description", ErrRule)
			}
			return &node{kind: kManual, s1: s}, nil
		}
		return nil, fmt.Errorf("%w: unknown atom %q", ErrRule, k)
	}
	return nil, fmt.Errorf("%w: empty atom", ErrRule)
}

func parseGates(obj map[string]any, g any) (*node, error) {
	if len(obj) != 2 {
		return nil, fmt.Errorf("%w: gates_passed takes exactly gates_passed and env", ErrRule)
	}
	env, ok := obj["env"].(string)
	if !ok || !envs[env] {
		return nil, fmt.Errorf("%w: gates_passed needs env dev, stg or prod", ErrRule)
	}
	arr, ok := g.([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("%w: gates_passed needs a non-empty list", ErrRule)
	}
	n := &node{kind: kGates, s1: env}
	seen := map[string]bool{}
	for _, e := range arr {
		eo, ok := e.(map[string]any)
		if !ok || len(eo) != 2 {
			return nil, fmt.Errorf("%w: each gate has test and spec_sha256", ErrRule)
		}
		test, ok1 := eo["test"].(string)
		spec, ok2 := eo["spec_sha256"].(string)
		if !ok1 || !ok2 || !testIDRe.MatchString(test) || !sha64.MatchString(spec) {
			return nil, fmt.Errorf("%w: each gate has a test id and a 64-character spec_sha256", ErrRule)
		}
		if seen[test] {
			return nil, fmt.Errorf("%w: gate %s listed twice", ErrRule, test)
		}
		seen[test] = true
		n.gates = append(n.gates, gate{test: test, spec: spec})
	}
	return n, nil
}

// Refs lists the objects the rule refers to, sorted, so the store can check
// that each is bound to the step before the rule is accepted.
func (r Rule) Refs() []Ref {
	var out []Ref
	var walk func(*node)
	walk = func(n *node) {
		switch n.kind {
		case kTask:
			out = append(out, Ref{RefTask, n.s1})
		case kApproval:
			out = append(out, Ref{RefApproval, fmt.Sprint(n.n)})
		case kQuestion:
			out = append(out, Ref{RefQuestion, n.s1})
		case kSignoff:
			out = append(out, Ref{RefSignoff, n.s1})
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if r.node != nil {
		walk(r.node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Eval evaluates the rule with Kleene logic: all is false if any child is
// false, else unknown if any is unknown; any is true if any child is true,
// else unknown if any is unknown. An empty rule is never true.
func (r Rule) Eval(f Facts) Result {
	if r.node == nil {
		return Result{Value: False}
	}
	return eval(r.node, f)
}

func eval(n *node, f Facts) Result {
	switch n.kind {
	case kAll:
		res := Result{Value: True}
		for _, c := range n.children {
			cr := eval(c, f)
			switch cr.Value {
			case False:
				return Result{Value: False}
			case Unknown:
				res.Value = Unknown
			}
			res.Manual = res.Manual || cr.Manual
		}
		if res.Value != True {
			res.Manual = false
		}
		return res
	case kAny:
		res := Result{Value: False}
		manualOnly := true
		for _, c := range n.children {
			cr := eval(c, f)
			switch cr.Value {
			case True:
				res.Value = True
				if !cr.Manual {
					manualOnly = false
				}
			case Unknown:
				if res.Value != True {
					res.Value = Unknown
				}
			}
		}
		res.Manual = res.Value == True && manualOnly
		return res
	case kTask:
		return tri(f.TaskAccepted(n.s1), false)
	case kApproval:
		return tri(f.ApprovalExecuted(n.n), false)
	case kQuestion:
		return tri(f.QuestionAnswered(n.s1, n.s2), false)
	case kCI:
		return tri(f.CISucceeded(n.s1, n.s2), false)
	case kSignoff:
		return tri(f.OwnerSignoff(n.s1), false)
	case kManual:
		return tri(f.ManualRecorded(n.s1), true)
	case kGates:
		out := True
		for _, g := range n.gates {
			switch f.GatePassed(n.s1, g.test, g.spec) {
			case False:
				return Result{Value: False}
			case Unknown:
				out = Unknown
			}
		}
		return Result{Value: out}
	}
	return Result{Value: Unknown}
}

func tri(ok, manual bool) Result {
	if ok {
		return Result{Value: True, Manual: manual}
	}
	return Result{Value: False}
}
