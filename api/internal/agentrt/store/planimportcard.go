package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// The first import of a plan through the interface (POST /plan/imports;
// CUT-plan-import-2026-10-19.md §4 A, Owner 2026-10-19 「三件照建议」). A
// thread proposes a request; it is tried whole and rolled back, so a request
// the database would refuse is refused now; the request is kept as an
// artifact and the Owner gets one decision card. The proposal is a
// plan_change_proposed of one change, op import, so the controller's plan
// decision pass (plandecide.go) decides it like any plan change: on Apply it
// lands the import in the transaction that records the decision, the
// import's head caused by that decision.

// OpImport is the change an import proposal holds.
const OpImport = "import"

// The import card's options, in the order the question store keys them (a,
// b): the decision pass applies the first (CardApplyKey).
const (
	ImportApply  = "Import the plan"
	ImportRefuse = "Do not import it"
)

// importOf is the request artifact of a proposal that is an import.
func importOf(changes []PlanChange) (string, bool) {
	if len(changes) == 1 && changes[0].Op == OpImport && changes[0].Artifact != "" {
		return changes[0].Artifact, true
	}
	return "", false
}

// ProposePlanImport proposes importing req, why reason says. key is the
// writer's idempotency key. A request this plan was already imported from
// answers so, with no card; one waiting on the Owner answers with its card.
func ProposePlanImport(ctx context.Context, db DB, w Writer, key, reason string, req planimport.Request, ask Ask) (map[string]any, error) {
	switch {
	case key == "":
		return nil, invalid("an idempotency key is required")
	case strings.TrimSpace(reason) == "":
		return nil, invalid("a plan import says why")
	}
	doc, reqID, err := prepareImport(req)
	if err != nil {
		return nil, err
	}
	root := req.Root.ID
	request := map[string]any{"root": root, "request_artifact_id": reqID, "reason": reason}
	return idempotentAs(ctx, db, w, "plan/imports", key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockTree(ctx, tx, root); err != nil {
			return nil, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1)`, root).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			head, err := importedFrom(ctx, tx, root, reqID)
			if err != nil {
				return nil, err
			}
			if head == 0 {
				return nil, invalid("plan %s already exists and was not imported from this request; change it through /plan/proposals", root)
			}
			return map[string]any{"decision": "applied", "imported": false, "event_id": head, "request_artifact_id": reqID}, nil
		}
		waiting, err := waitingImport(ctx, tx, root)
		if err != nil {
			return nil, err
		}
		if waiting != nil {
			if waiting.artifact != reqID {
				return nil, invalid("another import of %s (%s) waits on the Owner's card %s", root, waiting.artifact, waiting.question)
			}
			return map[string]any{"proposal_event_id": waiting.event, "decision": "", "question_id": waiting.question,
				"request_artifact_id": reqID, "waiting": "the Owner decides a plan import"}, nil
		}
		// Tried whole, with every deferred check made now, and rolled back.
		if err = tryImport(ctx, tx, req, doc, reqID); err != nil {
			return nil, err
		}
		if _, err = PutArtifact(ctx, tx, w.Actor, "other", "application/json", doc); err != nil {
			return nil, err
		}
		cardKey := canonKey(w.Actor, "import:"+key)
		body := importCardBody(w, cardKey, reason, req, reqID)
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			return nil, invalid("the Owner's card for this import would be %d bytes, more than the question store's %d", n, questions.BodyLimit)
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		q, err := ask(ctx, Card{Key: cardKey, Title: "Import plan " + root + ": " + firstLine(reason), Body: body,
			Options: []string{ImportApply, ImportRefuse}, Meanwhile: "the plan is not imported; the board keeps reading the ledger",
			OwnerDuty: "irreversible", Urgency: "when you can: only this import waits for you"})
		if errors.Is(err, ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		changes, err := changesDoc([]PlanChange{{Op: OpImport, Artifact: reqID}})
		if err != nil {
			return nil, err
		}
		proposed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor,
			Body: map[string]any{"changes": changes, "reason": reason, "standing": false, "token": w.Token, "question_id": q}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"proposal_event_id": proposed.EventID, "decision": "", "question_id": q, "request_artifact_id": reqID,
			"waiting": "the Owner decides a plan import"}, nil
	})
}

type pendingImport struct {
	event              int64
	artifact, question string
}

// waitingImport is the import of root that waits on a decision, if any.
func waitingImport(ctx context.Context, tx pgx.Tx, root string) (*pendingImport, error) {
	var p pendingImport
	err := tx.QueryRow(ctx, `SELECT p.event_id, p.body_json->'changes'->0->>'artifact', p.body_json->>'question_id' FROM agentrt.event p
	  WHERE p.event_stream_id = $1 AND p.event_type = 'plan_change_proposed' AND p.body_json->'changes'->0->>'op' = $2
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.event_stream_id = p.event_stream_id
	      AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.event_id LIMIT 1`, PlanStream(root), OpImport).Scan(&p.event, &p.artifact, &p.question)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// tryImport imports in a savepoint with every deferred check made at once,
// then rolls back; a request the database refuses is refused here.
func tryImport(ctx context.Context, tx pgx.Tx, req planimport.Request, doc []byte, reqID string) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT try_plan_import`); err != nil {
		return err
	}
	out := ImportResult{Root: req.Root.ID, Request: reqID, Nodes: 1 + len(req.Nodes)}
	err := importTree(ctx, tx, "controller", req, doc, reqID, 0, &out)
	if err == nil {
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	}
	if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT try_plan_import`); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// importCardBody is the import card's text: who proposes, what the plan is,
// where it comes from, and what each answer does.
func importCardBody(w Writer, cardKey, reason string, req planimport.Request, reqID string) string {
	if req.Root.Kind == "arch_root" {
		return archCardBody(w, cardKey, reason, req, reqID)
	}
	items, done, active, planned := 0, 0, 0, 0
	for _, n := range req.Nodes {
		switch {
		case n.Kind == "item":
			items++
		case n.State == planimport.StateDone:
			done++
		case n.State == planimport.StateActive:
			active++
		default:
			planned++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import plan %s (%s), proposal %s.\n\nWhy: %s\n\n", w.Actor, req.Root.ID, req.Root.Title, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d items, %d steps: %d done (each with its ledger line as evidence; Progress counts them as manual), %d in progress, %d not started.\n\n",
		items, done+active+planned, done, active, planned)
	fmt.Fprintf(&b, "From %s, %s, lines %d-%d, checked at %s", s.File, s.Section, s.FirstLine, s.LastLine, s.CheckedAt)
	if s.Commit != "" {
		fmt.Fprintf(&b, ", commit %s", s.Commit)
	}
	fmt.Fprintf(&b, ".\nSection sha256 %s; request %s.\n\n", s.SHA256, reqID)
	b.WriteString("Importing writes the plan under your decision; its nodes and numbers are then fixed and can only change by plan changes. " +
		"Not importing writes nothing; the board keeps reading the ledger.")
	return b.String()
}

// archCardBody is the import card's text for the architecture table.
func archCardBody(w Writer, cardKey, reason string, req planimport.Request, reqID string) string {
	ruled, noted := 0, 0
	for _, n := range req.Nodes {
		if len(n.Rule) > 0 {
			ruled++
		}
		if planimport.ArchNow(n.Evidence.Line) != "" {
			noted++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the architecture table %s (%s), proposal %s.\n\nWhy: %s\n\n", w.Actor, req.Root.ID, req.Root.Title,
		cardKey[:12], reason)
	fmt.Fprintf(&b, "%d rows: %d with gate tests, ticked by whether those tests passed for what is deployed now; %d with none yet, shown unknown (no gate test yet). "+
		"%d rows get what their line says they are now as their first note.\n\n", len(req.Nodes), ruled, len(req.Nodes)-ruled, noted)
	fmt.Fprintf(&b, "From %s, %s, lines %d-%d, checked at %s", s.File, s.Section, s.FirstLine, s.LastLine, s.CheckedAt)
	if s.Commit != "" {
		fmt.Fprintf(&b, ", commit %s", s.Commit)
	}
	fmt.Fprintf(&b, ".\nSection sha256 %s; request %s.\n\n", s.SHA256, reqID)
	b.WriteString("Importing writes the table under your decision; its rows and numbers are then fixed, their rules change only by plan changes, " +
		"and their notes are added to, never changed. Not importing writes nothing; the board keeps reading the ledger.")
	return b.String()
}

// decideImport lands an import proposal's request in tx under the decision
// decided: the request is read back from its artifact.
func decideImport(ctx context.Context, tx pgx.Tx, artifact string, decided int64) error {
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, artifact).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("the import's request %s is not stored", artifact)
	}
	if err != nil {
		return err
	}
	var req planimport.Request
	if err = decodeStrict(doc, &req); err != nil {
		return invalid("the import's request %s does not read: %v", artifact, err)
	}
	again, reqID, err := prepareImport(req)
	if err != nil {
		return err
	}
	if reqID != artifact || string(again) != string(doc) {
		return invalid("the import's request %s does not read back as itself", artifact)
	}
	out := ImportResult{Root: req.Root.ID, Request: reqID, Nodes: 1 + len(req.Nodes)}
	return importOnce(ctx, tx, "controller", req, doc, reqID, decided, &out)
}

// decodeStrict reads one JSON value into v, refusing a field v does not have.
func decodeStrict(doc []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("more than one value")
	}
	return nil
}
