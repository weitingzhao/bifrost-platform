package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// landing_delivered_by (DESIGN-step-landed-superseded-2026-11-01 §6, R5:5.6-1):
// on the repository's landing stream, the controller's record that a root
// the Owner declined (its latest operation failed{declined}: "Do not merge"
// on its card, or its landing grant request rejected) had its change
// delivered by another accepted implementation of the same step whose
// landing landed: the same change by the verbatim patch-id and the raw
// diff's sha256 of each one's A (landinggit.SameChange).
//
// It is a note beside the protocol, like landing_disposed: the fold never
// reads it, so the declined root stays failed{declined} for every reader.
// Only step_landed reads it (stepevidence.go), and checks again there that
// by is one of the step's landed roots.
type DeliveredBy struct {
	Root, Op, By, ByOp, OldA, NewA, PatchID, DiffSHA256 string
}

// ErrDeliveredByRefused: the stream does not support the note now; nothing
// was written.
var ErrDeliveredByRefused = errors.New("landing_delivered_by refused")

var deliveredByFields = []string{"root", "op", "by", "by_op", "old_a", "new_a", "patch_id", "diff_sha256"}

var patchID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var diffDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (d DeliveredBy) body() map[string]any {
	return map[string]any{"root": d.Root, "op": d.Op, "by": d.By, "by_op": d.ByOp, "old_a": d.OldA, "new_a": d.NewA,
		"patch_id": d.PatchID, "diff_sha256": d.DiffSHA256}
}

func deliveredByOf(m map[string]any) DeliveredBy {
	s := func(k string) string { v, _ := m[k].(string); return v }
	return DeliveredBy{Root: s("root"), Op: s("op"), By: s("by"), ByOp: s("by_op"), OldA: s("old_a"), NewA: s("new_a"),
		PatchID: s("patch_id"), DiffSHA256: s("diff_sha256")}
}

// badLandingDeliveredBy is why a landing_delivered_by body is refused, or "".
func badLandingDeliveredBy(v any) string {
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(deliveredByFields) {
		return "landing_delivered_by is an object of " + strings.Join(deliveredByFields, ", ")
	}
	for _, k := range deliveredByFields {
		if s, _ := m[k].(string); s == "" {
			return "landing_delivered_by needs " + k
		}
	}
	d := deliveredByOf(m)
	switch {
	case !landingRoot.MatchString(d.Root) || !landingRoot.MatchString(d.By):
		return "landing_delivered_by root and by are tasks"
	case !landingSHA.MatchString(d.OldA) || !landingSHA.MatchString(d.NewA):
		return "landing_delivered_by old_a and new_a are commits"
	case !patchID.MatchString(d.PatchID) || !diffDigest.MatchString(d.DiffSHA256):
		return "landing_delivered_by patch_id and diff_sha256 are digests"
	}
	return ""
}

