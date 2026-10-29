package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// B4: closing a mission from landing evidence (S3, CUT §11.8 v2/v3). A
// mission with a root in the live landing scope is closed only when every
// such root is delivered or disposed, classified one class per root by a
// priority table; a delivery needs main evidence read in the mission's
// current closing round and the rule in force at the decision explicitly
// stg_release none. No timer releases it.

// Root classes.
const (
	RootDelivered  = "delivered"
	RootDisposed   = "disposed"
	RootUnresolved = "unresolved"
)

// CloseRoundFor is how long a closing round's evidence counts.
const CloseRoundFor = 30 * time.Minute

// LandingRootClass is one root's class and why.
type LandingRootClass struct {
	Root, Op, Class, Reason string
	// NeedsEvidence: a delivery that lacks only main evidence read in the
	// current closing round.
	NeedsEvidence bool
	// key is the root's identity in a closing round's snapshot: what it is
	// before this round's evidence (review 672 B-01). read: a round reads
	// its operation's main.
	key  string
	read bool
	// evidenceAt: when the main evidence a delivery rests on was read (a
	// delivery in the closing round); zero otherwise (review 675 N-01).
	evidenceAt time.Time
}

// closeSnapshot is the mission's closing-round snapshot: every in-scope
// root's identity, sorted; and the operations a round reads.
func closeSnapshot(roots []LandingRootClass) (snapshot, ops []string) {
	for _, r := range roots {
		snapshot = append(snapshot, r.key)
		if r.read {
			ops = append(ops, r.Op)
		}
	}
	sort.Strings(snapshot)
	sort.Strings(ops)
	return snapshot, ops
}

// closeRound is a mission's closing round (landing_close_round). A v2 round
// fixes the snapshot of every in-scope root it was opened on: evidence for
// some of its operations only narrows what is left to read; it is reopened
// only when the snapshot changes or its time ends (review 672 B-01). A v1
// round (no snapshot) never counts.
type closeRound struct {
	Round     int64
	At, Until time.Time
	Ops       []string
	Snapshot  []string
}

// validFor: the round counts for a decision at at on this snapshot.
func (r *closeRound) validFor(snapshot []string, at time.Time) bool {
	return r != nil && len(r.Snapshot) > 0 && at.Before(r.Until) && slices.Equal(r.Snapshot, snapshot)
}

// readCloseRound is the mission's latest closing round, or nil.
func readCloseRound(ctx context.Context, tx pgx.Tx, mission string) (*closeRound, error) {
	var m map[string]any
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_close_round' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json ? 'landing_close_round' ORDER BY seq DESC LIMIT 1`, MissionStream(mission)).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &closeRound{}
	r.Round, _ = cardNumber(m["round"])
	r.At, _ = time.Parse(time.RFC3339Nano, fmt.Sprint(m["at"]))
	r.Until, _ = time.Parse(time.RFC3339Nano, fmt.Sprint(m["until"]))
	if ops, ok := m["ops"].([]any); ok {
		for _, o := range ops {
			r.Ops = append(r.Ops, fmt.Sprint(o))
		}
	}
	if m["v"] == "2" {
		if snap, ok := m["snapshot"].([]any); ok {
			for _, k := range snap {
				r.Snapshot = append(r.Snapshot, fmt.Sprint(k))
			}
		}
	}
	return r, nil
}

// badCloseRound is why a landing_close_round body is refused, or "".
func badCloseRound(v any) string {
	m, ok := v.(map[string]any)
	if !ok || (m["v"] != "1" && m["v"] != "2") {
		return "landing_close_round is not a v1 or v2 object"
	}
	if m["v"] == "2" && !nonEmptyStrings(m["snapshot"]) {
		return "landing_close_round v2 needs snapshot"
	}
	if n, ok := cardNumber(m["round"]); !ok || n < 1 {
		return "landing_close_round needs round"
	}
	at, err1 := time.Parse(time.RFC3339Nano, fmt.Sprint(m["at"]))
	until, err2 := time.Parse(time.RFC3339Nano, fmt.Sprint(m["until"]))
	if err1 != nil || err2 != nil || !until.After(at) {
		return "landing_close_round needs at before until"
	}
	switch ops := m["ops"].(type) {
	case []any:
		if len(ops) == 0 {
			return "landing_close_round needs ops"
		}
	case []string:
		if len(ops) == 0 {
			return "landing_close_round needs ops"
		}
	default:
		return "landing_close_round needs ops"
	}
	return ""
}

