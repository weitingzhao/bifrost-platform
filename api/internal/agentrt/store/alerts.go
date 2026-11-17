package store

import (
	"crypto/sha256"
	"encoding/hex"
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
// The event is the one coord/claims.go writes for a text that claims the Owner's
// approval (rule approval_claim), with one body: like that one it carries no
// text's own words. Its phrase is seatAlertPhrase, its artifact the seat's
// report, which holds the line; line_digest tells one line from another.

// seatAlertPhrase is a seat alert's phrase.
const seatAlertPhrase = "POLICY_ALERT"

// lineDigest names an alert line in its event.
func lineDigest(line string) string {
	sum := sha256.Sum256([]byte(line))
	return "sha256:" + hex.EncodeToString(sum[:])
}
