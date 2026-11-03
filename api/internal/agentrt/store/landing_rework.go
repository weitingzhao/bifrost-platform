package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// A root the Owner declined whose step has reworked work landed (R5:5.5-18,
// DESIGN-r5-5.5-18-declined-rework-2026-11-03): when every landed sibling's
// change differs from the root's (or cannot be compared), the controller
// records landing_rework_seen; a rework_landed card asks the Owner whether
// those siblings deliver the root's work, and only his count_replaced on
// the Console records landing_replaced. step_landed excuses the root while
// a replaced record's siblings are all still landed on the step
// (reworkExcuse). Same changes keep their own path (landing_delivered_by).
//
// Both are notes beside the protocol, like landing_delivered_by: the fold
// never reads them, so the declined root stays failed{declined} for every
// other reader.

// ReworkSibling is one landed sibling as landing_rework_seen names it: its
// task, its landed operation, the A that operation claimed, and how its
// change compared with the root's (different or not_comparable).
type ReworkSibling struct {
	By, ByOp, NewA, Why string
}

// ReworkSeen is a landing_rework_seen body.
type ReworkSeen struct {
	Root, Op, OldA, SetKey string
	Siblings               []ReworkSibling
}

// ErrReworkRefused: the stream does not take the record now; nothing was
// written.
var ErrReworkRefused = errors.New("landing rework record refused")

var reworkSeenFields = []string{"v", "root", "op", "old_a", "set_key", "siblings"}
var reworkReplacedFields = []string{"v", "root", "op", "trigger_seq", "question_id", "set_key"}
var reworkWhys = []string{"different", "not_comparable"}

// ReworkSetKey is the identity of a sibling set: the sha256 of its
// (by, by_op, new_a) triples in by order. why is not part of it.
func ReworkSetKey(sibs []ReworkSibling) string {
	s := slices.Clone(sibs)
	slices.SortFunc(s, func(a, b ReworkSibling) int { return strings.Compare(a.By, b.By) })
	list := make([]any, 0, len(s))
	for _, x := range s {
		list = append(list, []any{x.By, x.ByOp, x.NewA})
	}
	sum, _ := canon.Sum(map[string]any{"rework_set": list})
	return strings.TrimPrefix(sum, "sha256:")
}

func (r ReworkSeen) body() map[string]any {
	sibs := make([]any, 0, len(r.Siblings))
	for _, x := range r.Siblings {
		sibs = append(sibs, map[string]any{"by": x.By, "by_op": x.ByOp, "new_a": x.NewA, "why": x.Why})
	}
	return map[string]any{"v": "1", "root": r.Root, "op": r.Op, "old_a": r.OldA, "set_key": r.SetKey, "siblings": sibs}
}

func reworkSeenOf(m map[string]any) ReworkSeen {
	s := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	r := ReworkSeen{Root: s(m, "root"), Op: s(m, "op"), OldA: s(m, "old_a"), SetKey: s(m, "set_key")}
	list, _ := m["siblings"].([]any)
	for _, x := range list {
		if o, ok := x.(map[string]any); ok {
			r.Siblings = append(r.Siblings, ReworkSibling{By: s(o, "by"), ByOp: s(o, "by_op"), NewA: s(o, "new_a"), Why: s(o, "why")})
		}
	}
	return r
}

// exactKeys: m has exactly these keys.
func exactKeys(m map[string]any, keys []string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// badLandingReworkSeen is why a landing_rework_seen body is refused, or "".
func badLandingReworkSeen(v any) string {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, reworkSeenFields) || m["v"] != "1" {
		return "landing_rework_seen is a v1 object of " + strings.Join(reworkSeenFields, ", ")
	}
	r := reworkSeenOf(m)
	switch {
	case !landingRoot.MatchString(r.Root):
		return "landing_rework_seen root is a task"
	case r.Op == "":
		return "landing_rework_seen needs op"
	case !landingSHA.MatchString(r.OldA):
		return "landing_rework_seen old_a is a commit"
	case !diffDigest.MatchString(r.SetKey):
		return "landing_rework_seen set_key is a digest"
	}
	list, _ := m["siblings"].([]any)
	if len(list) == 0 || len(list) != len(r.Siblings) {
		return "landing_rework_seen names at least one sibling, each an object"
	}
	for i, x := range r.Siblings {
		o := list[i].(map[string]any)
		switch {
		case !exactKeys(o, []string{"by", "by_op", "new_a", "why"}):
			return "landing_rework_seen sibling is an object of by, by_op, new_a, why"
		case !landingRoot.MatchString(x.By) || x.By == r.Root:
			return "landing_rework_seen sibling by is another task"
		case x.ByOp == "" || !landingSHA.MatchString(x.NewA):
			return "landing_rework_seen sibling needs by_op and new_a"
		case !slices.Contains(reworkWhys, x.Why):
			return "landing_rework_seen sibling why is different or not_comparable"
		case i > 0 && strings.Compare(r.Siblings[i-1].By, x.By) >= 0:
			return "landing_rework_seen siblings are in by order, each once"
		}
	}
	if ReworkSetKey(r.Siblings) != r.SetKey {
		return "landing_rework_seen set_key is not its siblings'"
	}
	return ""
}