// nonEmptyStrings: a non-empty list of non-empty strings, decoded or not.
func nonEmptyStrings(v any) bool {
	var xs []string
	switch t := v.(type) {
	case []string:
		xs = t
	case []any:
		for _, x := range t {
			s, ok := x.(string)
			if !ok {
				return false
			}
			xs = append(xs, s)
		}
	default:
		return false
	}
	if len(xs) == 0 {
		return false
	}
	for _, x := range xs {
		if x == "" {
			return false
		}
	}
	return true
}

// checkCloseRoundAppend: the controller writes landing_close_round and
// landing_close on a mission's stream.
func checkCloseRoundAppend(ev Event) error {
	for _, k := range []string{"landing_close_round", "landing_close"} {
		if _, ok := ev.Body[k]; !ok {
			continue
		}
		if ev.Type != "note" || ev.Actor != "controller" || !strings.HasPrefix(ev.Stream, "mission:") {
			return fmt.Errorf("%s is written by the controller on a mission's stream", k)
		}
	}
	return nil
}

// classifyLandingRoots classifies the live-scope roots of a mission at the
// decision time at, under the caller's locks (the mission's, then the
// repository stream's). inScope false: no root of the mission is in the live
// landing scope, and the current rules close it.
func classifyLandingRoots(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, round *closeRound, at time.Time) (inScope bool, roots []LandingRootClass, ruleNote string, err error) {
	const repo = "bifrost-platform"
	stream := "landing:" + repo
	state, err := folds.state(ctx, tx, stream)
	if err != nil || state.LiveAt.IsZero() {
		return false, nil, "", err
	}
	var created time.Time
	var noMerge bool
	var tenant string
	if err := tx.QueryRow(ctx, `SELECT created_at, COALESCE(scope_json->>'no_merge','') <> '', tenant FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&created, &noMerge, &tenant); err != nil {
		return false, nil, "", err
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.state,
	    (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted'),
	    t.state = 'cancelled' AND EXISTS (SELECT 1 FROM agentrt.event d WHERE d.task_id = t.task_id AND d.event_type = 'owner_wait_closed' AND d.body_json->>'outcome' = 'dropped')
	  FROM agentrt.task t WHERE t.mission_id = $1 AND t.role = 'implement' AND t.repo = $2 ORDER BY t.task_id`, mission, repo)
	if err != nil {
		return false, nil, "", err
	}
	type root struct {
		task, state string
		accepted    *time.Time
		dropped     bool
	}
	var candidates []root
	for rows.Next() {
		var r root
		if err := rows.Scan(&r.task, &r.state, &r.accepted, &r.dropped); err != nil {
			rows.Close()
			return false, nil, "", err
		}
		candidates = append(candidates, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, "", err
	}
	fence := state.Fence()
	// The rule as in force at the decision (CUT §11.8 v3 item 5).
	none := false
	rule, err := CurrentRule(ctx, tx, "landing:"+tenant, at)
	if err != nil {
		return false, nil, "", err
	}
	switch {
	case rule == nil || rule.Rule.Landing == nil:
		ruleNote = "no landing rule in force"
	default:
		r, ok := rule.Rule.Landing.Repo(repo)
		switch {
		case !ok:
			ruleNote = fmt.Sprintf("%s v%d has no %s", rule.Rule.ID, rule.Rule.Version, repo)
		case r.STGRelease != "none":
			ruleNote = fmt.Sprintf("%s v%d, stg_release %s", rule.Rule.ID, rule.Rule.Version, r.STGRelease)
		default:
			none = true
			ruleNote = fmt.Sprintf("%s v%d, stg_release none", rule.Rule.ID, rule.Rule.Version)
		}
	}
	// Landed roots whose latest evidence is effective: delivered when it was
	// read in a round valid for this snapshot.
	type freshRead struct {
		i      int
		readAt time.Time
	}
	var fresh []freshRead
	for _, r := range candidates {
		live := false
		for _, o := range state.Operations {
			if o.Task == r.task && o.Live {
				live = true
			}
		}
		handoff := !fence.IsZero() && r.accepted != nil && !r.accepted.Before(fence) && !created.Before(fence) && !noMerge
		if !live && !handoff {
			continue
		}
		inScope = true
		op := state.LatestOperation(r.task)
		o, has := state.Operations[op]
		c := LandingRootClass{Root: r.task, Op: op, Class: RootUnresolved}
		landed := false
		dispWhy, err := landingDisposed(ctx, tx, stream, r.task, op)
		if err != nil {
			return false, nil, "", err
		}
		disposed := dispWhy == "owner"
		switch {
		case !has:
			c.Reason = "unclaimed"
			// Why it waits: the loop's latest refusal, when there is one.
			if why, _, err := latestClaimRefusal(ctx, tx, stream, r.task); err != nil {
				return false, nil, "", err
			} else if why != "" {
				c.Reason = "unclaimed:" + why
			}
			if r.dropped {
				c.Class, c.Reason = RootDisposed, "dropped"
			}
		case o.AtMain():
			c.Reason = "at_main"
		case o.EverLanded && !o.Reverted:
			// The priority table, each row whole (review 672 R-01): a
			// delivery read in a valid round (decided below, once the
			// snapshot is known) over the Owner's disposition over the
			// rule over evidence still to read.
			ev, found, err := latestMainEvidence(ctx, tx, stream, op)
			if err != nil {
				return false, nil, "", err
			}
			switch {
			case disposed || r.dropped:
				c.Class, c.Reason = RootDisposed, "owner"
				if !disposed {
					c.Reason = "dropped"
				}
				if none && found && ev.validity == "effective" {
					c.read = true
					fresh = append(fresh, freshRead{len(roots), ev.readAt})
				}
			case !none:
				c.Reason = "rule"
			case !found || ev.validity == "effective":
				c.Reason, c.NeedsEvidence, c.read, landed = "evidence", true, true, true
				if found {
					fresh = append(fresh, freshRead{len(roots), ev.readAt})
				}
			default:
				c.Reason, c.read, landed = "landed_unverified", true, true
			}
		case o.EverLanded && o.Reverted:
			c.Class, c.Reason = RootDisposed, "reverted"
		case o.Phase == "failed" && slices.Contains([]string{"declined", "main_lost", "reverted"}, o.Outcome):
			c.Class, c.Reason = RootDisposed, o.Outcome
		case disposed:
			c.Class, c.Reason = RootDisposed, "owner"
		case r.dropped:
			c.Class, c.Reason = RootDisposed, "dropped"
		case o.Phase == "failed" && o.Outcome == "in_main" && dispWhy == "owner_delivered":
			// The Owner counted the change already in main as delivered.
			if none {
				c.Class, c.Reason = RootDelivered, "in_main"
			} else {
				c.Reason = "rule"
			}
		case o.Phase == "failed" && o.Outcome == "in_main":
			c.Reason = "in_main_unverified"
		case o.Phase == "failed":
			c.Reason = "failed:" + o.Outcome
		case o.Phase == "parked":
			c.Reason = "parked:" + o.Why // shown with its since (review 672 R-07)
		default:
			c.Reason = o.Phase
			if o.Outcome != "" {
				c.Reason += ":" + o.Outcome
			}
		}
		c.key = c.Root + "|" + c.Op + "|" + c.Class + ":" + c.Reason
		if landed {
			c.key = c.Root + "|" + c.Op + "|landed"
		}
		roots = append(roots, c)
	}
	// A delivery: effective evidence read after the start of a round opened
	// on this very snapshot, before its end.
	snapshot, _ := closeSnapshot(roots)
	if round.validFor(snapshot, at) {
		for _, f := range fresh {
			if f.readAt.After(round.At) {
				r := &roots[f.i]
				r.Class, r.Reason, r.NeedsEvidence, r.evidenceAt = RootDelivered, "landed", false, f.readAt
			}
		}
	}
	return inScope, roots, ruleNote, nil
}

// landingDisposed is how the Owner counted this root's operation on its card
// (landing_disposed, B4 v3 item 4): "owner" (not delivered),
// "owner_delivered", or "" for no decision. The latest counts.
func landingDisposed(ctx context.Context, tx pgx.Tx, stream, root, op string) (string, error) {
	var why string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT body_json->'landing_disposed'->>'why' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_disposed'->>'root' = $2 AND body_json->'landing_disposed'->>'op' = $3 ORDER BY seq DESC LIMIT 1), '')`, stream, root, op).Scan(&why)
	return why, err
}

// landingCloseB4 decides a mission in the live landing scope (CUT §11.8
// v3): applies false when none of its roots is in scope. Otherwise held while
// a root is unresolved; when the only thing missing is fresh main evidence,
// a closing round is opened (or kept) and the mission held; when every root
// is delivered or disposed, the state to move to and why.
func landingCloseB4(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string) (applies, held bool, to, why string, err error) {
	// Only a stream with the live mode is locked here: every other mission
	// closes by the rules before S3, with no new lock on the landing stream
	// (a close pass must not wait on it, nor make its writers wait). The
	// mode is written once; one that appears after this read is seen by the
	// next pass.
	// A mission with no implementation root in the repository never reads
	// its stream: an unreadable stream holds only the missions it carries.
	var carries bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.task WHERE mission_id = $1 AND role = 'implement' AND repo = 'bifrost-platform')`, mission).Scan(&carries); err != nil || !carries {
		return false, false, "", "", err
	}
	if s, err := folds.state(ctx, tx, "landing:bifrost-platform"); err != nil || s.LiveAt.IsZero() {
		return false, false, "", "", err
	}
	if err := lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
		return false, false, "", "", err
	}
	at := DecisionAt()
	round, err := readCloseRound(ctx, tx, mission)
	if err != nil {
		return false, false, "", "", err
	}
	inScope, roots, ruleNote, err := classifyLandingRoots(ctx, tx, folds, mission, round, at)
	if err != nil || !inScope {
		return false, false, "", "", err
	}
	var needs int
	delivered, disposed := 0, 0
	for _, r := range roots {
		switch {
		case r.NeedsEvidence:
			needs++
		case r.Class == RootUnresolved:
			return true, true, "", "", nil
		case r.Class == RootDelivered:
			delivered++
		default:
			disposed++
		}
	}
	snapshot, ops := closeSnapshot(roots)
	if needs > 0 {
		// A round open on this snapshot waits for its reads: evidence for
		// some of its operations narrows what is left, it does not reopen
		// the round (review 672 B-01).
		if round.validFor(snapshot, at) {
			return true, true, "", "", nil
		}
		next := int64(1)
		if round != nil {
			next = round.Round + 1
		}
		_, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: "controller", MissionID: mission,
			Body: map[string]any{"text": fmt.Sprintf("Closing round %d: main is read again for %d landed root(s)", next, len(ops)),
				"landing_close_round": map[string]any{"v": "2", "round": next, "ops": ops, "snapshot": snapshot,
					"at": at.UTC().Format(time.RFC3339Nano), "until": at.Add(CloseRoundFor).UTC().Format(time.RFC3339Nano)}}})
		return true, true, "", "", err
	}
	var briefs int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = $1 AND role = 'brief' AND state = 'accepted'`, mission).Scan(&briefs); err != nil {
		return false, false, "", "", err
	}
	to = "stopped"
	if delivered > 0 || briefs > 0 {
		to = "done"
	}
	why = fmt.Sprintf("Closed from landing evidence at %s: %d root(s) delivered, %d disposed, 0 unresolved (%s)", at.UTC().Format(time.RFC3339), delivered, disposed, ruleNote)
	if delivered == 0 && briefs > 0 {
		why += fmt.Sprintf("; done for its %d accepted brief(s), no root delivered", briefs)
	}
	// What the close decided on, kept apart from what is read later (review
	// 672 N-01): the policy, the round, each root's class and the counts.
	// The round is named only when a delivery rests on its evidence; each
	// root keeps when that evidence was read (review 675 N-01).
	var roundN int64
	list := make([]any, 0, len(roots))
	for _, r := range roots {
		item := map[string]any{"root": r.Root, "op": r.Op, "class": r.Class, "reason": r.Reason}
		if !r.evidenceAt.IsZero() {
			item["evidence_read_at"] = r.evidenceAt.UTC().Format(time.RFC3339Nano)
			if round != nil {
				roundN = round.Round
			}
		}
		list = append(list, item)
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: "controller", MissionID: mission,
		Body: map[string]any{"text": why, "landing_close": map[string]any{"v": "1", "to": to, "at": at.UTC().Format(time.RFC3339Nano),
			"policy": "landing_evidence", "rule": ruleNote, "round": roundN, "roots": list,
			"delivered": delivered, "disposed": disposed, "briefs": briefs}}}); err != nil {
		return false, false, "", "", err
	}
	return true, false, to, why, nil
}

