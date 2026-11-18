// Package actionrecord is the append-only record of actions taken or refused
// (migration 0022; Owner 2026-11-15). A card is for changing a law; every
// other action is done by the line that owns it and recorded here, for the
// Owner's review afterwards: who, what, why, what undoes it and whether that
// was ready, on which law it rested, how it ended, what was checked after,
// what was not.
package actionrecord

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

const (
	RegretReady      = "ready"      // prepared and verified by the machine
	RegretDeclared   = "declared"   // the caller wrote what undoes it; nobody verified it
	RegretUndeclared = "undeclared" // the caller wrote nothing (first phase: record only)
	RegretNotReady   = "not_ready"

	OutcomeDone    = "done"
	OutcomeRefused = "refused"
	OutcomeFailed  = "failed"
)

// Record is one action. ID and At are set on a record read back.
type Record struct {
	ID   int64
	At   time.Time
	Seat string // the session that did it, or the principal's name
	// Action is the catalog id; Target one line of its key parameters.
	Action, Target string
	Reason         string
	// Regret is what undoes it; RegretState says whether that was ready when
	// the action ran (ready, not_ready, none_needed).
	Regret, RegretState string
	// Law is the rule the action rested on, one short phrase naming it.
	Law     string
	Outcome string // done, refused, failed
	// CheckResult is what was checked after; NotVerified what was not.
	CheckResult, NotVerified string
	ParamsHash               string
}

var (
	actionName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)
	hashText   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ErrInvalid wraps every refusal of a record that does not read as one.
var ErrInvalid = eventlog.ErrInvalid

// Redactor cleans free text before it is stored (secrets, tokens). The
// server passes approvals.Redact; a nil one stores the text as given.
type Redactor func(string) string

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Normalize checks a record and returns it with its free text trimmed and
// redacted. A record that cannot say who, what, why, on which law and how it
// ended is refused, so a gap shows when the action is built, not at review.
func Normalize(r Record, redact Redactor) (Record, error) {
	clean := func(s string, max int) string {
		s = oneLine(s)
		if redact != nil {
			s = redact(s)
		}
		if len(s) > max {
			s = s[:max]
		}
		return s
	}
	r.Seat = strings.TrimSpace(r.Seat)
	r.Action = strings.TrimSpace(r.Action)
	r.Target = clean(r.Target, 500)
	r.Reason = clean(r.Reason, 4000)
	r.Regret = clean(r.Regret, 2000)
	r.Law = clean(r.Law, 200)
	r.CheckResult = clean(r.CheckResult, 2000)
	r.NotVerified = clean(r.NotVerified, 2000)
	switch {
	case r.Seat == "" || len(r.Seat) > 128:
		return r, fmt.Errorf("%w: a record names who did it", ErrInvalid)
	case !actionName.MatchString(r.Action):
		return r, fmt.Errorf("%w: a record names the action", ErrInvalid)
	case r.Reason == "":
		return r, fmt.Errorf("%w: a record says why", ErrInvalid)
	case r.Law == "":
		return r, fmt.Errorf("%w: a record names the law it rested on", ErrInvalid)
	case r.RegretState != RegretReady && r.RegretState != RegretDeclared && r.RegretState != RegretUndeclared &&
		r.RegretState != RegretNotReady:
		return r, fmt.Errorf("%w: regret_state is ready, declared, undeclared or not_ready", ErrInvalid)
	case r.Outcome != OutcomeDone && r.Outcome != OutcomeRefused && r.Outcome != OutcomeFailed:
		return r, fmt.Errorf("%w: outcome is done, refused or failed", ErrInvalid)
	case (r.RegretState == RegretReady || r.RegretState == RegretDeclared) && r.Regret == "":
		return r, fmt.Errorf("%w: a ready or declared regret says what it is", ErrInvalid)
	case r.Outcome == OutcomeDone && r.RegretState == RegretNotReady:
		// The rule of the new definition: no regret prepared, no action done.
		return r, fmt.Errorf("%w: an action is not done without its regret ready; record it as refused", ErrInvalid)
	case r.ParamsHash != "" && !hashText.MatchString(r.ParamsHash):
		return r, fmt.Errorf("%w: params_hash is a sha256", ErrInvalid)
	}
	return r, nil
}

// Insert normalizes and appends one record and returns its id.
func Insert(ctx context.Context, db eventlog.DB, r Record, redact Redactor) (int64, error) {
	r, err := Normalize(r, redact)
	if err != nil {
		return 0, err
	}
	var id int64
	err = eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO agentrt.action_record
		  (seat, action, target, reason, regret, regret_state, law, outcome, check_result, not_verified, params_hash)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')) RETURNING record_id`,
			r.Seat, r.Action, r.Target, r.Reason, r.Regret, r.RegretState, r.Law, r.Outcome, r.CheckResult, r.NotVerified,
			r.ParamsHash).Scan(&id); err != nil {
			return err
		}
		return announce(ctx, tx, id, r)
	})
	return id, err
}

