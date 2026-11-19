package planstore

import (
	"context"
	"errors"
)

// Using a plan (W-31 2.2b, the thin chief of staff, slice 3). The acceptance
// pass reads a submitted plan (planparse.go) and, when it passes, records it
// in one transaction, in this order:
//
//   - the Owner's decision cards, each with the option the work goes on with
//     while he has not answered (the question store, keyed by the task, its
//     round and the card's place, so a retried pass finds the same cards);
//   - the mission amended: its criteria, what is out of scope, a budget no
//     larger than it had (mission_amended);
//   - the implementation tasks, each with the repository's checks and the
//     plan's own, on the commit the planner read (task_proposed); they hang
//     on the planning task's step when it has one, and are off the plan,
//     "planned by task ...", when it has none;
//   - the choices the planner made, for the Owner to audit (decision_recorded,
//     the Owner's D2 A);
//   - the planning task accepted.
//
// The seat writes what the plan decides; the controller moves the task. A
// plan that does not pass goes back for another round with the reason added
// to its spec, as an implementation goes back for rework, until its rounds
// run out (max_rounds, 2 by default); then the task is blocked with the
// reason (review 216 A3). A mission that is no longer open blocks it at once.
// Nothing else is written but the plan's policy alerts (alerts.go), and the
// cards a refused round made are withdrawn in the same pass, before the
// refusal is written (review 217 B1).
//
// Lock order: the planning task's step (lockStep, as accept.go), its
// attempt, the task, the mission's row, the seat as a writer, the dependency
// graph and steps (lockBatch, the planning task's step again), then the
// streams. The graph lock comes before any event, as ProposeTasks over HTTP
// takes it before its own (review 216 A2).

// Withdraw withdraws a card the runtime asked, with a reason; a card that is
// already answered, withdrawn or gone is not an error.
type Withdraw func(ctx context.Context, questionID, reason string) error

// ErrAsked: the question store holds this question already, open or
// answered, under another key (a card of an earlier round, or another plan's).
// The Owner has been asked; a plan goes on with its default and does not
// count the card as its own (review 217 B2).
var ErrAsked = errors.New("store: the Owner was already asked this question")
