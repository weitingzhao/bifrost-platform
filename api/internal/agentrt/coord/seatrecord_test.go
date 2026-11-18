package coord_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, the seat record, built without a new table on the status
// messages: the record must say what the board says, a malformed status must
// write none, and a new session naming a charter takes its binding over.
func TestTheSeatRecordFollowsTheStatusMessagesPG(t *testing.T) {
	t.Parallel()
	_, pool, a := conclusionDB(t)
	b := actor.Writer{Actor: "thread:local_other", Token: "other"}
	s := coord.SeatStatus{Goal: "bus", Doing: "records", Percent: 40, PercentBasis: "2 of 5", Next: "release", Charter: "8-2-comm-desk"}
	send := func(w actor.Writer, body []byte) {
		t.Helper()
		_, err := coord.SendMessage(ctx, pool, w, coord.StatusBoard, "note", body)
		ok2(t, err)
	}
	send(a, statusBody(t, "first", s))
	s.Doing = "reads"
	send(a, statusBody(t, "second", s))
	send(b, statusBody(t, "bad", "not a status"))
	since := time.Now().Add(-time.Hour)
	board, err := coord.SeatStatuses(ctx, pool, since)
	ok2(t, err)
	records, err := coord.SeatRecords(ctx, pool, since)
	ok2(t, err)
	if len(records) != 1 || records[0].Seat != a.Actor || records[0].Doing != "reads" || records[0].Charter != "8-2-comm-desk" {
		t.Fatalf("records: %+v", records)
	}
	if diff := coord.SeatRecordDiff(board, records); len(diff) != 0 {
		t.Fatalf("the record differs from the board: %v", diff)
	}
	// The positive control: a record that is not the board's latest is named.
	stale := records[0]
	stale.MessageID = "m_other"
	if diff := coord.SeatRecordDiff(board, []coord.SeatStatusRecord{stale}); len(diff) != 1 {
		t.Fatalf("a stale record was not named: %v", diff)
	}
	sessions := func() []string {
		t.Helper()
		var out []string
		ok2(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			out = nil
			rows, err := tx.Query(ctx, `SELECT body_json->>'session' FROM agentrt.event WHERE event_type = $1 ORDER BY event_id`, coord.SeatSessionType)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}))
		return out
	}
	if got := sessions(); len(got) != 1 || got[0] != a.Actor {
		t.Fatalf("one binding for two statuses of one session: %v", got)
	}
	send(b, statusBody(t, "takes over", s))
	if got := sessions(); len(got) != 2 || got[1] != b.Actor {
		t.Fatalf("the new session did not take the binding: %v", got)
	}
}

func TestTheSeatEventsKeepTheirContract(t *testing.T) {
	for typ, body := range map[string]map[string]any{
		coord.SeatStatusType:  coord.SeatStatusBody("thread:local_a", "m_1"),
		coord.SeatSessionType: coord.SeatSessionBody("8-2-comm-desk", "thread:local_a"),
	} {
		ok2(t, eventlog.CheckBody(typ, body))
		ok2(t, registry.Agree(typ))
	}
	if p := coord.CheckSeatStatus(coord.SeatStatus{Goal: "g", Doing: "d", PercentBasis: "b", Next: "n", Charter: "Not A Name"}); p == "" {
		t.Error("a charter that is no charter's name was taken")
	}
}