// TypeRecorded is the bus's word that an action was recorded (channel S07,
// 2026-11-18: Agent actions on the bus). It carries who, what and how it
// ended; the reason and the rest stay in the record.
const TypeRecorded = "audit.action.recorded"

// busActor writes the events; the platform seeds it.
const busActor = "controller"

// busSeat is a seat the event can name.
var busSeat = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)

// Body is the event's body for a record.
func Body(id int64, r Record) map[string]any {
	s := r.Seat + ":" + r.Action
	return map[string]any{"record_id": id, "seat": r.Seat, "action": r.Action, "outcome": r.Outcome,
		"subject": s, "ref": "/api/v1/audit", "change": r.Outcome}
}

// announce writes the event in the record's transaction. A seat the event
// cannot name, or a ledger without the controller, leaves the record alone.
func announce(ctx context.Context, tx pgx.Tx, id int64, r Record) error {
	if !busSeat.MatchString(r.Seat) {
		return nil
	}
	if ok, err := actor.Exists(ctx, tx, busActor); err != nil || !ok {
		return err
	}
	_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "action:" + r.Seat, Type: TypeRecorded, Actor: busActor, Body: Body(id, r)})
	return err
}

// AuditBody is the event's body for a line of the platform's audit log, which
// has no record of its own here: who, what and how it ended (S02, 2026-11-18).
func AuditBody(who, action, status string) map[string]any {
	return map[string]any{"seat": who, "action": action, "outcome": status,
		"subject": who + ":" + action, "ref": "/api/v1/audit", "change": status}
}

// eventAction is an action the event can name: a record's, or an audit line's,
// which is dotted ("delivery.release_window.hold").
var eventAction = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,99}$`)

var auditStatus = regexp.MustCompile(`^[a-z_]{1,40}$`)

// AnnounceAudit says a line of the audit log on the bus. The target and the
// detail stay in the log. A line the event cannot name is left out.
func AnnounceAudit(ctx context.Context, db eventlog.DB, who, action, status string) error {
	if !busSeat.MatchString(who) || !eventAction.MatchString(action) || !auditStatus.MatchString(status) {
		return nil
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if ok, err := actor.Exists(ctx, tx, busActor); err != nil || !ok {
			return err
		}
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "audit:" + who, Type: TypeRecorded, Actor: busActor, Body: AuditBody(who, action, status)})
		return err
	})
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{TypeRecorded: {
			{Name: "record_id", Kind: eventlog.Int, Optional: true}, {Name: "seat", Kind: eventlog.Str}, {Name: "action", Kind: eventlog.Str},
			{Name: "outcome", Kind: eventlog.Str}, {Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str},
		}},
		Values: map[string]map[string]eventlog.Value{TypeRecorded: {"seat": eventlog.Shaped("a seat", busSeat), "action": eventlog.Shaped("an action id", eventAction)}},
		After: map[string]func(map[string]any) error{TypeRecorded: func(b map[string]any) error {
			seat, _ := b["seat"].(string)
			action, _ := b["action"].(string)
			if b["subject"] != seat+":"+action || b["ref"] != "/api/v1/audit" || b["change"] != b["outcome"] {
				return eventlog.Invalid("%s: subject, ref and change are made of seat, action and outcome", TypeRecorded)
			}
			return nil
		}},
	})
}

// Filter selects records; empty fields select all. Limit defaults to 100 and
// is at most 1000: a read that would be cut says so with More.
type Filter struct {
	Seat, Law, Action string
	Since, Until      time.Time
	Limit             int
}

// List reads records newest first. more is true when the filter matched
// more than Limit, so a count of the page is never taken for the whole.
func List(ctx context.Context, db eventlog.DB, f Filter) (records []Record, more bool, err error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		return nil, false, errors.New("limit is at most 1000")
	}
	err = eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		records = records[:0]
		rows, qerr := tx.Query(ctx, `SELECT record_id, recorded_at, seat, action, target, reason, regret, regret_state, law, outcome,
		    check_result, not_verified, COALESCE(params_hash,'')
		  FROM agentrt.action_record
		  WHERE ($1 = '' OR seat = $1) AND ($2 = '' OR law = $2) AND ($3 = '' OR action = $3)
		    AND ($4::timestamptz IS NULL OR recorded_at >= $4) AND ($5::timestamptz IS NULL OR recorded_at < $5)
		  ORDER BY record_id DESC LIMIT $6`,
			f.Seat, f.Law, f.Action, nullTime(f.Since), nullTime(f.Until), f.Limit+1)
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var r Record
			if qerr = rows.Scan(&r.ID, &r.At, &r.Seat, &r.Action, &r.Target, &r.Reason, &r.Regret, &r.RegretState, &r.Law, &r.Outcome,
				&r.CheckResult, &r.NotVerified, &r.ParamsHash); qerr != nil {
				return qerr
			}
			records = append(records, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, false, err
	}
	if len(records) > f.Limit {
		records, more = records[:f.Limit], true
	}
	return records, more, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