// LandingClose is a mission's close from landing evidence as decided
// (landing_close): Roots are as classified then.
type LandingClose struct {
	To, Rule, At                string
	Round                       int64
	Delivered, Disposed, Briefs int64
	Roots                       []map[string]any
}

// readLandingClose is the mission's latest landing_close, or nil.
func readLandingClose(ctx context.Context, tx pgx.Tx, mission string) (*LandingClose, error) {
	var m map[string]any
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_close' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND actor_id = 'controller' AND body_json ? 'landing_close' ORDER BY seq DESC LIMIT 1`, MissionStream(mission)).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &LandingClose{To: fmt.Sprint(m["to"]), Rule: fmt.Sprint(m["rule"]), At: fmt.Sprint(m["at"])}
	c.Round, _ = cardNumber(m["round"])
	c.Delivered, _ = cardNumber(m["delivered"])
	c.Disposed, _ = cardNumber(m["disposed"])
	c.Briefs, _ = cardNumber(m["briefs"])
	if roots, ok := m["roots"].([]any); ok {
		for _, r := range roots {
			if x, ok := r.(map[string]any); ok {
				c.Roots = append(c.Roots, x)
			}
		}
	}
	return c, nil
}

// badLandingClose is why a landing_close body is refused, or "".
func badLandingClose(v any) string {
	m, ok := v.(map[string]any)
	if !ok || m["v"] != "1" {
		return "landing_close is not a v1 object"
	}
	if to := m["to"]; to != "done" && to != "stopped" {
		return "landing_close needs to done or stopped"
	}
	if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(m["at"])); err != nil {
		return "landing_close needs at"
	}
	if m["policy"] != "landing_evidence" {
		return "landing_close needs policy"
	}
	for _, k := range []string{"round", "delivered", "disposed", "briefs"} {
		if n, ok := cardNumber(m[k]); !ok || n < 0 {
			return "landing_close needs " + k
		}
	}
	var roots []any
	switch t := m["roots"].(type) {
	case []any:
		roots = t
	case []map[string]any:
		for _, x := range t {
			roots = append(roots, x)
		}
	default:
		return "landing_close needs roots"
	}
	if len(roots) == 0 {
		return "landing_close needs roots"
	}
	for _, r := range roots {
		x, ok := r.(map[string]any)
		if !ok {
			return "landing_close root is not an object"
		}
		for _, k := range []string{"root", "class", "reason"} {
			if s, _ := x[k].(string); s == "" {
				return "landing_close root needs " + k
			}
		}
		if c := x["class"]; c != RootDelivered && c != RootDisposed {
			return "landing_close root is delivered or disposed"
		}
		if e, has := x["evidence_read_at"]; has {
			if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(e)); err != nil {
				return "landing_close root evidence_read_at is not a time"
			}
		}
	}
	return ""
}

