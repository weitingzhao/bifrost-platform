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
}

// closeRound is a mission's closing round (landing_close_round).
type closeRound struct {
	Round     int64
	At, Until time.Time
	Ops       []string
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
	return r, nil
}

// badCloseRound is why a landing_close_round body is refused, or "".
func badCloseRound(v any) string {
	m, ok := v.(map[string]any)
	if !ok || m["v"] != "1" {
		return "landing_close_round is not a v1 object"
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

// checkCloseRoundAppend: the controller writes it on a mission's stream.
func checkCloseRoundAppend(ev Event) error {
	if _, ok := ev.Body["landing_close_round"]; !ok {
		return nil
	}
	if ev.Type != "note" || ev.Actor != "controller" || !strings.HasPrefix(ev.Stream, "mission:") {
		return fmt.Errorf("landing_close_round is written by the controller on a mission's stream")
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
	for _, r := range candidates {
		live := false
		for _, o := range state.Operations {
			if o.Task == r.task && o.Live {
				live = true
			}
		}
		handoff := r.accepted != nil && !r.accepted.Before(fence) && !created.Before(fence) && !noMerge
		if !live && !handoff {
			continue
		}
		inScope = true
		op := state.LatestOperation(r.task)
		o, has := state.Operations[op]
		c := LandingRootClass{Root: r.task, Op: op, Class: RootUnresolved}
		dispWhy, err := landingDisposed(ctx, tx, stream, r.task, op)
		if err != nil {
			return false, nil, "", err
		}
		disposed := dispWhy == "owner"
		switch {
		case !has:
			c.Reason = "unclaimed"
			if r.dropped {
				c.Class, c.Reason = RootDisposed, "dropped"
			}
		case o.AtMain():
			c.Reason = "at_main"
		case o.EverLanded && !o.Reverted:
			ev, found, err := latestMainEvidence(ctx, tx, stream, op)
			if err != nil {
				return false, nil, "", err
			}
			switch {
			case !none:
				c.Reason = "rule"
			case found && ev.validity == "effective" && round != nil && slices.Contains(round.Ops, op) && ev.readAt.After(round.At) && at.Before(round.Until):
				c.Class, c.Reason = RootDelivered, "landed"
			case !found || ev.validity == "effective":
				c.Reason, c.NeedsEvidence = "evidence", true
			default:
				c.Reason = "landed_unverified"
				if disposed {
					c.Class, c.Reason = RootDisposed, "owner"
				}
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
		default:
			c.Reason = o.Phase
			if o.Outcome != "" {
				c.Reason += ":" + o.Outcome
			}
		}
		roots = append(roots, c)
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
	var needs []string
	delivered, disposed := 0, 0
	for _, r := range roots {
		switch {
		case r.NeedsEvidence:
			needs = append(needs, r.Op)
		case r.Class == RootUnresolved:
			return true, true, "", "", nil
		case r.Class == RootDelivered:
			delivered++
		default:
			disposed++
		}
	}
	if len(needs) > 0 {
		sort.Strings(needs)
		if round != nil && at.Before(round.Until) && slices.Equal(round.Ops, needs) {
			return true, true, "", "", nil // the round is open: its reads come
		}
		next := int64(1)
		if round != nil {
			next = round.Round + 1
		}
		_, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: "controller", MissionID: mission,
			Body: map[string]any{"text": fmt.Sprintf("Closing round %d: main is read again for %d landed root(s)", next, len(needs)),
				"landing_close_round": map[string]any{"v": "1", "round": next, "ops": needs, "at": at.UTC().Format(time.RFC3339Nano),
					"until": at.Add(CloseRoundFor).UTC().Format(time.RFC3339Nano)}}})
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
	return true, false, to, why, nil
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
// stands for it (if any), the rule as read now and the current closing
// round. A read: no lock, no write.
type MissionLandingStatus struct {
	InScope bool
	Roots   []LandingRootStatus
	Rule    string
	Round   *closeRound
}

// LandingRootStatus is one root in MissionLandingStatus.
type LandingRootStatus struct {
	LandingRootClass
	CardKind, QuestionID string
}

// ReadMissionLandingStatus reads a mission's landing status.
func ReadMissionLandingStatus(ctx context.Context, db DB, mission string) (MissionLandingStatus, error) {
	var st MissionLandingStatus
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		round, err := readCloseRound(ctx, tx, mission)
		if err != nil {
			return err
		}
		inScope, roots, rule, err := classifyLandingRoots(ctx, tx, landingFolds{}, mission, round, DecisionAt())
		if err != nil || !inScope {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream("bifrost-platform"))
		if err != nil {
			return err
		}
		st = MissionLandingStatus{InScope: true, Rule: rule, Round: round}
		for _, r := range roots {
			rs := LandingRootStatus{LandingRootClass: r}
			var latest CardRecord
			for _, c := range cards {
				if c.Root == r.Root && c.Op == r.Op && !c.Ended() && c.N >= latest.N {
					latest = c
				}
			}
			rs.CardKind, rs.QuestionID = latest.Kind, latest.QuestionID
			st.Roots = append(st.Roots, rs)
		}
		return nil
	})
	return st, err
}
