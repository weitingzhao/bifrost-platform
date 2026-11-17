package coord

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// WakePrefix starts every wake the bus's dispatcher posts (subscribe.Body);
// UndeliverablePrefix one it could not deliver and sent to the tenant's
// undeliverable_to instead.
const (
	WakePrefix          = "bus: "
	UndeliverablePrefix = "bus: a wake could not be delivered"
)

// WakeReadings is what the bus's wakes did since a time, read from the
// messages themselves: how many reached a seat's inbox, how many could not
// and went to the duty seat, and, of those the seat has acknowledged, how
// long from the message to its acknowledgement (a doorbell wakes the seat,
// which then reads and acknowledges).
type WakeReadings struct {
	Since         time.Time      `json:"since"`
	Delivered     int            `json:"delivered"`
	Undeliverable int            `json:"undeliverable"`
	Acked         int            `json:"acked"`
	AckMedianSecs float64        `json:"ack_median_seconds"`
	BySeat        map[string]int `json:"by_seat"`
}

// Wakes reads the bus's wakes since `since`.
func Wakes(ctx context.Context, db eventlog.DB, since time.Time) (WakeReadings, error) {
	out := WakeReadings{Since: since, BySeat: map[string]int{}}
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = WakeReadings{Since: since, BySeat: map[string]int{}}
		rows, err := tx.Query(ctx, `SELECT m.to_actor_id,
		    substring(b.body from 1 for octet_length(convert_to($3, 'UTF8'))) = convert_to($3, 'UTF8'), m.created_at, m.acked_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.from_actor_id = 'controller' AND m.created_at >= $1
		    AND substring(b.body from 1 for octet_length(convert_to($2, 'UTF8'))) = convert_to($2, 'UTF8')`, since, WakePrefix, UndeliverablePrefix)
		if err != nil {
			return err
		}
		defer rows.Close()
		var acks []float64
		for rows.Next() {
			var to string
			var rerouted bool
			var at time.Time
			var acked *time.Time
			if err := rows.Scan(&to, &rerouted, &at, &acked); err != nil {
				return err
			}
			if rerouted {
				out.Undeliverable++
				continue
			}
			out.Delivered++
			out.BySeat[to]++
			if acked != nil {
				out.Acked++
				acks = append(acks, acked.Sub(at).Seconds())
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		out.AckMedianSecs = median(acks)
		return nil
	})
	return out, err
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