// OpenCloseRoundOps are the operations whose main is to be read for an open
// closing round now: the reconciler reads them ahead of its usual wait.
func OpenCloseRoundOps(ctx context.Context, db DB, now time.Time) (map[string]bool, error) {
	out := map[string]bool{}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (event_stream_id) body_json->'landing_close_round' FROM agentrt.event
		  WHERE event_type = 'note' AND body_json ? 'landing_close_round' ORDER BY event_stream_id, seq DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m map[string]any
			if err := rows.Scan(&m); err != nil {
				return err
			}
			until, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(m["until"]))
			if !now.Before(until) {
				continue
			}
			if ops, ok := m["ops"].([]any); ok {
				for _, o := range ops {
					out[fmt.Sprint(o)] = true
				}
			}
		}
		return rows.Err()
	})
	return out, err
}

// MissionLandingStatus is a mission's landing status for the Console (CUT
// §11.8 v3 item 9): each live-scope root's class and reason, the card that
// stands for it (if any), the rule as read now and the closing round still
// open (nil once its time is over). A read: no lock, no write.
type MissionLandingStatus struct {
	InScope bool
	Roots   []LandingRootStatus
	Rule    string
	Round   *closeRound
	// Closed is the close as decided then (landing_close), apart from the
	// roots as read now (review 672 N-01). nil while not closed so.
	Closed *LandingClose
}

