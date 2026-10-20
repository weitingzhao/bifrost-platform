package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// Importing a plan (W-31 round 2, CUT-plan-import-2026-10-19.md §3): the
// one-time move of a round of the work ledger into the runtime, in one
// transaction. The request is stored as an artifact and named by its
// canonical digest in a legacy_imported event, the head of the import; each
// node is created by its own plan_node_created event caused by that head.
// A step the ledger marks done gets a manual done rule, a note with its
// evidence (the ledger line and the request) and is moved to done by the same
// evaluation the controller runs, so it is done because its rule holds (v5
// §5.1.2); a step in progress is moved to active by the import. Importing the
// same request again changes nothing; a different request for a root that
// exists is refused: after the import, the plan changes through proposals.
// The architecture table (planimport/arch.go) imports the same way: its rows
// start with the done rules the request gives them, and what each row's
// line says it actually is now becomes its first note, held to a note's
// rules (plannote.go).
// The route (POST /plan/imports, planimportcard.go) proposes an import and
// puts it to the Owner on a decision card; the controller's plan decision
// pass lands it with importOnce, its head caused by the decision.

// ImportResult is what an import did.
type ImportResult struct {
	Root     string `json:"root"`
	Imported bool   `json:"imported"` // false: this request was imported before
	EventID  int64  `json:"event_id"` // the legacy_imported event
	Request  string `json:"request_artifact_id"`
	Nodes    int    `json:"nodes"` // the root and the nodes under it
	Done     int    `json:"done"`
	Active   int    `json:"active"`
}

// ImportPlan imports a plan request as actor, in a transaction of its own and
// without a decision: the landing the decision pass runs, and what the tests
// drive directly. No route calls it.
func ImportPlan(ctx context.Context, db DB, actor string, req planimport.Request) (ImportResult, error) {
	doc, reqID, err := prepareImport(req)
	if err != nil {
		return ImportResult{}, err
	}
	out := ImportResult{Root: req.Root.ID, Request: reqID, Nodes: 1 + len(req.Nodes)}
	err = InTx(ctx, db, func(tx pgx.Tx) error { return importOnce(ctx, tx, actor, req, doc, reqID, 0, &out) })
	if err != nil {
		return ImportResult{}, err
	}
	return out, nil
}

// prepareImport checks a request and gives its canonical bytes and the
// artifact id they are stored under.
func prepareImport(req planimport.Request) ([]byte, string, error) {
	if err := req.Validate(); err != nil {
		return nil, "", invalid("plan import: %v", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}
	doc, err := canon.Marshal(raw)
	if err != nil {
		return nil, "", err
	}
	if len(doc) > maxArtifactBytes {
		return nil, "", invalid("plan import: the request is %d bytes; an artifact is at most %d", len(doc), maxArtifactBytes)
	}
	sum := sha256.Sum256(doc)
	return doc, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// importedFrom is the import head of root's plan when it was imported from
// the request stored as reqID, or 0.
func importedFrom(ctx context.Context, tx pgx.Tx, root, reqID string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'legacy_imported'
	  AND body_json->>'source' = $2 ORDER BY event_id LIMIT 1`, PlanStream(root), planimport.ImportSource(reqID)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// importOnce imports the request under its tree's lock, its head caused by
// cause (the decision that lands it; 0 for none), or finds the import of
// this same request that already happened.
func importOnce(ctx context.Context, tx pgx.Tx, actor string, req planimport.Request, doc []byte, reqID string, cause int64,
	out *ImportResult) error {
	root := req.Root.ID
	if err := lockTree(ctx, tx, root); err != nil {
		return err
	}
	hook("plan import locked", root)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1)`, root).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return importTree(ctx, tx, actor, req, doc, reqID, cause, out)
	}
	id, err := importedFrom(ctx, tx, root, reqID)
	if err == nil && id == 0 {
		return invalid("plan %s already exists and was not imported from this request; change it through /plan/proposals", root)
	}
	out.EventID = id
	return err
}