// checkLandingDeliveredByAppend checks the note when it is written (§6 item
// 1): the controller writes it on the repository stream under its root;
// root's latest operation op is failed{declined} and claimed old_a; by_op is
// by's landed operation (a reverted one's phase is reverted) and claimed
// new_a; by is an accepted implementation that writes code, on root's step,
// and both are of the stream's repository. Anything else is refused and
// nothing written. The stream facts are read under the stream lock Append
// holds; the task rows (step, repository, state) are not locked and may move
// after, which is why step_landed checks by again when it reads the note.
func checkLandingDeliveredByAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, ok := ev.Body["landing_delivered_by"]
	if !ok {
		return nil
	}
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrDeliveredByRefused, why) }
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID != m["root"] {
		return refuse("it is written by the controller on the repository stream under its root")
	}
	if _, ok := ev.Body["landing"]; ok {
		return refuse("it is not a landing record")
	}
	d := deliveredByOf(m)
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	o, has := s.Operations[d.Op]
	switch {
	case !has || o.Task != d.Root || s.LatestOperation(d.Root) != d.Op || o.Phase != "failed" || o.Outcome != "declined":
		return refuse("root's latest operation is not op, failed{declined}")
	case o.A != d.OldA:
		return refuse("old_a is not the A op claimed")
	}
	b, has := s.Operations[d.ByOp]
	switch {
	case !has || b.Task != d.By || b.Phase != "landed":
		return refuse("by_op is not by's landed operation")
	case b.A != d.NewA:
		return refuse("new_a is not the A by_op claimed")
	}
	type task struct{ step, repo, role, state, effect string }
	read := func(id string) (task, error) {
		var t task
		err := tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id, ''), COALESCE(repo, ''), role, state, COALESCE(effect_class, '')
		  FROM agentrt.task WHERE task_id = $1`, id).Scan(&t.step, &t.repo, &t.role, &t.state, &t.effect)
		return t, err
	}
	root, err := read(d.Root)
	if err != nil {
		return err
	}
	by, err := read(d.By)
	if err != nil {
		return err
	}
	repo := strings.TrimPrefix(ev.Stream, "landing:")
	switch {
	case by.role != "implement" || by.state != "accepted" || by.effect != "branch":
		return refuse("by is not an accepted implementation that writes code")
	case root.step == "" || by.step != root.step:
		return refuse("by is not on root's step")
	case root.repo != repo || by.repo != repo:
		return refuse("root and by are not both of the stream's repository")
	}
	return nil
}

// RecordLandingDeliveredBy writes the note on repo's stream (the controller's
// backfill, controller/landing_delivered_by.go); the append checks decide.
func RecordLandingDeliveredBy(ctx context.Context, db DB, repo string, d DeliveredBy) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: "landing:" + repo, Type: "note", Actor: "controller", TaskID: d.Root,
			Body: map[string]any{"text": "Landing: the change of the declined " + d.Root + " landed from " + d.By, "landing_delivered_by": d.body()}})
		return err
	})
}

// DeliveredByPair is a declined root and a landed sibling whose changes the
// controller compares: root's latest operation op is failed{declined}, by's
// latest by_op landed, both accepted implementations of one step and of the
// stream's repository.
type DeliveredByPair struct {
	Root, Op, OldA, By, ByOp, NewA string
}

// LandingDeliveredByCandidates lists the pairs to compare on repo's stream
// now, in root then by order (§6 item 3: level-triggered, every pass), from
// s, the stream's fold as the caller read it (the landing pass's own). A
// root is left out while its latest note names a sibling still landed on the
// step, which is what step_landed reads (stepevidence.go); once that sibling
// moved off the step or its landing was reverted, the root is listed again
// with the step's other landed siblings.
func LandingDeliveredByCandidates(ctx context.Context, db DB, repo string, s landing.State) ([]DeliveredByPair, error) {
	stream := "landing:" + repo
	var roots []string
	for id, o := range s.Operations {
		if o.Phase == "failed" && o.Outcome == "declined" && s.LatestOperation(o.Task) == id {
			roots = append(roots, o.Task)
		}
	}
	if len(roots) == 0 {
		return nil, nil
	}
	var out []DeliveredByPair
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (n->>'root', n->>'op') n->>'root', n->>'op', n->>'by'
		  FROM (SELECT seq, body_json->'landing_delivered_by' n FROM agentrt.event WHERE event_stream_id = $1 AND task_id = ANY($2)
		    AND event_type = 'note' AND body_json ? 'landing_delivered_by') x
		  ORDER BY n->>'root', n->>'op', seq DESC`, stream, roots)
		if err != nil {
			return err
		}
		noted := map[[2]string]string{}
		for rows.Next() {
			var k [2]string
			var by string
			if err := rows.Scan(&k[0], &k[1], &by); err != nil {
				rows.Close()
				return err
			}
			noted[k] = by
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT r.task_id, b.task_id FROM agentrt.task r
		  JOIN agentrt.task b ON b.plan_node_id = r.plan_node_id AND b.task_id <> r.task_id
		  WHERE r.task_id = ANY($1) AND r.repo = $2 AND b.repo = $2
		    AND r.role = 'implement' AND r.state = 'accepted' AND r.effect_class = 'branch'
		    AND b.role = 'implement' AND b.state = 'accepted' AND b.effect_class = 'branch'
		  ORDER BY r.task_id, b.task_id`, roots, repo)
		if err != nil {
			return err
		}
		pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
			var p [2]string
			return p, row.Scan(&p[0], &p[1])
		})
		if err != nil {
			return err
		}
		landed := map[[2]string]bool{} // root, a sibling landed on its step
		for _, p := range pairs {
			if s.Operations[s.LatestOperation(p[1])].Phase == "landed" {
				landed[p] = true
			}
		}
		for _, p := range pairs {
			op, byOp := s.LatestOperation(p[0]), s.LatestOperation(p[1])
			if by, ok := noted[[2]string{p[0], op}]; (ok && landed[[2]string{p[0], by}]) || !landed[p] {
				continue
			}
			out = append(out, DeliveredByPair{Root: p[0], Op: op, OldA: s.Operations[op].A, By: p[1], ByOp: byOp, NewA: s.Operations[byOp].A})
		}
		return nil
	})
	return out, err
}

// landingDeliveredBy is who the latest landing_delivered_by note on stream
// says delivered root's operation op, "" for none (§6 item 4: the latest
// note wins).
func landingDeliveredBy(ctx context.Context, tx pgx.Tx, stream, root, op string) (string, error) {
	var by string
	// The note is under its root (checkLandingDeliveredByAppend), so the
	// task index finds it.
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT body_json->'landing_delivered_by'->>'by' FROM agentrt.event WHERE task_id = $2 AND event_stream_id = $1
	  AND event_type = 'note' AND body_json->'landing_delivered_by'->>'root' = $2 AND body_json->'landing_delivered_by'->>'op' = $3 ORDER BY seq DESC LIMIT 1), '')`, stream, root, op).Scan(&by)
	return by, err
}
