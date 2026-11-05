package plan

// The sign-off gate (R5:5.5-15, the debt paydown plan D1 of 2026-11-05): a
// step usually lands in several slices, and the three atoms that read the
// step's own work (step_tasks, step_landed, step_stg_settled) hold once the
// tasks bound so far are accepted, landed and settled. Without a further
// condition the first slice to settle marks the whole step done. So a rule
// naming any of them must hold only with the Owner's sign-off: its top node is
// all, and an owner_signoff is one of its parts, directly or through further
// alls. Anything else may sit below it; an owner_signoff only inside an any
// does not count, since another branch of the any can hold without it.

// CountsStepWork reports whether the rule names step_tasks, step_landed or
// step_stg_settled anywhere.
func (r Rule) CountsStepWork() bool {
	return r.Names(AtomStepTasks) || r.Names(AtomStepLanded) || r.Names(AtomStepSTG)
}

// SignoffGated reports whether the rule satisfies the sign-off gate: true for
// a rule that counts no step work, and for one whose top node is all with an
// owner_signoff reached from it through alls alone.
func (r Rule) SignoffGated() bool {
	if !r.CountsStepWork() {
		return true
	}
	return r.node != nil && r.node.kind == kAll && r.node.signoffThroughAll()
}

// signoffThroughAll: an owner_signoff is reached from this all through alls
// alone, so the rule cannot hold while every sign-off is false. Plan-view
// adoption wraps a rule in a further all beside its bound atoms
// (composeBoundRule), which keeps it gated.
func (n *node) signoffThroughAll() bool {
	for _, c := range n.children {
		if c.kind == kSignoff || c.kind == kAll && c.signoffThroughAll() {
			return true
		}
	}
	return false
}

// SignoffGateRule is the refusal's wording, shared by every place that writes
// a done rule so the words stay one.
const SignoffGateRule = "counts the step's own work (step_tasks, step_landed or step_stg_settled), so it must be an all with owner_signoff among its parts (directly or through further alls, never inside an any): otherwise the first slice to land would mark a step of several slices done (R5:5.5-15)"
