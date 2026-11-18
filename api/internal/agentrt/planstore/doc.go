// # The plan domain's contract
//
// This is what the plan domain offers other packages. Everything else in the
// package is inside: other packages use only the names below, and the
// domain is judged by contract_*_test.go, which call only these names and
// the base packages. A name here that changes meaning is a change of the
// contract; a change of anything else should not be noticed outside.
//
// Who calls: agentrtapi serves the routes, controller runs the passes, server
// wires the question store to Ask and the main-branch reader to PlanSources,
// and store binds the ports and hands LockTree, RootOf, DevTree and ViewTree
// on to externalwork.
//
// # Proposing and deciding a change
//
// ProposePlanChange(ctx, db, w, key, root, reason, changes, ask) proposes
// PlanChange values for one plan (POST /plan/proposals). key, root, reason
// and at least one change are required; root must be the root of a plan and
// every node named must be in it. Op is add_node, retitle, set_done_rule,
// rebind_step, drop_step, set_marks or adopt_plan_view (alone in its
// proposal; Artifact and ExpectedVersion); move_task is retired and refused.
// A set_done_rule rule must parse, and a rule that counts the step's own work
// must hold owner_signoff under an all. An architecture row's rule is gate
// tests only.
//
//   - Inside the standing authorization (a step added to an item with an
//     active step; the title of a planned step; a first done rule while the
//     plan-standing rule grants it) the change lands at once. The answer has
//     decision "applied", proposal_event_id and nodes (the ids added).
//     Written: plan_change_proposed, plan_change_decided by the controller,
//     plan_changed naming the decision, and the agentrt.plan_node rows.
//   - Otherwise the change is tried and rolled back, ask is called, and only
//     plan_change_proposed with question_id is written; the plan does not
//     move. The answer has decision "", question_id and waiting.
//
// DecidePlanChanges(ctx, db, question, ack) is the controller's pass. For each
// proposal with a card and no decision it reads question: answered with the
// first option's key "a" applies; answered otherwise is refused; withdrawn
// and expired are recorded as such; anything else waits. It writes one
// plan_change_decided per proposal, and returns how many it decided. A change
// that no longer fits is decided failed with its reason in error; a change
// that met a held row stays for the next pass. One proposal's failure does
// not stop the others. ack (may be nil) is then called for each applied,
// refused or failed card of the last 7 days not yet acknowledged, recorded
// as plan_card_settled;
// a failure is reported and tried by the next pass. Passes one after another
// acknowledge a card once; two at once decide it once and may both
// acknowledge it.
//
// Ask creates the Owner's card and returns its question id. It is called
// inside the proposal's transaction, so its Card.Key is stable across retries
// of one writer's key and the question store must answer a repeated key with
// the same question. A plan change's Card has no Options (the wiring adds
// CardApply and CardRefuse, keys "a" and "b"); an import's and a recorder
// import's name theirs. Card.ByRule says a card-auto-answer rule in force
// answers this kind, never for a view that binds completion atoms.
// RuntimeCard is a card as the question store reports it to the rule pass.
//
// # Notes
//
// PostPlanNote(ctx, db, w, key, nodeID, text) records a note on an
// architecture row (POST /plan/{id}/notes): event note on the plan's stream,
// naming the row. text is UTF-8 of 1 to 8192 bytes, not only white space, with
// no NUL, no local path and no reference to a task's workspace. Any other
// kind of node is refused. The latest note is what Progress shows. A text
// claiming the Owner's approval is written and also passed to the
// coordination port, which records the alert; it grants nothing.
//
// # Signing off
//
// SignOffStep(ctx, db, who, stepID, ref, evidence...) records the Owner's
// owner_signoff (POST /plan/{id}/signoff), the fact an owner_signoff atom
// waits for. who must be the actor "owner". ref may be empty when the done
// rule names exactly one. A ref of an adopted acceptance ("plan-acceptance:")
// needs 1 to 20 distinct stored artifact ids and the event carries the
// binding, acceptance digest, plan version and the sorted ids; any other ref
// takes no evidence. A step already signed answers the first signature with
// already_signed true and writes nothing; different evidence on an acceptance
// is ErrState. Signing moves nothing by itself: EvaluateStep does.
//
// # Importing
//
// ProposePlanImport(ctx, db, w, key, reason, req, ask) proposes a
// planimport.Request (POST /plan/imports): tried and rolled back, kept as an
// artifact, one card, a plan_change_proposed of op import on the plan's
// stream. A request waiting answers with its card; one imported answers
// decision "applied", imported false and its event_id; any other request for
// that root is refused. On the first option the decision pass writes
// legacy_imported caused by the decision, plan_node_created per node, the
// step moves, and for each done step plan_step_marked_done with its evidence.
//
// ProposeRecorderImport(ctx, db, w, key, reason, log, ask) does the same for a
// RecorderLog that lineage prepared (all three of its functions are
// required), on the log's own stream. Landing it is lineage's, through
// RecorderImportPorts.
//
// # Reading
//
// PlanViewPage(ctx, db, root, cursor) is GET /plan-view: one read-only
// repeatable-read snapshot, schema agentrt.plan_view_response/v1. An
// unadopted plan has plan_version 0. recent_changes holds at most 20 events;
// recent_next continues them, is valid only for its root, and never takes in
// an event committed after the first page.
//
// ProgressSince(ctx, db, since) is GET /progress: one read-only
// repeatable-read snapshot computed on each read, with needs_you, work,
// owner_next, plan (each round's counts and active steps; each architecture
// root's rows, tick and latest note), controls, recent, controller_alerts,
// schema and as_of_event_id. since is accepted and not used now.
//
// RootOf(ctx, tx, id) is the root of a node's tree. DevTree(ctx, tx, root)
// refuses a tree holding a mission of another tenant than dev. ViewTree is the
// head of a query binding the CTE tree to the node $1 and everything under it.
// LockTree(ctx, tx, root) holds one plan's tree until tx ends.
//
// # The done rule and observations
//
// RuledNodes(ctx, db) lists, in id order, the nodes with a done rule: steps
// neither dropped nor stopped, and architecture rows, each with its parsed
// rule. EvaluateStep(ctx, db, stepID, deployed) evaluates a step's rule and
// moves it: planned or active to done when the rule holds (cause done_rule,
// or done_rule_manual), done back to active when a standing condition lapses
// (standing_lapsed). It returns the state moved to, or "" when nothing moved.
// A step with no rule, dropped or stopped does not move, nor does a done step
// without a standing condition. A node that is not a step is pgx.ErrNoRows.
//
// CollectPlanObservations(ctx, db, src) reads each adopted binding through
// PlanSources.History outside any transaction and records its answer on
// plan-observation:<root> when it changed, and the pass on
// plan-observation-pass:<root>. It never moves a step. A failed read is
// returned and leaves the last answer standing until it is stale.
//
// # Errors
//
//	eventlog.ErrInvalid     the request breaks a rule of its own; nothing was written
//	eventlog.ErrState       SignOffStep: the step is done, stopped or dropped, or an acceptance has a different sign-off
//	actor.ErrConflict       the key was used for another request; or a view's expected_version is not the adopted one
//	actor.ErrNotYours       the thread's session belongs to another token
//	pgx.ErrNoRows           no such node (PostPlanNote, SignOffStep, EvaluateStep, RootOf) or plan (PlanViewPage)
//	ErrNotOwner             only the Owner may sign off
//	ErrAsk                  the Owner cannot be asked (no store, or it refused); nothing was written
//	ErrBusy                 ProposePlanChange met held rows on all 5 tries; nothing was written, try again
//	ErrQuestionGone         returned by ack: the store dropped the card; it is recorded and not asked about again
//	ErrPortUnbound          the operation needs a port nobody bound; it is refused
//
// An error from ask that wraps eventlog.ErrInvalid (a card of the wrong form)
// stays ErrInvalid. A change the database itself refuses comes back with the
// database's error from ProposePlanChange, and as a failed decision from
// DecidePlanChanges.
//
// # Transactions, keys and locks
//
// Functions taking a db run their own transactions; LockTree, RootOf and
// DevTree run in the caller's tx. The three propose functions and PostPlanNote
// are idempotent per writer, route and key: a retry gets the first answer and
// writes nothing, and a failure uses up nothing. They register a thread at its
// first write. DecidePlanChanges uses one transaction per decision.
//
// A write takes the tree's advisory lock, then the rows it changes in id order
// (steps it changes FOR UPDATE, the parent and siblings it rests on FOR
// SHARE), then the plan's stream. adopt_plan_view first takes the lock of all
// view adoptions and the trees of every adopted plan, in order. SignOffStep
// and EvaluateStep take the step's row, then the stream.
//
// # Ports
//
// What the domain needs of others it declares and another side binds at start
// (today store's init), once per process: BindPlanRules (RulePorts, type
// PlanRule), BindPlanExternalWork (ExternalWorkPorts), BindPlanRecorderImport
// (RecorderImportPorts), BindPlanCoordination (CoordinationPorts),
// BindPlanQuestions (QuestionPorts), BindPlanLoops (LoopPorts). A port nobody
// bound refuses with ErrPortUnbound; it never answers as if there were nothing:
//
//	RulePorts            ProposePlanChange; and every card that asks whether a rule answers it
//	ExternalWorkPorts    PlanViewPage; a stored external work description or note; binding a mission to a node of a plan with no adopted view
//	RecorderImportPorts  applying a recorder import (the proposal stays undecided)
//	CoordinationPorts    PostPlanNote
//	QuestionPorts        ProposePlanImport, ProposeRecorderImport
//	LoopPorts            ProgressSince
//
// ExternalWorkPorts is also how the domain reads the records of registered
// work, in the caller's transaction: the plan view's work, sign-off candidates,
// Recent changes and coverage, and whether a plan has any work. The domain
// does not name those records in SQL of its own.
//
// The event types and bodies named here are the plan's own (vocab.go); the
// tables are agentrt.plan_node and agentrt.plan_node_binding.
package planstore
