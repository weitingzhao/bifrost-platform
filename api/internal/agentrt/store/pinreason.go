package store

import (
	"fmt"
	"strings"
	"time"
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
