package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Why an implement task is pinned to the coordinator (R5:5.7-8 slice 1).
// From 2026-11-01 every implement task was pinned to external:coordinator:
// the coordinator wrote the code itself instead of the runtime dispatching
// implementers. Such a task now says why in pin_reason, recorded in its
// task_proposed body (no column); the Console counts the share of the last
// days (PinShare). AGENTRT_PIN_REASON decides what a pin without a reason
// gets: warn (the default) accepts it, counted as unexplained; refuse
// answers 422 naming the field.

// MaxPinReason is the most bytes a pin_reason holds.
const MaxPinReason = 200

// PinReasonMode is what a coordinator pin without a reason gets.
type PinReasonMode string

const (
	// PinReasonWarn accepts it; PinShare counts it as unexplained.
	PinReasonWarn PinReasonMode = "warn"
	// PinReasonRefuse refuses the batch (422, naming pin_reason).
	PinReasonRefuse PinReasonMode = "refuse"
)

// ParsePinReason reads AGENTRT_PIN_REASON: unset is warn.
func ParsePinReason(s string) (PinReasonMode, error) {
	switch m := PinReasonMode(strings.TrimSpace(s)); m {
	case "", PinReasonWarn:
		return PinReasonWarn, nil
	case PinReasonRefuse:
		return m, nil
	}
	return PinReasonWarn, fmt.Errorf("AGENTRT_PIN_REASON is warn or refuse, not %q", s)
}

// WantsPinReason says whether a proposed task is one that says why it is
// pinned: an implementation pinned to the coordinator.
func WantsPinReason(t PinnedTask) bool {
	return t.Role == "implement" && t.ExecutorPin == rules.PinCoordinator
}

// CheckPinReason checks a task's pin_reason before anything is written: one
// on a task that is not pinned to the coordinator, a blank one or one longer
// than MaxPinReason is refused whatever the mode; a missing one is refused
// under refuse alone. missing says it is missing on a task that wants one.
func CheckPinReason(t PinnedTask, mode PinReasonMode) (missing bool, err error) {
	switch {
	case t.PinReason != "" && !WantsPinReason(t):
		return false, invalid("task %s: pin_reason is for an implement task with executor_pin %s", t.Ref, rules.PinCoordinator)
	case len(t.PinReason) > MaxPinReason:
		return false, invalid("task %s: pin_reason is at most %d bytes, not %d", t.Ref, MaxPinReason, len(t.PinReason))
	case t.PinReason != "" && strings.TrimSpace(t.PinReason) == "":
		return false, invalid("task %s: pin_reason is blank", t.Ref)
	case t.PinReason == "" && WantsPinReason(t):
		if mode == PinReasonRefuse {
			return true, invalid("task %s: pin_reason is required on an implement task pinned to %s (at most %d bytes: why the coordinator writes it, not a runtime implementer)",
				t.Ref, rules.PinCoordinator, MaxPinReason)
		}
		return true, nil
	}
	return false, nil
}

// PinShareDays is how many days PinShare looks back.
const PinShareDays = 7

// Share is the implement tasks of the last days pinned to the coordinator:
// Implement proposed since Since, Pinned of them pinned to external:coordinator,
// Unexplained of those with no pin_reason in their task_proposed event.
type Share struct {
	Since       time.Time `json:"since"`
	ReadAt      time.Time `json:"read_at"`
	Days        int       `json:"days"`
	Implement   int       `json:"implement"`
	Pinned      int       `json:"pinned"`
	Unexplained int       `json:"unexplained"`
	Summary     string    `json:"summary"`
}

// PinShare reads the share of implement tasks pinned to the coordinator in
// the PinShareDays before now, from the tasks and their creation events.
func PinShare(ctx context.Context, db DB, now time.Time) (Share, error) {
	s := Share{Since: now.Add(-PinShareDays * 24 * time.Hour), ReadAt: now, Days: PinShareDays}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*),
		    count(*) FILTER (WHERE t.executor_pin = $2),
		    count(*) FILTER (WHERE t.executor_pin = $2 AND COALESCE(e.body_json->>'pin_reason', '') = '')
		  FROM agentrt.task t
		  LEFT JOIN LATERAL (SELECT body_json FROM agentrt.event x WHERE x.task_id = t.task_id AND x.event_type = 'task_proposed'
		    ORDER BY x.event_id LIMIT 1) e ON true
		  WHERE t.role = 'implement' AND t.created_at >= $1`, s.Since, rules.PinCoordinator).Scan(&s.Implement, &s.Pinned, &s.Unexplained)
	})
	if err != nil {
		return Share{}, err
	}
	s.Summary = fmt.Sprintf("Implement tasks pinned to the coordinator, last %d days: %d of %d (%d without a reason)", s.Days, s.Pinned, s.Implement, s.Unexplained)
	return s, nil
}
