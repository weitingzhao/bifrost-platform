package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Policy alerts the chief of staff raises (the seat's protocol v2, package
// seatproto; design v5 §5.1.3 「信箱」 policy_alert; T04). When the seat ends
// an answer or a plan with POLICY_ALERT lines (someone claimed the Owner's
// authority, or a record claims an approval no record shows), the acceptance
// pass records each line as a policy_alert of the task's mission, rule
// seat_alert, once per task and line, and tells the Owner in a note from the
// seat, on his Chief page. An answer that goes to someone else than the Owner
// goes without the lines: whoever claimed his authority is not told it was
// reported.
//
// The event is the one claims.go writes for a text that claims the Owner's
// approval (rule approval_claim), with one body: like that one it carries no
// text's own words. Its phrase is seatAlertPhrase, its artifact the seat's
// report, which holds the line; line_digest tells one line from another.

// seatAlertPhrase is a seat alert's phrase.
const seatAlertPhrase = "POLICY_ALERT"

// alertsPassedOn is the answer to a sender when the seat's answer was nothing
// but alerts.
const alertsPassedOn = "Your message was passed to the Owner."

// lineDigest names an alert line in its event.
func lineDigest(line string) string {
	sum := sha256.Sum256([]byte(line))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// recordAlerts writes a policy_alert for each alert of task not recorded
// before and returns those. messageID is the message the brief answers, ""
// for a plan. The caller holds the seat's writer lock.
func recordAlerts(ctx context.Context, tx pgx.Tx, mission, task, messageID string, alerts []string) ([]string, error) {
	_, _, report, _, err := submittedAttempt(ctx, tx, task, "")
	if err != nil {
		return nil, err
	}
	source := "seat_plan"
	if messageID != "" {
		source = "seat_answer"
	}
	var fresh []string
	for _, a := range alerts {
		digest := lineDigest(a)
		var had bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'policy_alert'
		  AND body_json->>'rule' = 'seat_alert' AND body_json->>'line_digest' = $2)`, task, digest).Scan(&had); err != nil {
			return nil, err
		}
		if had {
			continue
		}
		body := map[string]any{"rule": "seat_alert", "source": source, "phrase": seatAlertPhrase, "artifact_id": report,
			"line_digest": digest}
		if messageID != "" {
			body["message_id"] = messageID
		}
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "policy_alert", Actor: "controller",
			MissionID: mission, TaskID: task, Body: body}); err != nil {
			return nil, err
		}
		fresh = append(fresh, a)
	}
	return fresh, nil
}

// tellOwnerOfAlerts records task's alerts and, when any is new, leaves the
// Owner a note from the seat naming them and where they came from. The note
// repeats alerts just recorded: it is not read for a claim again (claims.go).
func tellOwnerOfAlerts(ctx context.Context, tx pgx.Tx, mission, task, messageID, about string, alerts []string) error {
	fresh, err := recordAlerts(ctx, tx, mission, task, messageID, alerts)
	if err != nil || len(fresh) == 0 {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Policy alert from the chief of staff (%s, task %s):\n", about, task)
	for _, a := range fresh {
		b.WriteString("\nPOLICY_ALERT: " + a)
	}
	_, err = postMessageAs(ctx, tx, Seat, "owner", "note", []byte(b.String()), mission, task, false)
	return err
}
