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
// No route calls this yet (CUT §4).

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

// ImportPlan imports a plan request as actor.
func ImportPlan(ctx context.Context, db DB, actor string, req planimport.Request) (ImportResult, error) {
	if err := req.Validate(); err != nil {
		return ImportResult{}, invalid("plan import: %v", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return ImportResult{}, err
	}
	doc, err := canon.Marshal(raw)
	if err != nil {
		return ImportResult{}, err
	}
	if len(doc) > maxArtifactBytes {
		return ImportResult{}, invalid("plan import: the request is %d bytes; an artifact is at most %d", len(doc), maxArtifactBytes)
	}
	sum := sha256.Sum256(doc)
	reqID := "sha256:" + hex.EncodeToString(sum[:])
	root := req.Root.ID
	out := ImportResult{Root: root, Request: reqID, Nodes: 1 + len(req.Nodes)}
	err = InTx(ctx, db, func(tx pgx.Tx) error { return importOnce(ctx, tx, actor, req, doc, reqID, &out) })
	if err != nil {
		return ImportResult{}, err
	}
	return out, nil
}

// importOnce imports the request under its tree's lock, or finds the import
// of this same request that already happened.
func importOnce(ctx context.Context, tx pgx.Tx, actor string, req planimport.Request, doc []byte, reqID string, out *ImportResult) error {
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
		return importTree(ctx, tx, actor, req, doc, reqID, out)
	}
	err := tx.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'legacy_imported'
	  AND body_json->>'source' = $2 ORDER BY event_id LIMIT 1`, PlanStream(root), planimport.ImportSource(reqID)).Scan(&out.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("plan %s already exists and was not imported from this request; change it through /plan/proposals", root)
	}
	return err
}

func importTree(ctx context.Context, tx pgx.Tx, actor string, req planimport.Request, doc []byte, reqID string, out *ImportResult) error {
	root := req.Root.ID
	if _, err := PutArtifact(ctx, tx, actor, "other", "application/json", doc); err != nil {
		return err
	}
	head, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "legacy_imported", Actor: actor,
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
