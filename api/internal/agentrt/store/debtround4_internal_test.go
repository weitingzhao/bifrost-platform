package store

import (
	"errors"
	"testing"
)

// R4-S1 (review 631d): the store refuses a debt event naming half a move,
// as the database does; whole or none is taken. Go's text check refuses a
// blank text, as the database's does (R4-M4).
func TestDebtEventNamesAMoveWhole(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]bool{"from": false, "to": false, "both": true, "neither": true} {
		b := map[string]any{"debt_id": "TDS-980", "debt_work_id": 980020}
		if body == "from" || body == "both" {
			b["from"] = "awaiting_signoff"
		}
		if body == "to" || body == "both" {
			b["to"] = "doing"
		}
		if err := checkBody("debt_work_linked", b); (err == nil) != want || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Errorf("%s: %v", body, err)
		}
	}
	for _, s := range []string{"", " ", "\t\n", "　"} {
		if err := checkDebtText("a text", s, 262144); err == nil {
			t.Errorf("the store took %q as a text", s)
		}
	}
}
