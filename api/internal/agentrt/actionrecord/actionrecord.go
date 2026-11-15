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

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

const (
	RegretReady      = "ready"      // prepared and verified by the machine
	RegretDeclared   = "declared"   // the caller wrote what undoes it; nobody verified it
	RegretUndeclared = "undeclared" // the caller wrote nothing (first phase: record only)
	RegretNotReady   = "not_ready"
	RegretNoneNeeded = "none_needed"

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
	ParamsHash, ApprovalRef  string
	// CorrectionOf is the record this one corrects, 0 for none.
	CorrectionOf int64
}

var (
	actionName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)
	hashText   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	refText    = regexp.MustCompile(`^[A-Za-z0-9_#-]{1,64}$`)
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
		r.RegretState != RegretNotReady && r.RegretState != RegretNoneNeeded:
		return r, fmt.Errorf("%w: regret_state is ready, declared, undeclared, not_ready or none_needed", ErrInvalid)
	case r.Outcome != OutcomeDone && r.Outcome != OutcomeRefused && r.Outcome != OutcomeFailed:
		return r, fmt.Errorf("%w: outcome is done, refused or failed", ErrInvalid)
	case (r.RegretState == RegretReady || r.RegretState == RegretDeclared) && r.Regret == "":
		return r, fmt.Errorf("%w: a ready or declared regret says what it is", ErrInvalid)
	case r.Outcome == OutcomeDone && r.RegretState == RegretNotReady:
		// The rule of the new definition: no regret prepared, no action done.
		return r, fmt.Errorf("%w: an action is not done without its regret ready; record it as refused", ErrInvalid)
	case r.ParamsHash != "" && !hashText.MatchString(r.ParamsHash):
		return r, fmt.Errorf("%w: params_hash is a sha256", ErrInvalid)
	case r.ApprovalRef != "" && !refText.MatchString(r.ApprovalRef):
		return r, fmt.Errorf("%w: approval_ref is a card id or number", ErrInvalid)
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
		return tx.QueryRow(ctx, `INSERT INTO agentrt.action_record
		  (seat, action, target, reason, regret, regret_state, law, outcome, check_result, not_verified, params_hash, approval_ref, correction_of)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13::bigint,0)) RETURNING record_id`,
			r.Seat, r.Action, r.Target, r.Reason, r.Regret, r.RegretState, r.Law, r.Outcome, r.CheckResult, r.NotVerified,
			r.ParamsHash, r.ApprovalRef, r.CorrectionOf).Scan(&id)
	})
	return id, err
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
		    check_result, not_verified, COALESCE(params_hash,''), COALESCE(approval_ref,''), COALESCE(correction_of,0)
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
				&r.CheckResult, &r.NotVerified, &r.ParamsHash, &r.ApprovalRef, &r.CorrectionOf); qerr != nil {
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
