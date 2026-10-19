package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The Owner's decisions on plan changes (planchange.go makes the
// proposals): the controller's pass that applies or refuses a change once
// its decision card is answered, and acknowledges the answer.

// pendingChange is a proposal waiting on the Owner's answer.
type pendingChange struct {
	event    int64
	stream   string // the proposal's: a plan's, or a recorder log's (recorderimport.go)
	root     string // the plan's root; "" on a stream that is not a plan's
	question string
	changes  []PlanChange
}

// streamRoot is the plan root a stream belongs to, or "".
func streamRoot(stream string) string {
	if root, ok := strings.CutPrefix(stream, "plan:"); ok {
		return root
	}
	return ""
}

// DecidePlanChanges is one pass of the plan decisions: each proposal waiting
// on a question whose answer is in gets plan_change_decided. The first
// option (CardApply) applies the change under that decision; any other
// option refuses it; a question withdrawn or expired leaves it unapplied. A
// change that no longer fits when it is applied is decided failed, with
// the database's reason. One that fails to be read does not stop the
// others. It returns how many proposals it decided.
func DecidePlanChanges(ctx context.Context, db DB, question func(context.Context, string) (Question, bool), ack func(context.Context, string) error) (int, error) {
	n, err := decidePending(ctx, db, question)
	if ack == nil {
		return n, err
	}
	return n, errors.Join(err, ackDecided(ctx, db, ack))
}

// ErrQuestionGone: the question store no longer has the question (it trims
// read and expired ones); there is nothing left to acknowledge.
var ErrQuestionGone = errors.New("store: the question is gone from the question store")

