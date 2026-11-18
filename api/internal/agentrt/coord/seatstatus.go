package coord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A seat's status is a message it sends with the existing message route
// (2026-11-17, the coordinator after card #587: no new write route, no list
// changed, no migration): a typed envelope of type "status" to the status
// board, its text the status as JSON. The board reads each seat's latest
// one; the fields are checked here, on read, and a status that does not
// parse is shown as such, naming its sender. The Owner reads where every
// line stands on Ops instead of asking the coordinator.

// StatusBoard is the fixed recipient of statuses: the runtime's controller,
// a registered actor whose inbox nobody reads (no route registers a new
// seat; seat actors come from the seed).
const StatusBoard = "controller"

// MaxSeatStatusField is the longest text of one field, in bytes.
const MaxSeatStatusField = 600

// SeatStatus is what a seat says of its line: the five things the Owner
// asks (11-15) and how its percent is counted.
type SeatStatus struct {
	Goal         string `json:"goal"`
	Doing        string `json:"doing"`
	Percent      int    `json:"percent"`
	PercentBasis string `json:"percent_basis"`
	WaitingOn    string `json:"waiting_on,omitempty"`
	Next         string `json:"next"`
	// Charter is the seat this session says it is: the name of its charter
	// (agent-config/charters/<name>.md), declared by the session and shown as
	// its own statement. Empty for a session that does not say.
	Charter string `json:"charter,omitempty"`
}

// SeatStatusRecord is a seat's latest status message as the board reads it.
// Problem is set when the message is not a status of the expected form; the
// fields are then empty and Summary is what the seat wrote.
type SeatStatusRecord struct {
	Seat      string    `json:"seat"`
	MessageID string    `json:"message_id"`
	At        time.Time `json:"at"`
	Summary   string    `json:"summary,omitempty"`
	Problem   string    `json:"problem,omitempty"`
	SeatStatus
}

// CheckSeatStatus says why s is not a status ("" when it is one).
func CheckSeatStatus(s SeatStatus) string {
	for _, f := range []struct{ name, v string }{{"goal", s.Goal}, {"doing", s.Doing}, {"percent_basis", s.PercentBasis}, {"next", s.Next}} {
		if strings.TrimSpace(f.v) == "" {
			return "no " + f.name
		}
	}
	for _, f := range []struct{ name, v string }{{"goal", s.Goal}, {"doing", s.Doing}, {"percent_basis", s.PercentBasis}, {"next", s.Next}, {"waiting_on", s.WaitingOn}} {
		if len(f.v) > MaxSeatStatusField || !utf8.ValidString(f.v) || strings.IndexByte(f.v, 0) >= 0 {
			return fmt.Sprintf("%s is not UTF-8 text of at most %d bytes", f.name, MaxSeatStatusField)
		}
	}
	if s.Charter != "" && !charterRE.MatchString(s.Charter) {
		return "charter is not a charter's name"
	}
	if s.Percent < 0 || s.Percent > 100 {
		return "percent is not 0 to 100"
	}
	return ""
}

// ParseSeatStatus reads a status message's body: the envelope, then its
// text. A body that is a message but not a status answers ok false.
func ParseSeatStatus(body []byte) (summary string, s SeatStatus, problem string, ok bool) {
	var env struct {
		V       int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	}
	if json.Unmarshal(body, &env) != nil || env.Type != "status" {
		return "", SeatStatus{}, "", false
	}
	dec := json.NewDecoder(strings.NewReader(env.Text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return env.Summary, SeatStatus{}, "format: the text is not the status fields (" + err.Error() + ")", true
	}
	if p := CheckSeatStatus(s); p != "" {
		return env.Summary, SeatStatus{}, "format: " + p, true
	}
	return env.Summary, s, "", true
}

// SeatStatuses is each sender's latest status message to the board since
// `since`, newest first.
func SeatStatuses(ctx context.Context, db eventlog.DB, since time.Time) ([]SeatStatusRecord, error) {
	var out []SeatStatusRecord
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		rows, err := tx.Query(ctx, `SELECT m.message_id, m.from_actor_id, b.body, m.created_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.to_actor_id = $1 AND m.message_kind = 'note' AND m.created_at >= $2
		  ORDER BY m.created_at DESC, m.message_id DESC`, StatusBoard, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var r SeatStatusRecord
			var body []byte
			if err := rows.Scan(&r.MessageID, &r.Seat, &body, &r.At); err != nil {
				return err
			}
			if seen[r.Seat] {
				continue
			}
			summary, s, problem, ok := ParseSeatStatus(body)
			if !ok {
				continue
			}
			seen[r.Seat] = true
			r.Summary, r.SeatStatus, r.Problem = summary, s, problem
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
