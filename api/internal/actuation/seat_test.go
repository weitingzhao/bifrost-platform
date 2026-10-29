package actuation

import (
	"errors"
	"strings"
	"testing"
)

// A seat is one token (DESIGN-coordinator-landing-v3 §2.1): two entries
// named for the same seat are refused at load, even when one of them has no
// value configured; two seats, and a seat beside other tokens, load.
func TestSeatTokenNamedOnce(t *testing.T) {
	t.Parallel()
	two := `
tokens:
  - name: seat:coordinator
    role: reporter
    token: fixture-seat-a
  - name: seat:chief_of_staff
    role: reporter
    token: fixture-seat-b
  - name: operator
    role: operator
    token: fixture-operator
`
	auth, err := LoadAuth(writeAuthFile(t, two))
	if err != nil || auth == nil {
		t.Fatalf("two seats: %v", err)
	}
	for name, body := range map[string]string{
		"a second value": two + `  - name: seat:coordinator
    role: reporter
    token: fixture-seat-c
`,
		"a second entry with no value": two + `  - name: seat:coordinator
    role: reporter
    token_env: AGENTRT_TEST_UNSET_SEAT_TOKEN
`,
	} {
		_, err := LoadAuth(writeAuthFile(t, body))
		if !errors.Is(err, ErrDuplicateSeat) || !strings.Contains(err.Error(), `seat token "seat:coordinator" is named twice`) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