func importTree(ctx context.Context, tx pgx.Tx, actor string, req planimport.Request, doc []byte, reqID string, cause int64,
	out *ImportResult) error {
	root := req.Root.ID
	if _, err := PutArtifact(ctx, tx, actor, "other", "application/json", doc); err != nil {
		return err
	}
	head, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "legacy_imported", Actor: actor, Cause: cause,
		Body: map[string]any{"source": planimport.ImportSource(reqID), "count": out.Nodes}})
	if err != nil {
		return err
	}
	out.EventID, out.Imported = head.EventID, true
	for _, n := range append([]planimport.Node{req.Root}, req.Nodes...) {
		if err := importNode(ctx, tx, actor, root, head.EventID, n); err != nil {
			return fmt.Errorf("plan import: node %s: %w", n.ID, err)
		}
	}
	noDeployment := func(context.Context, string) (string, map[string]any, bool) { return "", nil, false }
	for _, n := range req.Nodes {
		switch {
		case n.Kind == "arch_row":
			if err := archNote(ctx, tx, actor, root, head.EventID, reqID, n); err != nil {
				return fmt.Errorf("plan import: row %s: %w", n.ID, err)
			}
		case n.Kind != "step":
		case n.State == planimport.StateDone:
			ev, err := evidenceDoc(n.Evidence)
			if err != nil {
				return err
			}
			evidence, err := canon.Encode(map[string]any{"plan_node_id": n.ID, "request_artifact_id": reqID,
				"source": sourceDoc(req.Source), "evidence": ev})
			if err != nil {
				return err
			}
			eid, err := PutArtifact(ctx, tx, actor, "other", "application/json", evidence)
			if err != nil {
				return err
			}
			text := fmt.Sprintf("imported from %s, %s line %d: done", req.Source.File, req.Source.Section, n.Evidence.LineNo)
			if n.Evidence.MarkedAt != "" {
				text += ", marked " + n.Evidence.MarkedAt
			}
			if err = RecordManual(ctx, tx, actor, n.ID, planimport.ManualAtom(n.ID), text, []string{eid, reqID}); err != nil {
				return fmt.Errorf("plan import: step %s: %w", n.ID, err)
			}
			to, err := evaluateStep(ctx, tx, n.ID, noDeployment)
			if err != nil {
				return fmt.Errorf("plan import: step %s: %w", n.ID, err)
			}
			if to != "done" {
				return fmt.Errorf("plan import: step %s: its manual rule did not make it done", n.ID)
			}
			out.Done++
		case n.State == planimport.StateActive:
			if _, err := SetStepState(ctx, tx, actor, root, n.ID, "active", "legacy_imported"); err != nil {
				return fmt.Errorf("plan import: step %s: %w", n.ID, err)
			}
			out.Active++
		}
		hook("plan import step", n.ID)
	}
	return nil
}

// importNode writes one node with its plan_node_created event. A step the
// ledger marks done starts with its manual rule.
func importNode(ctx context.Context, tx pgx.Tx, actor, root string, head int64, n planimport.Node) error {
	if _, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_node_created", Actor: actor, PlanNode: n.ID, Cause: head,
		Body: map[string]any{"node_kind": n.Kind, "label": n.Label, "title": n.Title, "ordinal": n.Ordinal}}); err != nil {
		return err
	}
	var state any
	rule := []byte("{}")
	if n.Kind == "arch_row" && len(n.Rule) > 0 {
		var err error
		if rule, err = canonRule(n.Rule); err != nil {
			return err
		}
	}
	if n.Kind == "step" {
		state = planimport.StatePlanned
		if n.State == planimport.StateDone {
			var err error
			if rule, err = canon.Encode(map[string]any{"manual": planimport.ManualAtom(n.ID)}); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title,
	  work_id, step_kind, owner_step, effect_step, done_rule_json, state, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13)`,
		n.ID, nullable(n.Parent), n.Kind, n.Ordinal, n.Label, n.Title, nullable(n.WorkID), nullable(n.StepKind),
		n.Owner, n.Effect, string(rule), state, actor)
	return err
}

// archNote records what a row's line says the row actually is now as its
// first note, caused by the import's head, with the request as its evidence.
// A line with nothing after its title gives no note.
func archNote(ctx context.Context, tx pgx.Tx, actor, root string, head int64, reqID string, n planimport.Node) error {
	text := planimport.ArchNow(n.Evidence.Line)
	if text == "" {
		return nil
	}
	if err := checkNoteText(text); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "note", Actor: actor, PlanNode: n.ID, Cause: head,
		Body: map[string]any{"text": text, "evidence": []string{reqID}}})
	return err
}

func sourceDoc(s planimport.Source) map[string]any {
	d := map[string]any{"file": s.File, "section": s.Section, "checked_at": s.CheckedAt, "sha256": s.SHA256,
		"first_line": s.FirstLine, "last_line": s.LastLine}
	if s.Commit != "" {
		d["commit"] = s.Commit
	}
	return d
}

// evidenceDoc is the step's evidence exactly as the request holds it, so the
// step's own artifact never says less than the request (review 312 B1).
func evidenceDoc(e *planimport.Evidence) (any, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return canon.Parse(raw)
}
