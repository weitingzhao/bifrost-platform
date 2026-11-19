package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// cardStore is a question store as the card-auto-answer rule sees it: cards
// asked (with when, and the option they recommend), open until answered.
type cardStore struct {
	mu      sync.Mutex
	cards   map[string]*storedCard
	order   []string
	answers int
	acked   map[string]int
	during  func(id string) // runs as an answer is written, before it lands
	after   func(id string) // runs once an answer has landed, before the rule's caller hears so
	ackErr  error           // what the next acknowledgement answers
}

type storedCard struct {
	card                     planstore.RuntimeCard
	status, option, by, text string
	read                     bool
}

func (s *cardStore) put(c planstore.RuntimeCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cards == nil {
		s.cards, s.acked = map[string]*storedCard{}, map[string]int{}
	}
	s.cards[c.ID] = &storedCard{card: c, status: "open"}
	s.order = append(s.order, c.ID)
}

// ask makes a card asked now; created moves it back.
func (s *cardStore) ask(_ context.Context, c planstore.Card) (string, error) {
	id := "q_" + c.Key[:16]
	s.mu.Lock()
	_, have := s.cards[id]
	s.mu.Unlock()
	if !have {
		rec := ""
		if c.Recommended > 0 {
			rec = string(rune('a' + c.Recommended - 1))
		}
		// The question store keys the options a, b, … in order.
		var keys []string
		for i := range c.Options {
			keys = append(keys, string(rune('a'+i)))
		}
		s.put(planstore.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now(), Options: keys})
	}
	return id, nil
}

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []planstore.Card
	answers map[string]seenfacts.Question
	acked   map[string]int
}

func (a *asker) ack(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acked == nil {
		a.acked = map[string]int{}
	}
	a.acked[id]++
	return nil
}

func (a *asker) ask(_ context.Context, c planstore.Card) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, prev := range a.cards {
		if prev.Key == c.Key {
			return "q_" + string(rune('a'+i)), nil
		}
	}
	a.cards = append(a.cards, c)
	return "q_" + string(rune('a'+len(a.cards)-1)), nil
}

func (a *asker) lastID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return "q_" + string(rune('a'+len(a.cards)-1))
}

func (a *asker) answer(id, status, option string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answers == nil {
		a.answers = map[string]seenfacts.Question{}
	}
	a.answers[id] = seenfacts.Question{ID: id, Status: status, Option: option, AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}
}

func (a *asker) question(_ context.Context, id string) (seenfacts.Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.answers[id]
	if !ok {
		return seenfacts.Question{ID: id, Status: "open"}, true
	}
	return q, true
}

// What the plan tests still in this package use of the helpers of those that
// moved to planstore (TD-582); a small copy each, gone with the last of them.

// answered is a full answer: option, who, when.
func answered(id, option string) seenfacts.Question {
	return seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: option, Text: "yes", AnsweredBy: "owner",
		AnsweredAt: "2026-10-14T12:00:00Z"}
}

// Readers of outside records that always return the given value.
func approvalIs(status string) func(context.Context) (seenfacts.ApprovalFact, bool) {
	return func(context.Context) (seenfacts.ApprovalFact, bool) {
		return seenfacts.ApprovalFact{Status: status, Action: "test", Tier: "test"}, true
	}
}

func questionIs(q seenfacts.Question) func(context.Context) (seenfacts.Question, bool) {
	return func(context.Context) (seenfacts.Question, bool) { return q, true }
}

// deployments is a fake delivery record: what each environment runs, or
// that it cannot be read.
type deployment struct {
	commit string
	ref    map[string]any
}

type deployments struct {
	mu sync.Mutex
	m  map[string]deployment
}

func (d *deployments) read(_ context.Context, env string) (string, map[string]any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	x, ok := d.m[env]
	return x.commit, x.ref, ok
}

func (w *world) evaluate(t *testing.T, step string, d *deployments) string {
	t.Helper()
	ctx := ctxOf(t)
	moved, err := planstore.EvaluateStep(ctx, w.rt, step, d.read)
	ok(t, err)
	return moved
}

// rule sets a node's done rule and binds approvals or questions to it, as an
// authorized plan change does.
func (w *world) rule(t *testing.T, node, rule string, binds ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, node, rule); err != nil {
			return err
		}
		for _, b := range binds {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id)
			  VALUES ($1, $2, $3, current_setting('agentrt.event_id')::bigint)`, node, b[0], b[1]); err != nil {
				return err
			}
		}
		return nil
	}))
}

// step adds a step of M-1 under the item, with a done rule when rule is set.
func (w *world) step(t *testing.T, id string, ordinal int, rule string, binds ...[2]string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: id, Parent: item, Kind: "step", Label: id, Title: id,
			MissionID: "M-1", StepKind: "agent", Ordinal: ordinal, Root: root})
		return err
	}))
	if rule != "" {
		w.rule(t, id, rule, binds...)
	}
}

func (w *world) stepState(t *testing.T, id string) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, id).Scan(&s))
	return s
}

// arch makes the ARCH root and its rows in order, each [id, rule] with its
// rule set by a plan change when there is one.
func (w *world) arch(t *testing.T, rows ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		for i, r := range rows {
			label := strings.TrimPrefix(r[0], "ARCH:")
			if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: r[0], Parent: "ARCH", Kind: "arch_row", Label: label,
				Title: label, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, r[0], r[1])
			return err
		}))
	}
}
