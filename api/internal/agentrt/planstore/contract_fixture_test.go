package planstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The fixture of the plan domain's contract tests (doc.go). It reaches the
// domain the way its callers do: a plan exists because an import was proposed
// and the Owner's card answered, and it changes through proposals. Only the
// names other packages' production code uses are called; what the contract
// fixes as a value and exports under a name nobody else uses (the key of a
// card's first option, the plan's stream, the longest note) is written here
// as the value, so a change of it fails here.
//
// The ports are the ones this test binary has: planobservations_ledger_test.go
// imports store, whose init binds all six to their real providers. Nothing
// here binds one (a binding is process-wide and these tests run in parallel).

const (
	optionApply  = "a" // the key of a card's first option
	optionRefuse = "b"
	ownerActor   = "owner"
	maxNoteBytes = 8 << 10

	planRoot = "R2"
	item1    = "R2:2.1" // has an active step
	item2    = "R2:2.2" // has none
	stepLive = "R2:2.1-1"
	stepA    = "R2:2.1-2"
	stepB    = "R2:2.1-3"
	stepDone = "R2:2.1-4"
	stepC    = "R2:2.2-1"
	archRoot = "ARCH"
	archRowA = "ARCH:release-guard"
	archRowB = "ARCH:state-db"
)

// contractLedger is the piece of the work ledger the plans come from.
const contractLedger = `- **第 1 轮（现在）**：护栏。
    - **架构对照（设计 v4 → 现在，核对于 2026-10-19 01:20）**。原则。
      - [x] 发布护栏｜在正式环境
      - [~] 状态库｜两个环境都在用
    - **第 2 轮逐项（核对于 2026-10-19 01:20）**。说明。
      - 2.1 数据层
        - [~] 迁移器
        - [ ] 第二步
        - [ ] 第三步
        - [x] 已做完的一步｜10-14 01:09 落地
      - 2.2 第二项
        - [ ] 还没开始
`

var contractCtx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantIs(t *testing.T, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: err = %v; want %v", what, err, target)
	}
}

// seedContract writes the two actors every plan write rests on: the
// controller, which decides and applies, and the Owner. Threads register
// themselves at their first write.
func seedContract(ctx context.Context, owner *pgx.Conn) error {
	return eventlog.InTx(ctx, owner, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": []string{"controller", ownerActor}, "note": "contract fixture"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
		  VALUES ('controller', 'controller', 'Controller'), ('owner', 'owner', 'Owner')`)
		return err
	})
}

// cardDesk stands for the Owner's question store: it keeps the cards it is
// asked, gives one question per card key, and holds the answers.
type cardDesk struct {
	mu      sync.Mutex
	cards   []planstore.Card
	byKey   map[string]string
	answers map[string]seenfacts.Question
	acked   map[string]int
}

func newDesk() *cardDesk {
	return &cardDesk{byKey: map[string]string{}, answers: map[string]seenfacts.Question{}, acked: map[string]int{}}
}

func (d *cardDesk) ask(_ context.Context, c planstore.Card) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id, ok := d.byKey[c.Key]; ok {
		return id, nil
	}
	d.cards = append(d.cards, c)
	id := fmt.Sprintf("q_%d", len(d.cards))
	d.byKey[c.Key] = id
	return id, nil
}

func (d *cardDesk) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.cards)
}

func (d *cardDesk) last() planstore.Card {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cards[len(d.cards)-1]
}

func (d *cardDesk) answer(id, option string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.answers[id] = seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: option, AnsweredBy: ownerActor, AnsweredAt: "2026-11-18T00:00:00Z"}
}

func (d *cardDesk) close(id, status string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.answers[id] = seenfacts.Question{ID: id, Status: status}
}

func (d *cardDesk) question(_ context.Context, id string) (seenfacts.Question, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if q, ok := d.answers[id]; ok {
		return q, true
	}
	return seenfacts.Question{ID: id, Status: "open"}, true
}

func (d *cardDesk) ack(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.acked[id]++
	return nil
}

func (d *cardDesk) ackCount(id string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.acked[id]
}

// planWorld is a database with no plan yet, or with the plans a test imported.
type planWorld struct {
	t    *testing.T
	db   *agentrttest.DB
	rt   *pgxpool.Pool
	desk *cardDesk
}

var writer = actor.Writer{Actor: "thread:contract", Token: "operator"}

func emptyWorld(t *testing.T) *planWorld {
	t.Helper()
	db := agentrttest.MigratedWith(t, "plan-contract", seedContract)
	return &planWorld{t: t, db: db, rt: db.Pool(t, db.Runtime), desk: newDesk()}
}

// newPlanWorld has round 2 imported, through its card:
//
//	R2
//	  R2:2.1  -1 active, -2 and -3 planned, -4 done
//	  R2:2.2  -1 planned
func newPlanWorld(t *testing.T) *planWorld {
	t.Helper()
	w := emptyWorld(t)
	w.importAndApply("import-round", roundRequest(t))
	return w
}

func roundRequest(t *testing.T) planimport.Request {
	t.Helper()
	req, err := planimport.ParseLedger(contractLedger, 2, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	return req
}

func archRequest(t *testing.T, rule string) planimport.Request {
	t.Helper()
	rows := []planimport.ArchRow{{Label: "release-guard", Rule: []byte(rule)}, {Label: "state-db"}}
	req, err := planimport.ParseArch(contractLedger, rows, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	return req
}

func (w *planWorld) importAndApply(key string, req planimport.Request) {
	w.t.Helper()
	out, err := planstore.ProposePlanImport(contractCtx, w.rt, writer, key, "bring it in", req, w.desk.ask)
	must(w.t, err)
	w.applyCard(out)
}

// questionOf is the card a proposal waits on.
func (w *planWorld) questionOf(out map[string]any) string {
	w.t.Helper()
	q, _ := out["question_id"].(string)
	if q == "" {
		w.t.Fatalf("the proposal waits on no card: %v", out)
	}
	return q
}

// applyCard answers the proposal's card with its first option and runs the
// decision pass.
func (w *planWorld) applyCard(out map[string]any) {
	w.t.Helper()
	w.desk.answer(w.questionOf(out), optionApply)
	w.decide()
}

func (w *planWorld) decide() int {
	w.t.Helper()
	n, err := planstore.DecidePlanChanges(contractCtx, w.rt, w.desk.question, w.desk.ack)
	must(w.t, err)
	return n
}

func (w *planWorld) propose(key string, changes ...planstore.PlanChange) (map[string]any, error) {
	return planstore.ProposePlanChange(contractCtx, w.rt, writer, key, planRoot, "contract test", changes, w.desk.ask)
}

// land proposes a change outside the standing authorization and applies it.
func (w *planWorld) land(key string, changes ...planstore.PlanChange) {
	w.t.Helper()
	out, err := w.propose(key, changes...)
	must(w.t, err)
	w.applyCard(out)
	if got := w.decisionOf(out); got != "applied" {
		w.t.Fatalf("%s: decided %q: %s", key, got, w.decisionError(out))
	}
}

func (w *planWorld) scalar(dest any, q string, args ...any) {
	w.t.Helper()
	must(w.t, w.rt.QueryRow(contractCtx, q, args...).Scan(dest))
}

func (w *planWorld) count(q string, args ...any) int {
	w.t.Helper()
	var n int
	w.scalar(&n, q, args...)
	return n
}

// events counts the events of a type on a stream.
func (w *planWorld) events(stream, typ string) int {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = $2`, stream, typ)
}

