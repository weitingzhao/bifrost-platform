package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// One root's explanation (R5:5.5-19 §2, §5): every step under it gets a
// result, in one read-only repeatable-read transaction that first checks
// what it would read and stops rather than read too much.

// ExplainBounds bound one computation (§5).
type ExplainBounds struct {
	Steps              int           // steps explained per root
	Rows, Bytes, Tasks int64         // rows and body_json bytes of every stream read, together; tasks under the root
	Deadline           time.Duration // the transaction's time
}

// DefaultExplainBounds are §5's values, lowered by measurement (§5: the
// reused folds take no context, so the input bound alone bounds them). At
// 300,000 rows of one landing stream each fold took over three minutes and
// its read over 500 MB: the folds grow with the square of the operations
// (TestExplainFoldsAtTheBound). 20,000 rows keep each under 1.5 s; PROD's
// platform stream held about 8,400 on 2026-11-03.
func DefaultExplainBounds() ExplainBounds {
	return ExplainBounds{Steps: 20, Rows: 20_000, Bytes: 32 << 20, Tasks: 2_000, Deadline: 5 * time.Second}
}

// ErrExplainIncomplete: the computation stopped short; its Reason is the
// contract's incomplete reason.
type ErrExplainIncomplete struct{ Reason, Which string }

func (e *ErrExplainIncomplete) Error() string {
	if e.Which != "" {
		return "explain: " + e.Reason + " (" + e.Which + ")"
	}
	return "explain: " + e.Reason
}

// ExplainRoot is the explanation of root's steps, keyed by step id, and when
// it was read, within DefaultExplainBounds. A step that is done, stopped or
// dropped has no entry (the page shows none for it).
func ExplainRoot(ctx context.Context, db DB, root string) (map[string]any, time.Time, error) {
	return ExplainRootWithin(ctx, db, root, DefaultExplainBounds())
}

// ExplainRootWithin is ExplainRoot within lim.
func ExplainRootWithin(ctx context.Context, db DB, root string, lim ExplainBounds) (map[string]any, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, lim.Deadline)
	defer cancel()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	if err = explainPrecheck(ctx, tx, root, lim); err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	type step struct {
		id, state, doc string
		version        int64
	}
	rows, err := tx.Query(ctx, viewTree+`SELECT n.plan_node_id, n.state, n.done_rule_json::text, n.version
	  FROM tree JOIN agentrt.plan_node n USING(plan_node_id) LEFT JOIN agentrt.plan_node p ON p.plan_node_id = n.parent_plan_node_id
	  WHERE n.node_kind = 'step' ORDER BY p.ordinal NULLS FIRST, n.ordinal, n.plan_node_id`, root)
	if err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	steps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (step, error) {
		var s step
		return s, r.Scan(&s.id, &s.state, &s.doc, &s.version)
	})
	if err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	if len(steps) == 0 {
		return nil, time.Time{}, pgx.ErrNoRows
	}
	// First, from the rules alone, which steps are explained (supported and
	// within the step bound); nothing is read for the others.
	out := map[string]any{}
	type todo struct {
		id   string
		rule plan.Rule
		res  map[string]any
	}
	var explain []todo
	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			return nil, time.Time{}, explainErr(ctx, err)
		}
		res := map[string]any{"node_version": s.version}
		switch s.state {
		case "planned":
			res["kind"] = "not_started"
			out[s.id] = res
			continue
		case "active":
		default:
			continue
		}
		rule, err := plan.Parse([]byte(s.doc))
		switch {
		case err != nil:
			res["kind"], res["code"] = "explain_error", "rule_unreadable"
		case rule.Empty():
			res["kind"] = "no_done_rule"
		default:
			if ok, atoms := rule.ExplainSupported(); !ok {
				res["kind"], res["atoms"] = "rule_not_supported", atoms
			} else if len(explain) == lim.Steps {
				res["kind"], res["reason"] = "not_explained", "over_cap"
			} else {
				explain = append(explain, todo{s.id, rule, res})
				continue
			}
		}
		out[s.id] = res
	}
	// The implementations of those steps, by repository: the only roots
	// whose records the explanation reads (§5).
	ids := make([]string, len(explain))
	for i, x := range explain {
		ids[i] = x.id
	}
	roots := map[string]map[string]bool{}
	rows, err = tx.Query(ctx, `SELECT COALESCE(repo, ''), task_id FROM agentrt.task WHERE plan_node_id = ANY($1)
	  AND role = 'implement' AND state = 'accepted' AND effect_class = 'branch'`, ids)
	if err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	for rows.Next() {
		var repo, task string
		if err := rows.Scan(&repo, &task); err != nil {
			rows.Close()
			return nil, time.Time{}, explainErr(ctx, err)
		}
		if roots[repo] == nil {
			roots[repo] = map[string]bool{}
		}
		roots[repo][task] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	shared := map[string]*repoExplain{}
	for _, x := range explain {
		if err := ctx.Err(); err != nil {
			return nil, time.Time{}, explainErr(ctx, err)
		}
		res := x.res
		f := &stepFacts{ctx: ctx, tx: tx, step: x.id, repos: shared, explainAt: now, explainRoots: roots,
			deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false }}
		n, _, _ := x.rule.Explain(f)
		blockers, omitted := f.blockers(n.Blocking())
		if f.err == nil {
			f.err = ctx.Err() // a fold that cannot be stopped may have run past the deadline
		}
		if f.err != nil {
			if ctx.Err() != nil {
				return nil, time.Time{}, explainErr(ctx, ctx.Err())
			}
			res["kind"], res["code"] = "explain_error", "read_failed"
			out[x.id] = res
			continue
		}
		res["kind"], res["rule"], res["blockers"] = "explained", n.Value.String(), blockers
		if omitted > 0 {
			res["omitted"] = omitted
		}
		out[x.id] = res
	}
	// The whole result or none: work that ran past the deadline is not kept.
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, explainErr(ctx, err)
	}
	return out, now, nil
}

