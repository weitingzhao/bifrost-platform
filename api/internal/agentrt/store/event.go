// Package store is the data access of the agent runtime. It opens no HTTP;
// the kernel (2.2) calls it. Every state change goes through Append, the one
// path that writes an event (package eventlog, the base this package and
// the other domains write through).
package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The event log's own names, as this package has always exported them.
type (
	Event      = eventlog.Event
	Appended   = eventlog.Appended
	ChainBreak = eventlog.ChainBreak
)

const timeLayout = eventlog.TimeLayout

// Append writes one event inside tx (eventlog.Append), after the checks of
// the vocabulary this package and the other domains declare (vocab.go).
func Append(ctx context.Context, tx pgx.Tx, ev Event) (Appended, error) {
	return eventlog.Append(ctx, tx, ev)
}

func lockStream(ctx context.Context, tx pgx.Tx, stream string) error {
	return eventlog.LockStream(ctx, tx, stream)
}

func nullable(s string) any { return eventlog.Nullable(s) }

func eventNow(ctx context.Context) time.Time { return eventlog.Now(ctx) }

// WithEventClock, DecisionAt, VerifyStream and SetEventClock are the event
// log's (eventlog).
func WithEventClock(ctx context.Context, clock func() time.Time) context.Context {
	return eventlog.WithEventClock(ctx, clock)
}

func VerifyStream(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, stream string) (int64, error) {
	return eventlog.VerifyStream(ctx, db, stream)
}

// retiredNoteFields are the records of the landing lane, the B5 STG release
// and the platform's waves. Their writers were removed on 2026-11-14
// (LANE-BIZ-ALIGN, store cut 1); rows already written stay readable until
// the table clean-up.
var retiredNoteFields = []string{"landing", "stg_release", "stg_sample", "landing_answer", "landing_gate", "landing_grant_request",
	"landing_grant", "landing_grant_revoked", "landing_grant_use", "landing_grant_refused", "landing_ci_key", "landing_rule_clear",
	"landing_batch_grant", "landing_anomaly", "landing_main", "landing_stop", "landing_resume", "landing_card_prepared",
	"landing_card_ask_started", "landing_card", "landing_card_acked", "landing_card_closed", "landing_card_outcome",
	"landing_close_round", "landing_close", "landing_refusal", "landing_floor", "landing_repo", "landing_pusher", "landing_disposed",
	"landing_delivered_by", "landing_rework_seen", "landing_replaced", "landing_batch", "landing_batch_red",
	"wave_opened", "wave_appended", "wave_append_refused", "wave_closed", "wave_landing", "wave_red", "wave_landed",
	"wave_abandoned", "wave_tally"}

// retiredStreams are the prefixes of the streams those records lived on.
var retiredStreams = []string{"landing:", "landing-cards:", "stg-sample:", "wave:"}

// retiredRecord refuses an event that would write one of them: the stream is
// retired, and nothing checks such a record any more.
func retiredRecord(ev Event) error {
	for _, prefix := range retiredStreams {
		if strings.HasPrefix(ev.Stream, prefix) {
			return invalid("stream %s is retired: the landing lane, the B5 STG release and waves ended on 2026-11-14", ev.Stream)
		}
	}
	if ev.Type != "note" {
		return nil
	}
	for _, name := range retiredNoteFields {
		if _, ok := ev.Body[name]; ok {
			return invalid("the %s record is retired: the landing lane, the B5 STG release and waves ended on 2026-11-14", name)
		}
	}
	return nil
}