func (w *planWorld) decisionOf(out map[string]any) string {
	w.t.Helper()
	var d string
	err := w.rt.QueryRow(contractCtx, `SELECT body_json->>'decision' FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND (body_json->>'proposal_event_id')::bigint = $1`, out["proposal_event_id"]).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	must(w.t, err)
	return d
}

func (w *planWorld) decisionError(out map[string]any) string {
	w.t.Helper()
	var e string
	w.scalar(&e, `SELECT COALESCE(body_json->>'error', '') FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND (body_json->>'proposal_event_id')::bigint = $1`, out["proposal_event_id"])
	return e
}

// node is a plan node as it is stored.
type node struct{ kind, title, state, rule, mission string }

func (w *planWorld) node(id string) node {
	w.t.Helper()
	var n node
	must(w.t, w.rt.QueryRow(contractCtx, `SELECT node_kind, title, COALESCE(state, ''), done_rule_json::text, COALESCE(mission_id, '')
	  FROM agentrt.plan_node WHERE plan_node_id = $1`, id).Scan(&n.kind, &n.title, &n.state, &n.rule, &n.mission))
	return n
}

func (w *planWorld) hasNode(id string) bool {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = $1`, id) == 1
}

// footprint is everything a write could leave: actors, the ledger and its
// stream heads, keys, artifacts, and every plan node as it stands.
func (w *planWorld) footprint() string {
	w.t.Helper()
	var s string
	w.scalar(&s, `SELECT concat_ws(' | ',
	  (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event),
	  (SELECT string_agg(event_stream_id || '=' || last_seq, ',' ORDER BY event_stream_id) FROM agentrt.event_stream),
	  (SELECT count(*) FROM agentrt.idempotency_key), (SELECT count(*) FROM agentrt.artifact),
	  (SELECT count(*) FROM agentrt.plan_node_binding),
	  (SELECT md5(string_agg(n::text, ',' ORDER BY plan_node_id)) FROM agentrt.plan_node n))`)
	return s
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// evaluate runs the controller's evaluation of a step; no rule here reads a
// deployment.
func (w *planWorld) evaluate(step string) string {
	w.t.Helper()
	to, err := planstore.EvaluateStep(contractCtx, w.rt, step, nil)
	must(w.t, err)
	return to
}

func (w *planWorld) artifact(kind, body string) string {
	w.t.Helper()
	var id string
	must(w.t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = artifact.PutArtifact(contractCtx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

// artifactID is the id an artifact of these bytes is stored under.
func artifactID(doc []byte) string {
	sum := sha256.Sum256(doc)
	return "sha256:" + hex.EncodeToString(sum[:])
}
