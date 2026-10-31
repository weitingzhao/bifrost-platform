package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// B4 (S5 design §9): a landing rule that enrolls an S5 path is signed only
// once the deployment's trusted S5 contract is usable. Without that order
// the rule turns every platform .tsx display change into an S5 lane that can
// only answer S5_EVIDENCE_MISSING (review 2026-10-30, TSX under v3+UNBUILT).
// Serial: replaces the package-level S5SigningReady.
func TestSignRuleRefusesS5EnrollmentBeforeTrustedContract(t *testing.T) {
	text := func(allow ...string) []byte {
		return []byte("rule_id: landing:dev\nrule_version: 3\ndirection: loosen\neffective_at: 2026-10-29T00:00:00Z\nexpires_at: 2026-11-28T00:00:00Z\n" +
			"tenant: dev\nrepos:\n  - repo: bifrost-platform\n    allow: [\"" + strings.Join(allow, "\", \"") + "\"]\n    deny: []\n    stg_release: none\n" +
			"limits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 12, max_in_flight_per_repo: 1}\n")
	}
	display := []string{"console/src/components/**/*.tsx", "console/src/pages/**/*.tsx"}
	enrolled := append(append([]string{}, display...), rules.S5Paths()...)
	if r, err := rules.Parse(text(enrolled...)); err != nil || len(rules.S5Enrolled(r)) != len(rules.S5Paths()) {
		t.Fatal("fixture rule", err)
	}
	// A deny or a broader glob that does not name the path changes nothing.
	if r, err := rules.Parse(text("console/src/lib/**/*.ts")); err != nil || len(rules.S5Enrolled(r)) != len(rules.S5Paths()) {
		t.Fatal("glob enrollment not seen", err)
	}
	saved := S5SigningReady
	t.Cleanup(func() { S5SigningReady = saved })
	sign := func(body []byte) error {
		_, err := SignRule(context.Background(), nil, Writer{Actor: "owner", Token: "admin"}, "landing:dev", body, "not a signature", "SHA256:anchor")
		return err
	}
	for name, ready := range map[string]func(context.Context) error{
		"unwired": nil,
		"UNBUILT": func(context.Context) error { return errors.New("S5_CI_IDENTITY: checker image UNBUILT") },
	} {
		S5SigningReady = ready
		if err := sign(text(enrolled...)); err == nil || errors.Is(err, ErrUnsigned) || !strings.Contains(err.Error(), "S5") {
			t.Fatal(name, "S5 rule reached the signature", err)
		}
		// Rules without an S5 path are untouched (they fail on the bad
		// signature only).
		if err := sign(text(display...)); !errors.Is(err, ErrUnsigned) {
			t.Fatal(name, "display rule", err)
		}
	}
	S5SigningReady = func(context.Context) error { return nil }
	if err := sign(text(enrolled...)); !errors.Is(err, ErrUnsigned) {
		t.Fatal("deployable contract still refused", err)
	}
}
