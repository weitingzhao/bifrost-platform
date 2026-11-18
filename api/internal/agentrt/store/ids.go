package store

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// newULID returns a 26-character ULID (eventlog.NewULID).
func newULID(now time.Time) string { return eventlog.NewULID(now) }

// NewTaskID, NewAttemptID, NewMessageID and NewVerificationID mint ids in the
// shapes the tables check.
func NewTaskID() string { return "t_" + newULID(time.Now()) }

// NewAttemptID and NewVerificationID mint ids in the shapes the tables
// check. Only test fixtures of several packages use them now: nothing leases
// or verifies any more (store cut 4, 2026-11-14).
func NewAttemptID() string { return "a_" + newULID(time.Now()) }