// badLandingReplaced is why a landing_replaced body is refused, or "".
func badLandingReplaced(v any) string {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, reworkReplacedFields) || m["v"] != "1" {
		return "landing_replaced is a v1 object of " + strings.Join(reworkReplacedFields, ", ")
	}
	root, _ := m["root"].(string)
	op, _ := m["op"].(string)
	q, _ := m["question_id"].(string)
	key, _ := m["set_key"].(string)
	seq, okSeq := cardNumber(m["trigger_seq"])
	switch {
	case !landingRoot.MatchString(root) || op == "" || q == "":
		return "landing_replaced needs root, op and question_id"
	case !okSeq || seq < 1:
		return "landing_replaced needs trigger_seq"
	case !diffDigest.MatchString(key):
		return "landing_replaced set_key is a digest"
	}
	return ""
}

// onlyBusinessKey: the body is exactly text and name, so the note is no
// other reader's record as well.
func onlyBusinessKey(body map[string]any, name string) bool {
	_, hasText := body["text"]
	_, hasName := body[name]
	return len(body) == 2 && hasText && hasName
}

type reworkTask struct{ step, repo, role, state, effect string }

func readReworkTask(ctx context.Context, tx pgx.Tx, id string) (reworkTask, error) {
	var t reworkTask
	err := tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id, ''), COALESCE(repo, ''), role, state, COALESCE(effect_class, '')
	  FROM agentrt.task WHERE task_id = $1`, id).Scan(&t.step, &t.repo, &t.role, &t.state, &t.effect)
	return t, err
}

// reworkStepOpen: the step is neither done, dropped nor stopped. A closed
// step is not reopened by step evidence (facts.go evaluateStep), so nothing
// about its declined roots is recorded or asked (§2.4).
func reworkStepOpen(ctx context.Context, tx pgx.Tx, step string) (bool, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT COALESCE(state, '') FROM agentrt.plan_node WHERE plan_node_id = $1`, step).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !slices.Contains([]string{"done", "dropped", "stopped"}, state), nil
}

func (t reworkTask) implementation() bool {
	return t.role == "implement" && t.state == "accepted" && t.effect == "branch"
}