// ackDecided tells the question store the runtime has used the answer of
// each card it decided and has not yet acknowledged (review 180 B3): an
// answered card nobody acknowledged counts against the runtime's quota for a
// week. Each acknowledgement is recorded as a note on the plan's stream, so
// a pass acknowledges only what is left (review 182 B3); a card the store has
// since dropped is recorded as gone and not asked about again. A failure is
// tried again by the next pass.
func ackDecided(ctx context.Context, db DB, ack func(context.Context, string) error) error {
	type card struct{ stream, question string }
	var cards []card
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT d.event_stream_id, d.body_json->>'question_id' FROM agentrt.event d
		  WHERE d.event_type = 'plan_change_decided' AND d.body_json->>'decision' IN ('applied','refused','failed')
		    AND d.body_json ? 'question_id' AND d.at > clock_timestamp() - interval '7 days'
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event n WHERE n.event_type = 'note' AND n.event_stream_id = d.event_stream_id
		      AND n.body_json->>'text' IN ('acknowledged question ' || (d.body_json->>'question_id'),
		                                  'question ' || (d.body_json->>'question_id') || ' is gone from the question store'))
		  LIMIT 100`)
		if err != nil {
			return err
		}
		cards, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (card, error) {
			var c card
			return c, r.Scan(&c.stream, &c.question)
		})
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range cards {
		text := "acknowledged question " + c.question
		if aerr := ack(ctx, c.question); errors.Is(aerr, ErrQuestionGone) {
			text = "question " + c.question + " is gone from the question store"
		} else if aerr != nil {
			errs = append(errs, fmt.Errorf("acknowledge %s: %w", c.question, aerr))
			continue
		}
		if nerr := InTx(ctx, db, func(tx pgx.Tx) error {
			_, err := Append(ctx, tx, Event{Stream: c.stream, Type: "note", Actor: "controller", Body: map[string]any{"text": text}})
			return err
		}); nerr != nil {
			errs = append(errs, nerr)
		}
	}
	return errors.Join(errs...)
}

func decidePending(ctx context.Context, db DB, question func(context.Context, string) (Question, bool)) (int, error) {
	decided := 0
	var errs []error
	// Every waiting proposal, a page at a time after a cursor, so ones still
	// waiting for an answer never keep later ones from being decided.
	var after int64
	for {
		var page []pendingChange
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, qerr := tx.Query(ctx, `SELECT p.event_id, p.event_stream_id, p.body_json->>'question_id', (p.body_json->'changes')::text
			  FROM agentrt.event p
			  WHERE p.event_type = 'plan_change_proposed' AND p.body_json ? 'question_id' AND p.event_id > $1
			    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided'
			      AND d.event_stream_id = p.event_stream_id AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
			  ORDER BY p.event_id LIMIT $2`, after, decidePage)
			if qerr != nil {
				return qerr
			}
			var cerr error
			page, cerr = pgx.CollectRows(rows, func(r pgx.CollectableRow) (pendingChange, error) {
				var p pendingChange
				var doc string
				if serr := r.Scan(&p.event, &p.stream, &p.question, &doc); serr != nil {
					return p, serr
				}
				p.root = streamRoot(p.stream)
				return p, json.Unmarshal([]byte(doc), &p.changes)
			})
			return cerr
		})
		if err != nil {
			return decided, errors.Join(append(errs, err)...)
		}
		for _, p := range page {
			did, derr := decideOne(ctx, db, p, question)
			if derr != nil {
				errs = append(errs, fmt.Errorf("plan change %d: %w", p.event, derr))
			}
			if did {
				decided++
			}
		}
		if len(page) < decidePage {
			return decided, errors.Join(errs...)
		}
		after = page[len(page)-1].event
	}
}

// decidePage is how many waiting proposals a pass reads at a time.
const decidePage = 100

func decideOne(ctx context.Context, db DB, p pendingChange, question func(context.Context, string) (Question, bool)) (bool, error) {
	q, ok := question(ctx, p.question)
	switch {
	case !ok:
		return false, nil
	case q.Status == QuestionAnswered && q.Option == CardApplyKey:
		return decide(ctx, db, p, "applied", "")
	case q.Status == QuestionAnswered:
		return decide(ctx, db, p, "refused", "")
	case q.Status == "withdrawn" || q.Status == "expired":
		return decide(ctx, db, p, q.Status, "")
	}
	return false, nil
}

// decide records a decision under the tree's lock, once, and applies the
// change when it is applied; a change the database refuses then is decided
// failed instead, with its reason.
func decide(ctx context.Context, db DB, p pendingChange, decision, reason string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		artifact, isImport := importOf(p.changes)
		log, isRecorder := recorderImportOf(p.changes)
		switch {
		case isRecorder:
			// A recorder log's stream is its own lock (landRecorder).
			if err := lockStream(ctx, tx, p.stream); err != nil {
				return err
			}
		case p.root == "":
			return invalid("proposal %d on %s is neither a plan's nor a recorder import", p.event, p.stream)
		default:
			if err := lockTree(ctx, tx, p.root); err != nil {
				return err
			}
		}
		if decision == "applied" && !isImport && !isRecorder {
			if err := lockChanges(ctx, tx, p.root, p.changes); err != nil {
				return err
			}
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'plan_change_decided'
		  AND event_stream_id = $1 AND (body_json->>'proposal_event_id')::bigint = $2)`, p.stream, p.event).Scan(&done); err != nil || done {
			return err
		}
		body := map[string]any{"proposal_event_id": p.event, "decision": decision, "by": "owner", "question_id": p.question}
		if reason != "" {
			body["error"] = reason
		}
		ev, err := Append(ctx, tx, Event{Stream: p.stream, Type: "plan_change_decided", Actor: "controller", Body: body})
		if err != nil {
			return err
		}
		switch {
		case decision == "applied" && isImport:
			if err = decideImport(ctx, tx, artifact, ev.EventID); err != nil {
				return err
			}
		case decision == "applied" && isRecorder:
			if err = decideRecorderImport(ctx, tx, log, ev.EventID); err != nil {
				return err
			}
		case decision == "applied":
			if _, err = applyChanges(ctx, tx, "controller", p.root, ev.EventID, p.changes); err != nil {
				return err
			}
		}
		did = true
		return nil
	})
	if err != nil && decision == "applied" && retryable(err) {
		// Busy or moved: nothing was written; the next pass tries again.
		return false, nil
	}
	if err != nil && decision == "applied" && refusedByRules(err) {
		return decide(ctx, db, p, "failed", err.Error())
	}
	return did, err
}

// refusedByRules: the change broke a rule of its own or of the database,
// not a passing failure.
func refusedByRules(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, ErrInvalid) || (errors.As(err, &pg) && (pg.Code == "23502" || pg.Code == "23503" || pg.Code == "23505" ||
		pg.Code == "23514" || pg.Code == "P0001"))
}
