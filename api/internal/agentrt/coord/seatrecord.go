package coord

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The seat record (2026-11-18, the coordinator chose the design without a new
// table): a seat's status message stays where it is, and the ledger gets one
// event per status on the seat's own stream, pointing at that message. The
// stream's sequence is the record's version, and the event is what reaches the
// bus. A status naming a charter also binds the seat to the session that sent
// it; a later session naming the same charter takes the binding over.

// The events.
const (
	SeatStatusType  = "seat.status.changed"
	SeatSessionType = "seat.session.changed"
)

// seatRecordRef is where a seat's record is read.
const seatRecordRef = "/api/v1/agentrt/seats"

var charterRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// SeatStatusBody is the event's body: it points at the message.
func SeatStatusBody(from, messageID string) map[string]any {
	return map[string]any{"seat": from, "message_id": messageID, "subject": from, "key": from, "ref": seatRecordRef, "change": "status"}
}

// SeatSessionBody is the binding's event body.
func SeatSessionBody(charter, session string) map[string]any {
	return map[string]any{"seat": charter, "session": session, "subject": charter + "/" + session, "ref": seatRecordRef, "change": "bound"}
}

// announceSeatStatus writes the events of a status message; a message that is
// not a well-formed status writes none.
func announceSeatStatus(ctx context.Context, tx pgx.Tx, from, messageID string, body []byte) error {
	_, s, problem, ok := ParseSeatStatus(body)
	if !ok || problem != "" || !mailToRE.MatchString(from) {
		return nil
	}
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "seat-status:" + from, Type: SeatStatusType, Actor: from,
		Body: SeatStatusBody(from, messageID)}); err != nil {
		return err
	}
	if s.Charter == "" {
		return nil
	}
	stream := "seat-binding:" + s.Charter
	last, found, err := eventlog.Last(ctx, tx, stream)
	if err != nil {
		return err
	}
	if found && last.Body["session"] == from {
		return nil
	}
	_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: SeatSessionType, Actor: from, Body: SeatSessionBody(s.Charter, from)})
	return err
}

// SeatRecords is each seat's latest status through its record events since
// `since`, newest first, in the board's form.
func SeatRecords(ctx context.Context, db eventlog.DB, since time.Time) ([]SeatStatusRecord, error) {
	var out []SeatStatusRecord
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		latest, err := eventlog.LatestByActor(ctx, tx, SeatStatusType, since)
		if err != nil {
			return err
		}
		for _, ev := range latest {
			var r SeatStatusRecord
			var body []byte
			id, _ := ev.Body["message_id"].(string)
			err := tx.QueryRow(ctx, `SELECT m.message_id, m.from_actor_id, b.body, m.created_at
			  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
			  WHERE m.message_id = $1`, id).Scan(&r.MessageID, &r.Seat, &body, &r.At)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			summary, s, problem, ok := ParseSeatStatus(body)
			if !ok {
				continue
			}
			r.Summary, r.SeatStatus, r.Problem = summary, s, problem
			out = append(out, r)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].MessageID > out[j].MessageID
	})
	return out, err
}

// SeatRecordDiff names the seats whose record is not the board's latest
// status. A seat with no record yet (it has not reported since the records
// began) or whose latest status is malformed, which writes no record, is not
// a difference.
func SeatRecordDiff(board, records []SeatStatusRecord) []string {
	latest := map[string]SeatStatusRecord{}
	for _, r := range board {
		latest[r.Seat] = r
	}
	var diff []string
	for _, r := range records {
		b, ok := latest[r.Seat]
		if !ok || (b.MessageID != r.MessageID && b.Problem == "") {
			diff = append(diff, r.Seat)
		}
	}
	slices.Sort(diff)
	return diff
}

func init() {
	field := func(names ...string) []eventlog.Field {
		out := make([]eventlog.Field, 0, len(names))
		for _, n := range names {
			out = append(out, eventlog.Field{Name: n, Kind: eventlog.Str})
		}
		return out
	}
	str := func(b map[string]any, k string) string { v, _ := b[k].(string); return v }
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			SeatStatusType:  field("seat", "message_id", "subject", "key", "ref", "change"),
			SeatSessionType: field("seat", "session", "subject", "ref", "change"),
		},
		Values: map[string]map[string]eventlog.Value{
			SeatStatusType:  {"seat": eventlog.Shaped("an actor address", mailToRE)},
			SeatSessionType: {"seat": eventlog.Shaped("a charter name", charterRE), "session": eventlog.Shaped("an actor address", mailToRE)},
		},
		After: map[string]func(map[string]any) error{
			SeatStatusType: func(b map[string]any) error {
				if b["subject"] != str(b, "seat") || b["key"] != str(b, "seat") || b["ref"] != seatRecordRef || b["change"] != "status" {
					return eventlog.Invalid("%s: subject, key, ref and change are fixed by the seat", SeatStatusType)
				}
				return nil
			},
			SeatSessionType: func(b map[string]any) error {
				if b["subject"] != str(b, "seat")+"/"+str(b, "session") || b["ref"] != seatRecordRef || b["change"] != "bound" {
					return eventlog.Invalid("%s: subject, ref and change are fixed by the binding", SeatSessionType)
				}
				return nil
			},
		},
	})
}