// landedSiblings are, by task, the accepted implementations that write code
// on step in repo, other than root, whose latest operation on s landed: the
// operation and the A it claimed (Why empty).
func landedSiblings(ctx context.Context, tx pgx.Tx, s landing.State, repo, step, root string) ([]ReworkSibling, error) {
	rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE plan_node_id = $1 AND repo = $2 AND task_id <> $3
	  AND role = 'implement' AND state = 'accepted' AND effect_class = 'branch' ORDER BY task_id`, step, repo, root)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var out []ReworkSibling
	for _, id := range ids {
		op := s.LatestOperation(id)
		if o, ok := s.Operations[op]; ok && o.Phase == "landed" {
			out = append(out, ReworkSibling{By: id, ByOp: op, NewA: o.A})
		}
	}
	return out, nil
}

// sameSet: a and b name the same (by, by_op, new_a) triples.
func sameSet(a, b []ReworkSibling) bool {
	return len(a) == len(b) && ReworkSetKey(a) == ReworkSetKey(b)
}

// checkLandingReworkSeenAppend checks the record when it is written, under
// the stream lock Append holds (§3.1): the controller on the repository
// stream under its root; the root an accepted implementation that writes
// code, on a step, of the stream's repository, its latest operation op
// failed{declined} with A old_a; the siblings exactly the step's landed
// siblings in the repository now, each at its landed latest operation and
// claimed A; no record yet for (root, op, set_key). The task rows are not
// locked and may move after, which is why step_landed checks again.
func checkLandingReworkSeenAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, ok := ev.Body["landing_rework_seen"]
	if !ok {
		return nil
	}
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrReworkRefused, why) }
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID != m["root"] {
		return refuse("it is written by the controller on the repository stream under its root")
	}
	if !onlyBusinessKey(ev.Body, "landing_rework_seen") {
		return refuse("its body is its text and this record alone")
	}
	r := reworkSeenOf(m)
	repo := strings.TrimPrefix(ev.Stream, "landing:")
	root, err := readReworkTask(ctx, tx, r.Root)
	if errors.Is(err, pgx.ErrNoRows) {
		return refuse("root is not a task")
	}
	if err != nil {
		return err
	}
	if !root.implementation() || root.step == "" || root.repo != repo {
		return refuse("root is not an accepted implementation that writes code, on a step, of the stream's repository")
	}
	// The step's row is not locked here: a step closing at this moment
	// may still get the record; nothing is asked for a closed step
	// (reworkTrigger) and step evidence does not read a closed one.
	if open, err := reworkStepOpen(ctx, tx, root.step); err != nil || !open {
		if err != nil {
			return err
		}
		return refuse("root's step is closed")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	o, has := s.Operations[r.Op]
	switch {
	case !has || o.Task != r.Root || s.LatestOperation(r.Root) != r.Op || o.Phase != "failed" || o.Outcome != "declined":
		return refuse("root's latest operation is not op, failed{declined}")
	case o.A != r.OldA:
		return refuse("old_a is not the A op claimed")
	}
	now, err := landedSiblings(ctx, tx, s, repo, root.step, r.Root)
	if err != nil {
		return err
	}
	if !sameSet(now, r.Siblings) {
		return refuse("siblings are not the step's landed siblings now")
	}
	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND task_id = $2 AND event_type = 'note'
	  AND body_json->'landing_rework_seen'->>'op' = $3 AND body_json->'landing_rework_seen'->>'set_key' = $4)`,
		ev.Stream, r.Root, r.Op, r.SetKey).Scan(&dup); err != nil {
		return err
	}
	if dup {
		return refuse("the set is already recorded for root and op")
	}
	return nil
}

// reworkSeenAt is the landing_rework_seen at seq on stream, under root.
func reworkSeenAt(ctx context.Context, tx pgx.Tx, stream, root string, seq int64) (ReworkSeen, bool, error) {
	var m map[string]any
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_rework_seen' FROM agentrt.event WHERE event_stream_id = $1 AND seq = $2
	  AND task_id = $3 AND event_type = 'note' AND body_json ? 'landing_rework_seen'`, stream, seq, root).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReworkSeen{}, false, nil
	}
	if err != nil {
		return ReworkSeen{}, false, err
	}
	return reworkSeenOf(m), true, nil
}

// reworkSeenFor is the seq of the landing_rework_seen for (root, op,
// set_key) on stream, 0 for none (§3.1: looked up by its set, never "the
// latest").
func reworkSeenFor(ctx context.Context, tx pgx.Tx, stream, root, op, key string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT seq FROM agentrt.event WHERE event_stream_id = $1 AND task_id = $2 AND event_type = 'note'
	  AND body_json->'landing_rework_seen'->>'op' = $3 AND body_json->'landing_rework_seen'->>'set_key' = $4 ORDER BY seq LIMIT 1), 0)`,
		stream, root, op, key).Scan(&seq)
	return seq, err
}