// blockers turns a rule's blocking into the contract's blockers within
// explainItems items (each blocker, task candidate, card and block one
// item; a one_of is its branches' items), in rule order, and how many
// blockers were left out once the budget ran out, inside a one_of's
// branches as at the top (§5). The rule's value is not touched.
func (f *stepFacts) blockers(bs []plan.Blocking) ([]Blocker, int) {
	b := &itemBudget{left: explainItems}
	out := f.within(bs, b)
	if f.err != nil {
		return nil, 0
	}
	return out, b.omitted
}

// itemBudget is what is left of one step's items; once a blocker does not
// fit, every later one is left out too, so what is shown is a prefix.
type itemBudget struct {
	left, omitted int
	full          bool
}

func (f *stepFacts) within(bs []plan.Blocking, b *itemBudget) []Blocker {
	out := []Blocker{}
	for _, x := range bs {
		if f.err != nil {
			return nil
		}
		if f.err = f.ctx.Err(); f.err != nil {
			return nil
		}
		if x.Atom == nil {
			branches := make([][]Blocker, len(x.OneOf))
			shown := false
			for i, br := range x.OneOf {
				branches[i] = f.within(br, b)
				shown = shown || len(branches[i]) > 0
			}
			if shown { // a one_of with nothing left in any branch says nothing: its blockers are counted
				out = append(out, Blocker{"atom": "one_of", "branches": branches})
			}
			continue
		}
		atom := f.explainAtom
		if f.atomBlockers != nil {
			atom = f.atomBlockers
		}
		for _, y := range atom(x.Atom) {
			if f.err = f.ctx.Err(); f.err != nil {
				return nil
			}
			w := blockerWeight(y)
			if b.full || w > b.left {
				b.full = true
				b.omitted++
				continue
			}
			b.left -= w
			out = append(out, y)
		}
	}
	return out
}

// blockerWeight counts one blocker's items: itself and everything it lists.
func blockerWeight(b Blocker) int {
	w := 1
	for k, v := range b {
		switch x := v.(type) {
		case []map[string]any:
			if k == "candidates" || k == "cards" || k == "blocks" || k == "repo_blocks" {
				for _, m := range x {
					w += blockerWeight(m)
				}
			}
		case map[string]any:
			switch k {
			case "quarantine", "rework":
				w += blockerWeight(x) - 1
			case "card":
				w++
			}
		case [][]Blocker:
			w--
			for _, br := range x {
				for _, y := range br {
					w += blockerWeight(y)
				}
			}
		}
	}
	return w
}

// explainPrecheck counts what this root's explanation would read (§5):
// the landing, card and B5 streams of its tasks' repositories, and its
// tasks; any count over its bound stops the computation before it reads.
func explainPrecheck(ctx context.Context, tx pgx.Tx, root string, lim ExplainBounds) error {
	var tasks int64
	var repos []string
	if err := tx.QueryRow(ctx, viewTree+`SELECT count(*), COALESCE(array_agg(DISTINCT t.repo) FILTER (WHERE t.repo IS NOT NULL AND t.repo <> ''), '{}')
	  FROM agentrt.task t JOIN tree USING(plan_node_id)`, root).Scan(&tasks, &repos); err != nil {
		return err
	}
	if tasks > lim.Tasks {
		return &ErrExplainIncomplete{Reason: "input_too_large", Which: "tasks"}
	}
	// B5's fold reads the landing stream of the repository it releases
	// (StgReleaseState), so it is counted with that stream.
	streams := []string{}
	for _, r := range repos {
		streams = append(streams, "landing:"+r, CardsStream(r))
	}
	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = ANY($1)`, streams).Scan(&n); err != nil {
		return err
	}
	if n > lim.Rows {
		return &ErrExplainIncomplete{Reason: "input_too_large", Which: "rows"}
	}
	// The bytes the reads decode: the JSON text, not its stored (possibly
	// compressed) size, which pg_column_size would give. Counted only once
	// the rows are within bound, so this cast stays bounded too.
	var bytes int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(octet_length(body_json::text)), 0) FROM agentrt.event WHERE event_stream_id = ANY($1)`,
		streams).Scan(&bytes); err != nil {
		return err
	}
	if bytes > lim.Bytes {
		return &ErrExplainIncomplete{Reason: "input_too_large", Which: "bytes"}
	}
	return nil
}

// explainErr names a deadline as the contract's timeout.
func explainErr(ctx context.Context, err error) error {
	var inc *ErrExplainIncomplete
	switch {
	case errors.As(err, &inc):
		return err
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &ErrExplainIncomplete{Reason: "timeout"}
	}
	return fmt.Errorf("explain: %w", err)
}
