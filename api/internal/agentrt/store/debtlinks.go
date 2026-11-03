package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A debt's links to other debts (DESIGN-tech-debt-runtime part 2 §5,
// debt_edge; its links to steps, issues, the Owner's waits, ratchets and
// gaps, and the missions working on it, are 0011's debt_ref and debt_work,
// which the store writes from 5.8-3a's next part). Whether a debt can start
// is computed from them, never stored (§5.5). The waiting graph is blocks (the debt held up waits on the
// one holding it) and part_of (the umbrella waits on its parts) together; a
// link of either kind that would close a cycle in it is refused, whichever
// kinds the cycle mixes, under the debt graph lock so two halves of a cycle
// written at once cannot both commit (0011 checks it again).

// CycleError is ErrCycle with the cycle: each debt in Path waits on the
// next, the first wait being the one refused.
type CycleError struct{ Path []string }

func (e *CycleError) Error() string {
	return fmt.Sprintf("%v: %s", ErrCycle, strings.Join(e.Path, " waits on "))
}
func (e *CycleError) Unwrap() error { return ErrCycle }

// DebtLink is a link between two debts (link_debts): blocks (From holds To
// up until it is landed or closed, Needs), part_of (From is a part of the
// umbrella To), follows_from (From came out of To), related.
type DebtLink struct{ Kind, From, To, Needs string }

// LinkDebts adds a live edge, on From's stream (debt_edge_added).
func LinkDebts(ctx context.Context, db DB, w Writer, key string, l DebtLink) (map[string]any, error) {
	switch {
	case !slices.Contains(debtEdgeKinds, l.Kind):
		return nil, invalid("a link is one of %v, not %q", debtEdgeKinds, l.Kind)
	case !debtIDRE.MatchString(l.From) || !debtIDRE.MatchString(l.To) || l.From == l.To:
		return nil, invalid("a link joins two debts, not %q and %q", l.From, l.To)
	case (l.Kind == "blocks") != (l.Needs != ""):
		return nil, invalid("a blocks link says what it needs (landed or closed), and only it")
	case l.Needs != "" && !slices.Contains(debtNeeds, l.Needs):
		return nil, invalid("a blocks link needs landed or closed, not %q", l.Needs)
	}
	req := map[string]any{"kind": l.Kind, "from": l.From, "to": l.To, "needs": l.Needs}
	return debtOp(ctx, db, w, "debt-link", key, req, func(tx pgx.Tx) (map[string]any, error) {
		if l.Kind == "blocks" || l.Kind == "part_of" {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, debtGraphLock); err != nil {
				return nil, err
			}
			hook("debt_graph_locked", l.From)
		}
		for _, d := range []string{l.From, l.To} {
			if _, err := readDebt(ctx, tx, d, false); err != nil {
				return nil, err
			}
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.debt_edge WHERE ended_at IS NULL AND edge_kind = $1
		  AND from_debt_id = $2 AND to_debt_id = $3)`, l.Kind, l.From, l.To).Scan(&live); err != nil {
			return nil, err
		}
		if live {
			return nil, fmt.Errorf("%w: %s %s %s is linked already", ErrState, l.From, l.Kind, l.To)
		}
		if l.Kind == "blocks" || l.Kind == "part_of" {
			path, err := waitPath(ctx, tx, l.From, l.To)
			if err != nil {
				return nil, err
			}
			if path != nil {
				return nil, &CycleError{Path: append([]string{l.To}, path...)}
			}
		}
		ids, err := nextDebtIDs(ctx, tx, "debt_edge", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"debt_edge_id": ids[0], "edge_kind": l.Kind, "from_debt_id": l.From, "to_debt_id": l.To}
		if l.Needs != "" {
			body["needs"] = l.Needs
		}
		if _, err := appendDebt(ctx, tx, "debt_edge_added", w.Actor, l.From, "", "", body); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.debt_edge (debt_edge_id, edge_kind, from_debt_id, to_debt_id, needs, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,$4,$5,$6)`, ids[0], l.Kind, l.From, l.To, nullable(l.Needs), w.Actor); err != nil {
			return nil, err
		}
		return map[string]any{"debt_edge_id": ids[0], "kind": l.Kind, "from": l.From, "to": l.To}, nil
	})
}

// waitPath is the chain by which waiter already waits on target in the live
// waiting graph, waiter first and target last, or nil when it does not: a
// breadth-first walk, so the chain is a shortest one, its ties broken in
// the text order of the ids.
func waitPath(ctx context.Context, tx pgx.Tx, waiter, target string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT to_debt_id, from_debt_id FROM agentrt.debt_edge
	  WHERE ended_at IS NULL AND edge_kind IN ('blocks','part_of') ORDER BY to_debt_id, from_debt_id`)
	if err != nil {
		return nil, err
	}
	waits := map[string][]string{}
	for rows.Next() {
		var to, from string
		if err := rows.Scan(&to, &from); err != nil {
			rows.Close()
			return nil, err
		}
		waits[to] = append(waits[to], from)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	parent := map[string]string{waiter: ""}
	for queue := []string{waiter}; len(queue) > 0; queue = queue[1:] {
		at := queue[0]
		if at == target {
			var path []string
			for n := at; n != ""; n = parent[n] {
				path = append(path, n)
			}
			slices.Reverse(path)
			return path, nil
		}
		for _, next := range waits[at] {
			if _, seen := parent[next]; !seen {
				parent[next] = at
				queue = append(queue, next)
			}
		}
	}
	return nil, nil
}

// EndDebtEdge ends a live edge (debt_edge_ended, on its From's stream): a
// link withdrawn; the other ends (a merge's rewiring, a blocker withdrawn)
// come with their own moves (5.8-4).
func EndDebtEdge(ctx context.Context, db DB, w Writer, key string, edge int64) (map[string]any, error) {
	return debtOp(ctx, db, w, "debt-unlink", key, map[string]any{"debt_edge_id": edge}, func(tx pgx.Tx) (map[string]any, error) {
		var from string
		var ended bool
		err := tx.QueryRow(ctx, `SELECT from_debt_id, ended_at IS NOT NULL FROM agentrt.debt_edge WHERE debt_edge_id = $1 FOR UPDATE`,
			edge).Scan(&from, &ended)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, invalid("there is no debt link %d", edge)
		case err != nil:
			return nil, err
		case ended:
			return nil, fmt.Errorf("%w: debt link %d has ended already", ErrState, edge)
		}
		if _, err = appendDebt(ctx, tx, "debt_edge_ended", w.Actor, from, "", "",
			map[string]any{"debt_edge_id": edge, "ended_why": "withdrawn_link"}); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.debt_edge SET ended_at = clock_timestamp(), ended_why = 'withdrawn_link' WHERE debt_edge_id = $1`, edge)
		return map[string]any{"debt_edge_id": edge, "ended": true}, err
	})
}
