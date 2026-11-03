package plan

import "sort"

// Explaining a step that is not done (R5:5.5-19, DESIGN-r5-5.5-19-waiting-
// chain §3). Eval stops at the first child that settles a node; Explain
// reads every child, so it can name each thing that keeps the rule from
// true. It covers only rules built from all, any and the three aggregate
// atoms over the step's own work: those name no record in advance, so they
// are the ones a reader cannot follow by hand. Over that set its value is
// Eval's whole Result, which the tests check for every combination.

// Node is one node of an explained rule.
type Node struct {
	// Kind is "all", "any" or the atom's name (AtomStepTasks,
	// AtomStepLanded, AtomStepSTG).
	Kind string
	// State and Min are step_tasks' arguments; empty and 0 otherwise.
	State string
	Min   int64
	// Value is this node's value under Eval's Kleene logic.
	Value Tri
	// Path is the node's place in the rule: child indexes from the root,
	// so the same atom named twice has two places.
	Path     []int
	Children []Node
}

// Blocking is one thing that keeps a node from true: an atom that is not
// true, or, for an any that is not true, every branch with its own
// blocking (one branch true would be enough).
type Blocking struct {
	Atom  *Node
	OneOf [][]Blocking
}

// Explain reads every node of a rule Explain supports. ok is false for any
// other rule, with the atoms it names that Explain does not cover, sorted
// and once each; the zero rule names none.
func (r Rule) Explain(f Facts) (n Node, ok bool, unsupported []string) {
	if r.node == nil {
		return Node{}, false, nil
	}
	if out := r.node.unexplained(); len(out) > 0 {
		return Node{}, false, out
	}
	return explain(r.node, f, nil), true, nil
}

// ExplainSupported says, from the rule alone and reading nothing, whether
// Explain covers it, and if not the atoms it names that Explain does not.
func (r Rule) ExplainSupported() (bool, []string) {
	if r.node == nil {
		return false, nil
	}
	if out := r.node.unexplained(); len(out) > 0 {
		return false, out
	}
	return true, nil
}

// Blocking lists what keeps n from true: nothing when it is true; for an
// all, the blocking of each child that is not true, in rule order; for an
// any, one OneOf with each branch's blocking.
func (n *Node) Blocking() []Blocking {
	if n.Value == True {
		return nil
	}
	switch n.Kind {
	case "all":
		var out []Blocking
		for i := range n.Children {
			out = append(out, n.Children[i].Blocking()...)
		}
		return out
	case "any":
		branches := make([][]Blocking, len(n.Children))
		for i := range n.Children {
			branches[i] = n.Children[i].Blocking()
		}
		return []Blocking{{OneOf: branches}}
	}
	return []Blocking{{Atom: n}}
}

func explain(n *node, f Facts, path []int) Node {
	out := Node{Path: append([]int(nil), path...)}
	switch n.kind {
	case kAll, kAny:
		out.Kind = "all"
		out.Value = True
		if n.kind == kAny {
			out.Kind, out.Value = "any", False
		}
		for i, c := range n.children {
			cn := explain(c, f, append(path, i))
			out.Children = append(out.Children, cn)
			out.Value = combine(n.kind, out.Value, cn.Value)
		}
		return out
	}
	sf, has := f.(StepFacts)
	out.Value = Unknown
	switch n.kind {
	case kStepTasks:
		out.Kind, out.State, out.Min = AtomStepTasks, n.s1, n.n
		if has {
			out.Value = sf.StepTasks(n.s1, n.n)
		}
	case kStepLanded:
		out.Kind = AtomStepLanded
		if has {
			out.Value = sf.StepLanded()
		}
	case kStepSTG:
		out.Kind = AtomStepSTG
		if has {
			out.Value = sf.StepSTGSettled()
		}
	}
	return out
}

// combine folds one more child into an all or an any, as eval does but
// without stopping: all is false once a child is false, else unknown once
// one is unknown; any is true once a child is true, else unknown once one
// is unknown.
func combine(k nodeKind, acc, v Tri) Tri {
	if k == kAll {
		switch {
		case acc == False || v == False:
			return False
		case acc == Unknown || v == Unknown:
			return Unknown
		}
		return True
	}
	switch {
	case acc == True || v == True:
		return True
	case acc == Unknown || v == Unknown:
		return Unknown
	}
	return False
}

// unexplained lists the atoms under n that Explain does not cover.
func (n *node) unexplained() []string {
	seen := map[string]bool{}
	var walk func(*node)
	walk = func(n *node) {
		switch n.kind {
		case kAll, kAny:
			for _, c := range n.children {
				walk(c)
			}
		case kStepTasks, kStepLanded, kStepSTG:
		default:
			seen[n.atomName()] = true
		}
	}
	walk(n)
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// atomName is the grammar's name for an atom node.
func (n *node) atomName() string {
	if n.kind == kBound {
		return n.atom
	}
	for name, k := range atomKinds {
		if k == n.kind && k != kBound {
			return name
		}
	}
	return "unknown"
}