// checkLandingReplacedAppend checks the record when it is written (§3.2):
// the controller on the repository stream under its root; trigger_seq is
// this stream's landing_rework_seen of the same root, op and set_key; the
// stream already holds this question's landing_card_outcome, written by the
// controller: kind rework_landed, choice count_replaced, applied /
// counted_replaced, on the Console, same root, op and trigger; and no other
// landing_replaced for the question. Only that answer authorizes it. The
// check sees that the outcome exists; that both are one transaction is the
// handler's (HandleLandingCardAnswer).
func checkLandingReplacedAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, ok := ev.Body["landing_replaced"]
	if !ok {
		return nil
	}
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrReworkRefused, why) }
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID != m["root"] {
		return refuse("it is written by the controller on the repository stream under its root")
	}
	if !onlyBusinessKey(ev.Body, "landing_replaced") {
		return refuse("its body is its text and this record alone")
	}
	root, op, q, key := fmt.Sprint(m["root"]), fmt.Sprint(m["op"]), fmt.Sprint(m["question_id"]), fmt.Sprint(m["set_key"])
	seq, _ := cardNumber(m["trigger_seq"])
	seen, found, err := reworkSeenAt(ctx, tx, ev.Stream, root, seq)
	if err != nil {
		return err
	}
	if !found || seen.Root != root || seen.Op != op || seen.SetKey != key {
		return refuse("trigger_seq is not this root's landing_rework_seen for op and set_key")
	}
	var o map[string]any
	var actor string
	err = tx.QueryRow(ctx, `SELECT body_json->'landing_card_outcome', actor_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_card_outcome'->>'question_id' = $2 ORDER BY seq LIMIT 1`, ev.Stream, q).Scan(&o, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return refuse("it follows the Owner's count_replaced on this card")
	}
	if err != nil {
		return err
	}
	oseq, _ := cardNumber(o["trigger_seq"])
	if actor != "controller" || o["kind"] != CardReworkLanded || o["choice"] != "count_replaced" || o["outcome"] != "applied" ||
		o["why"] != "counted_replaced" || o["channel"] != "console" || o["root"] != root || o["op"] != op || oseq != seq {
		return refuse("it follows the Owner's count_replaced on this card")
	}
	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND task_id = $2 AND event_type = 'note'
	  AND body_json->'landing_replaced'->>'question_id' = $3)`, ev.Stream, root, q).Scan(&dup); err != nil {
		return err
	}
	if dup {
		return refuse("the question already has its landing_replaced")
	}
	return nil
}

// RecordReworkSeen writes landing_rework_seen for root's latest operation op
// on repo's stream, or finds it (§3.1): under the stream lock it looks the
// set up and returns its seq; otherwise it reads the facts again (root's
// latest operation, the step's landed siblings, an excuse in force) and
// writes only when they match what the caller compared: "stale" and seq 0
// when they do not. sibs carry each one's why.
func RecordReworkSeen(ctx context.Context, db DB, repo, root, op, oldA string, sibs []ReworkSibling) (int64, string, error) {
	stream := "landing:" + repo
	s := slices.Clone(sibs)
	slices.SortFunc(s, func(a, b ReworkSibling) int { return strings.Compare(a.By, b.By) })
	r := ReworkSeen{Root: root, Op: op, OldA: oldA, SetKey: ReworkSetKey(s), Siblings: s}
	var seq int64
	var status string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		seq, status = 0, ""
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		got, err := reworkSeenFor(ctx, tx, stream, root, op, r.SetKey)
		if err != nil {
			return err
		}
		if got > 0 {
			seq, status = got, "found"
			return nil
		}
		t, err := readReworkTask(ctx, tx, root)
		if err != nil {
			return err
		}
		st, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, has := st.Operations[op]
		if !t.implementation() || t.step == "" || !has || st.LatestOperation(root) != op || o.Phase != "failed" || o.Outcome != "declined" || o.A != oldA {
			status = "stale"
			return nil
		}
		if open, err := reworkStepOpen(ctx, tx, t.step); err != nil || !open {
			status = "closed"
			return err
		}
		now, err := landedSiblings(ctx, tx, st, repo, t.step, root)
		if err != nil {
			return err
		}
		if !sameSet(now, s) {
			status = "stale"
			return nil
		}
		// What would excuse or end the root without a card, read again
		// under the lock: the same change landed, the Owner disposed of
		// it, or he counted it delivered.
		by, err := landingDeliveredBy(ctx, tx, stream, root, op)
		if err != nil {
			return err
		}
		disposed, err := landingDisposed(ctx, tx, stream, root, op)
		if err != nil {
			return err
		}
		excused, err := reworkExcuse(ctx, tx, stream, root, op, now)
		if err != nil {
			return err
		}
		if excused || disposed != "" || slices.ContainsFunc(now, func(x ReworkSibling) bool { return x.By == by }) {
			status = "stale"
			return nil
		}
		hook("rework-seen-writing", root)
		a, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
			Body: map[string]any{"text": "Landing: the declined " + root + " has reworked work landed on its step, every change different", "landing_rework_seen": r.body()}})
		if err != nil {
			return err
		}
		seq, status = a.Seq, "written"
		return nil
	})
	return seq, status, err
}

// reworkExcuse: a landing_replaced for root's operation op on stream whose
// siblings (read from its landing_rework_seen) are all in landed now, each
// at the same operation and A (§4). The one judgment the candidates, the
// card trigger, the answer and step_landed share.
func reworkExcuse(ctx context.Context, tx pgx.Tx, stream, root, op string, landed []ReworkSibling) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT (body_json->'landing_replaced'->>'trigger_seq')::bigint FROM agentrt.event
	  WHERE event_stream_id = $1 AND task_id = $2 AND event_type = 'note' AND body_json->'landing_replaced'->>'op' = $3 ORDER BY seq`, stream, root, op)
	if err != nil {
		return false, err
	}
	seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return false, err
	}
	have := map[string]ReworkSibling{}
	for _, x := range landed {
		have[x.By] = x
	}
	for _, seq := range seqs {
		seen, found, err := reworkSeenAt(ctx, tx, stream, root, seq)
		if err != nil {
			return false, err
		}
		if !found || seen.Op != op || len(seen.Siblings) == 0 {
			continue
		}
		all := true
		for _, x := range seen.Siblings {
			if h, ok := have[x.By]; !ok || h.ByOp != x.ByOp || h.NewA != x.NewA {
				all = false
				break
			}
		}
		if all {
			return true, nil
		}
	}
	return false, nil
}

