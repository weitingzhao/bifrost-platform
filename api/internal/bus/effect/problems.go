package effect

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
)

// ProblemEvidence is evidence for the problem channels. Some topics are shared,
// and their traffic says nothing about one channel, so a channel can name its
// own proof: "alert:<name>" and "stream:<id>" are the last event of a stream,
// "source:<type>:<source>" the last event of that type from that source. A
// channel's drill key, "drill:<id>", is the last event of its drill stream
// (bus/drill): shown apart, never counted as a run.
// Anything else goes to base. drillStream names a drill's stream.
func ProblemEvidence(ledger func() eventlog.DB, drillStream func(string) string) func(ctx context.Context, now time.Time, base func(string) Evidence) func(string) Evidence {
	return func(ctx context.Context, now time.Time, base func(string) Evidence) func(string) Evidence {
		return func(id string) Evidence {
			kind, rest, _ := strings.Cut(id, ":")
			var read func(tx pgx.Tx) (eventlog.Row, bool, error)
			switch kind {
			case "drill":
				// Published by the relay under its own stream's name (relay.Pass).
				return base("topic:" + drillStream(rest))
			case "alert", "stream":
				stream := rest
				if kind == "alert" {
					stream = busalerts.Stream(rest)
				}
				read = func(tx pgx.Tx) (eventlog.Row, bool, error) { return eventlog.Last(ctx, tx, stream) }
			case "source":
				typ, source, _ := strings.Cut(rest, ":")
				read = func(tx pgx.Tx) (eventlog.Row, bool, error) {
					return eventlog.LatestWithField(ctx, tx, typ, "source", source, now.Add(-EvidenceWindow))
				}
			default:
				return base(id)
			}
			db := ledger()
			if db == nil {
				return Evidence{State: EvidenceUnmeasured}
			}
			var last float64
			err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
				r, found, err := read(tx)
				if found {
					last = float64(r.At.Unix())
				}
				return err
			})
			if err != nil {
				return Evidence{State: EvidenceUnmeasured}
			}
			return JudgeEvidence(now, last, 1)
		}
	}
}
