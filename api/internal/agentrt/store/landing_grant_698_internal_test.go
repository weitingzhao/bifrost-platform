package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Regressions of code review 698 (W-31 core C-5) on the production
// functions, with row doubles; the database cases are in
// landing_grant_698_test.go.

// rowTx answers QueryRow with row, everything else as waitReviewTx.
type rowTx struct {
	waitReviewTx
	row     func(sql string, dest ...any) error
	queries int
}

func (tx *rowTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queries++
	return funcRow(func(dest ...any) error { return tx.row(sql, dest...) })
}

var key698 = GrantCIKey{Repo: "bifrost-platform", Revision: strings.Repeat("b", 40), Base: strings.Repeat("a", 40),
	Siblings: map[string]string{"bifrost-ui": strings.Repeat("c", 40), "bifrost-trade-infra": strings.Repeat("d", 40)}}

// B-01: the first pinned key may ride on a rerun (the first run was the
// webhook's); once recorded it never changes. (On 082125dd a first key on
// ci_rerun was refused: "fixed at the first kick".)
func TestC5B01FirstPinnedKeyMayRideOnARerun(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	entries := reviewWaitEntries(t, at.Add(-time.Hour), at.Add(-time.Minute))[:4]
	op := "land:t_00000000000000000000000001:1"
	event := func(kind string, k GrantCIKey) Event {
		return Event{Stream: landingRepoStream, Type: "note", Actor: "controller", Body: map[string]any{
			"landing":        map[string]any{"v": landing.Version, "kind": kind, "op": op, "try": map[string]int{"ci_kick": 1, "ci_rerun": 2}[kind]},
			"landing_ci_key": toBody(landingCIKeyRecord{V: "1", Op: op, GrantCIKey: k})}}
	}
	none := &rowTx{waitReviewTx: waitReviewTx{entries: entries}, row: func(string, ...any) error { return pgx.ErrNoRows }}
	for _, kind := range []string{"ci_kick", "ci_rerun"} {
		if err := checkLandingCIKeyAppend(context.Background(), none, event(kind, key698)); err != nil {
			t.Errorf("first key on %s: %v", kind, err)
		}
	}
	prior, _ := json.Marshal(toBody(landingCIKeyRecord{V: "1", Op: op, GrantCIKey: key698}))
	kept := &rowTx{waitReviewTx: waitReviewTx{entries: entries}, row: func(_ string, dest ...any) error { *dest[0].(*[]byte) = prior; return nil }}
	if err := checkLandingCIKeyAppend(context.Background(), kept, event("ci_rerun", key698)); err != nil {
		t.Errorf("the same key again: %v", err)
	}
	other := key698
	other.Siblings = map[string]string{"bifrost-ui": strings.Repeat("e", 40), "bifrost-trade-infra": strings.Repeat("d", 40)}
	if err := checkLandingCIKeyAppend(context.Background(), kept, event("ci_rerun", other)); err == nil {
		t.Error("a second, different key accepted")
	}
}

// N-02: obvious placeholders are no answer; the full answers still are.
// (On 082125dd all three placeholders were accepted.)
func TestC5N02TrustPlaceholders(t *testing.T) {
	t.Parallel()
	body := func(why string) []byte {
		b := "## Trust chain answers\n"
		for _, id := range []string{"1", "2", "3", "4"} {
			b += "T" + id + ": No, " + why + "\n"
		}
		return []byte(b)
	}
	for _, why := range []string{"[explain what in the diff shows it]", "TBD TBD TBD TBD TBD", "the token is still read from ...",
		"the token is still read from the same file", "todo fill this in later please", "same same same same same"} {
		if trustAnswered(body(why)) {
			t.Errorf("placeholder accepted: %s", why)
		}
	}
	if !trustAnswered(body("the approval roles in approvals/service.go are untouched")) {
		t.Error("a real answer refused")
	}
}

// N-03: a rule_clear is the final gate's own certificate: without its
// landing_gate, on a rule version or text that does not exist, or other
// checks, it is refused; the gate's own passes. (On 082125dd an invented
// rule v999 with checks 999 and no landing_gate was accepted.)
func TestC5N03RuleClearIsTheGatesOwn(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	entries := reviewWaitEntries(t, at.Add(-time.Hour), at.Add(-time.Minute))
	op := "land:t_00000000000000000000000001:1"
	decided := at.Format(time.RFC3339Nano)
	sum := strings.Repeat("f", 64)
	event := func(version int64, sha string, checks int64, gate bool) Event {
		body := map[string]any{
			"landing": map[string]any{"v": landing.Version, "kind": "main_intent", "op": op, "R": strings.Repeat("b", 40), "M0": strings.Repeat("a", 40)},
			"landing_rule_clear": toBody(LandingRuleClear{V: "1", Op: op, Rule: GrantRule{ID: "landing:dev", Version: version, SHA256: sha},
				ChecksVersion: checks, DecidedAt: decided})}
		if gate {
			body["landing_gate"] = map[string]any{"v": LandingGateVersion, "op": op, "rule_version": version, "decided_at": decided,
				"quota_day": at.Format("2006-01-02"), "ci_run": "green", "ci_created_at": decided}
		}
		return Event{Stream: landingRepoStream, Type: "note", Actor: "controller", Body: body}
	}
	tx := &rowTx{waitReviewTx: waitReviewTx{entries: entries}, row: func(sql string, dest ...any) error {
		if strings.Contains(sql, "FROM agentrt.rule") {
			*dest[0].(*string) = sum
			return nil
		}
		return pgx.ErrNoRows
	}}
	missing := &rowTx{waitReviewTx: waitReviewTx{entries: entries}, row: func(string, ...any) error { return pgx.ErrNoRows }}
	if err := checkLandingRuleClearAppend(context.Background(), tx, event(1, sum, rules.LandingChecksVersion, true), at); err != nil {
		t.Fatal("the gate's own certificate:", err)
	}
	for name, c := range map[string]struct {
		tx pgx.Tx
		ev Event
	}{
		"no landing_gate":   {tx, event(1, sum, rules.LandingChecksVersion, false)},
		"invented v999":     {missing, event(999, sum, rules.LandingChecksVersion, true)},
		"another rule text": {tx, event(1, strings.Repeat("e", 64), rules.LandingChecksVersion, true)},
		"other checks":      {tx, event(1, sum, 999, true)},
	} {
		if err := checkLandingRuleClearAppend(context.Background(), c.tx, c.ev, at); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// R-05: pinnedReview reads its subject once, pinned or not (on 082125dd an
// unpinned review made a second read for the trust chain's questions).
func TestC5R05ReviewReadsItsSubjectOnce(t *testing.T) {
	t.Parallel()
	tx := &rowTx{row: func(sql string, dest ...any) error {
		if !strings.Contains(sql, "SELECT COALESCE(executor_pin, '')") || len(dest) < 5 {
			t.Fatalf("unexpected read: %s", sql)
		}
		*dest[0].(*string), *dest[1].(*bool), *dest[2].(*string), *dest[3].(*string), *dest[4].(*string) = "", true, "bifrost-platform", "implement", "dev"
		return nil
	}}
	spec := checkSpec{check: "review", attempt: "a"}
	if _, err := pinnedReview(context.Background(), tx, "t", &spec); err != nil {
		t.Fatal(err)
	}
	if tx.queries != 1 || spec.text != "" {
		t.Fatalf("%d reads, text %q", tx.queries, spec.text)
	}
}