// LandingRootStatus is one root in MissionLandingStatus.
type LandingRootStatus struct {
	LandingRootClass
	CardKind, QuestionID string
	// CardAlert names a card of this root that ended without its answer
	// being known (unconfirmed, lost): the Owner may have answered a card
	// the runtime could not read back (review 670 R-07). "" for none.
	CardAlert string
	// A parked root (review 672 R-07): since its park, its retry, the
	// status-only trigger it shows (ci_stuck, parked_long) and when a park
	// becomes parked_long. Zero / "" otherwise.
	ParkedSince, RetryAt, RemindAt time.Time
	Shown                          string
	// The Owner's answer holds the root (review 675 R-07): wait or remind
	// later on its latest answered card, until HeldUntil, on HeldQuestion.
	HeldUntil    time.Time
	HeldQuestion string
	// CardPending: a card for the root is prepared and not yet asked (the
	// card budget or quota holds it).
	CardPending bool
}

// ReadMissionLandingStatus reads a mission's landing status.
func ReadMissionLandingStatus(ctx context.Context, db DB, mission string) (MissionLandingStatus, error) {
	var st MissionLandingStatus
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		round, err := readCloseRound(ctx, tx, mission)
		if err != nil {
			return err
		}
		closed, err := readLandingClose(ctx, tx, mission)
		if err != nil {
			return err
		}
		st.Closed = closed
		at := DecisionAt()
		inScope, roots, rule, err := classifyLandingRoots(ctx, tx, landingFolds{}, mission, round, at)
		if err != nil || !inScope {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream("bifrost-platform"))
		if err != nil {
			return err
		}
		triggers, err := LandingCardTriggers(ctx, tx, "bifrost-platform", at)
		if err != nil {
			return err
		}
		// Only an open round is the current one (the 673 report): a past
		// round is history, its time over.
		if round != nil && !at.Before(round.Until) {
			round = nil
		}
		st = MissionLandingStatus{InScope: true, Rule: rule, Round: round, Closed: closed}
		for _, r := range roots {
			rs := LandingRootStatus{LandingRootClass: r}
			var latest, answered CardRecord
			for _, c := range cards {
				if c.Root == r.Root && c.Op == r.Op && !c.Ended() && c.N >= latest.N {
					latest = c
				}
				if c.Root == r.Root && c.Op == r.Op && c.QuestionID != "" && c.N >= answered.N {
					answered = c
				}
				if c.Root == r.Root && c.Op == r.Op && !c.Bound && !c.Ended() {
					rs.CardPending = true
				}
				if c.Root == r.Root && c.Op == r.Op && c.Closed && (c.ClosedWhy == "unconfirmed" || c.ClosedWhy == "lost") {
					rs.CardAlert = c.Prep + " " + c.ClosedWhy
				}
			}
			rs.CardKind, rs.QuestionID = latest.Kind, latest.QuestionID
			if answered.QuestionID != "" {
				o, _, found, err := storedCardRecord(ctx, tx, answered, answered.QuestionID)
				if err != nil {
					return err
				}
				if found && o.outcome == "held" && at.Before(o.remindAt) {
					rs.HeldUntil, rs.HeldQuestion = o.remindAt, answered.QuestionID
				}
			}
			if strings.HasPrefix(r.Reason, "parked:") {
				var retry *string
				err := tx.QueryRow(ctx, `SELECT at, body_json->'landing'->>'retry_at' FROM agentrt.event WHERE event_stream_id = 'landing:bifrost-platform'
				  AND event_type = 'note' AND body_json->'landing'->>'kind' = 'parked' AND body_json->'landing'->>'op' = $1 ORDER BY seq DESC LIMIT 1`, r.Op).Scan(&rs.ParkedSince, &retry)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if retry != nil {
					rs.RetryAt, _ = time.Parse(time.RFC3339Nano, *retry)
				}
				if why := strings.TrimPrefix(r.Reason, "parked:"); why != "ci_stuck" && why != "rule" && !rs.ParkedSince.IsZero() {
					rs.RemindAt = rs.ParkedSince.Add(ParkedLongAfter)
				}
				for _, tr := range triggers {
					if tr.Op == r.Op && slices.Contains(statusOnlyKinds, tr.Kind) {
						rs.Shown = tr.Kind
					}
				}
			}
			st.Roots = append(st.Roots, rs)
		}
		return nil
	})
	return st, err
}