// ReworkView is what a rework_landed card shows and binds: the root's old A
// and the landing_rework_seen's siblings, each with its task title.
type ReworkView struct {
	OldA, SetKey string
	Siblings     []ReworkSiblingView
}

// ReworkSiblingView is one sibling with its title's first line.
type ReworkSiblingView struct {
	ReworkSibling
	Title string
}

// reworkTrigger is root's rework_landed trigger now, if it has one (§6.1):
// root an accepted implementation that writes code on a step of repo, its
// latest operation op declined; landed siblings on the step; no
// landing_delivered_by naming one of them and no excuse in force; and a
// landing_rework_seen for exactly that set, whose seq is the trigger's.
// at is when op was declined.
func reworkTrigger(ctx context.Context, tx pgx.Tx, s landing.State, repo, root, op string, at time.Time) (CardTrigger, bool, error) {
	stream := "landing:" + repo
	t, err := readReworkTask(ctx, tx, root)
	if err != nil || !t.implementation() || t.step == "" || t.repo != repo {
		return CardTrigger{}, false, err
	}
	if open, err := reworkStepOpen(ctx, tx, t.step); err != nil || !open {
		return CardTrigger{}, false, err
	}
	landed, err := landedSiblings(ctx, tx, s, repo, t.step, root)
	if err != nil || len(landed) == 0 {
		return CardTrigger{}, false, err
	}
	by, err := landingDeliveredBy(ctx, tx, stream, root, op)
	if err != nil {
		return CardTrigger{}, false, err
	}
	for _, x := range landed {
		if x.By == by {
			return CardTrigger{}, false, nil
		}
	}
	if ok, err := reworkExcuse(ctx, tx, stream, root, op, landed); err != nil || ok {
		return CardTrigger{}, false, err
	}
	seq, err := reworkSeenFor(ctx, tx, stream, root, op, ReworkSetKey(landed))
	if err != nil || seq == 0 {
		return CardTrigger{}, false, err
	}
	seen, found, err := reworkSeenAt(ctx, tx, stream, root, seq)
	if err != nil || !found {
		return CardTrigger{}, false, err
	}
	view := &ReworkView{OldA: seen.OldA, SetKey: seen.SetKey}
	for _, x := range seen.Siblings {
		var title string
		if err := tx.QueryRow(ctx, `SELECT title FROM agentrt.task WHERE task_id = $1`, x.By).Scan(&title); err != nil {
			return CardTrigger{}, false, err
		}
		view.Siblings = append(view.Siblings, ReworkSiblingView{ReworkSibling: x, Title: firstLine(title)})
	}
	tr := CardTrigger{Kind: CardReworkLanded, Seq: seq, At: at, Root: root, Op: op, Why: "declined",
		Choices: []string{"count_replaced", "still_owed", "wait"}, Rework: view}
	if _, fits := reworkCardBody(repo, tr); !fits {
		return CardTrigger{}, false, nil
	}
	return tr, true, nil
}

// ReworkBodyLimit is the most a rework_landed card's body may take: the
// question store refuses a body over 3000 bytes (questions/types.go), and
// a prepared request is asked again as it is, so it must fit when prepared.
const ReworkBodyLimit = 2900

// reworkTitleBytes bounds each sibling's title in the full form.
const reworkTitleBytes = 80

// reworkCardBody is a rework_landed card's text, from its frozen trigger
// alone, and whether it fits ReworkBodyLimit. Every sibling is named, always
// by task and short SHA: with titles (each cut to reworkTitleBytes) when
// that fits, else without; when even that does not fit, the trigger is not
// asked (§6.2: never a card naming fewer siblings than the set it counts).
func reworkCardBody(repo string, tr CardTrigger) (string, bool) {
	head := fmt.Sprintf("Repository %s: the landing of this task (operation %s) was declined at %s.", repo, tr.Op, tr.At.UTC().Format(time.RFC3339))
	if tr.Rework == nil {
		return head, true
	}
	tail := " If they deliver its work, count it delivered: the step no longer waits on it while they stay landed. If not, its work is still owed and the step stays open."
	build := func(titles bool) string {
		var b strings.Builder
		b.WriteString(head)
		fmt.Fprintf(&b, " Since then these %d tasks of its step landed, each with a change that is not the same as its own (%s):", len(tr.Rework.Siblings), shortSHA(tr.Rework.OldA))
		for _, x := range tr.Rework.Siblings {
			how := "different"
			if x.Why == "not_comparable" {
				how = "not comparable"
			}
			if titles {
				fmt.Fprintf(&b, " %s “%s” (%s, %s);", x.By, cutBytes(x.Title, reworkTitleBytes), shortSHA(x.NewA), how)
			} else {
				fmt.Fprintf(&b, " %s (%s, %s);", x.By, shortSHA(x.NewA), how)
			}
		}
		b.WriteString(tail)
		return b.String()
	}
	if s := build(true); len(s) <= ReworkBodyLimit {
		return s, true
	}
	s := build(false)
	return s, len(s) <= ReworkBodyLimit
}

func shortSHA(a string) string {
	if len(a) > 8 {
		return a[:8]
	}
	return a
}

// checkReworkCardPrepared: a prepared rework_landed card binds to its
// root's landing_rework_seen at trigger_seq for the same operation, on the
// repository's landing stream (§6.3).
func checkReworkCardPrepared(ctx context.Context, tx pgx.Tx, body map[string]any) error {
	repo, root, op := fmt.Sprint(body["repo"]), fmt.Sprint(body["root"]), fmt.Sprint(body["op"])
	seq, ok := cardNumber(body["trigger_seq"])
	if !ok || !landingRoot.MatchString(root) || op == "" {
		return fmt.Errorf("a rework_landed card names its root, operation and trigger")
	}
	seen, found, err := reworkSeenAt(ctx, tx, "landing:"+repo, root, seq)
	if err != nil {
		return err
	}
	if !found || seen.Op != op {
		return fmt.Errorf("a rework_landed card's trigger is its root's landing_rework_seen for the operation")
	}
	return nil
}

// recordReplaced writes landing_replaced for card c's question, after its
// applied count_replaced outcome, in the handler's transaction (§6.4).
func recordReplaced(ctx context.Context, tx pgx.Tx, stream string, c CardRecord, mission, question string) error {
	seen, found, err := reworkSeenAt(ctx, tx, stream, c.Root, c.TriggerSeq)
	if err != nil {
		return err
	}
	if !found || seen.Op != c.Op {
		return fmt.Errorf("%w: the card's trigger is not its root's landing_rework_seen", ErrReworkRefused)
	}
	_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: c.Root, MissionID: mission,
		Body: map[string]any{"text": "Landing: the Owner counted the declined " + c.Root + " delivered by its step's reworked work",
			"landing_replaced": map[string]any{"v": "1", "root": c.Root, "op": c.Op, "trigger_seq": c.TriggerSeq, "question_id": question, "set_key": seen.SetKey}}})
	return err
}
