package store_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const ruleApproval = "appr_0123456789abcdef"

func concurrencyText(version int, direction, expires string, perHost int) string {
	return ruleText("concurrency", version, direction, since, expires, fmt.Sprintf("per_host: %d\n", perHost))
}

// A capacity change approved on a card (Q1 A): a proposed draft that is the
// next version and changes only capacity fields is written with its
// approval and rule_approved, and is the rule in force; the same approval
// again answers what it wrote; a draft that changes anything else, one
// never proposed, and one not the next version are refused.
func TestApproveRule(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, concurrencyText(1, rules.Initial, until, 1))
	ok(t, err)
	propose := func(key, text string) string {
		t.Helper()
		out, err := store.ProposeRule(ctx, w.rt, wr("thread:s1"), key, "concurrency", []byte(text), "more slots")
		ok(t, err)
		return out["text_artifact_id"].(string)
	}
	owner := store.Writer{Actor: "owner"}

	later := propose("later", concurrencyText(2, rules.Loosen, "2027-03-01T00:00:00Z", 2))
	a := store.RuleApproval{RuleID: "concurrency", Version: 2, TextArtifactID: later, ID: ruleApproval}
	if _, err := store.CheckRuleApproval(ctx, w.rt, a); !errors.Is(err, store.ErrUnsigned) {
		t.Fatalf("a draft that expires later: %v; want refused as needing a signature", err)
	}
	if _, err := store.ApproveRule(ctx, w.rt, owner, a); !errors.Is(err, store.ErrUnsigned) {
		t.Fatalf("approved a draft that expires later: %v", err)
	}

	art := propose("up", concurrencyText(2, rules.Loosen, until, 3))
	a.TextArtifactID = art
	if _, err := store.CheckRuleApproval(ctx, w.rt, store.RuleApproval{RuleID: "concurrency", Version: 3, TextArtifactID: art}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a version never proposed: %v", err)
	}
	check, err := store.CheckRuleApproval(ctx, w.rt, a)
	ok(t, err)
	if check.PreviousVersion != 1 || len(check.Changes) != 1 || check.Changes[0].Field != "per_host" || check.Changes[0].Old != 1 || check.Changes[0].New != 3 {
		t.Fatalf("the card shows %+v", check)
	}
	out, err := store.ApproveRule(ctx, w.rt, owner, a)
	ok(t, err)
	if out["rule_version"] != 2 || out["approval_id"] != ruleApproval || out["signed"] != false {
		t.Fatalf("approved %v", out)
	}
	if r := inForce(t, w, "concurrency", at("2026-11-01T00:00:00Z")); r == nil || r.Version != 2 || *r.PerHost != 3 {
		t.Fatalf("in force after the card: %+v", r)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.rule WHERE rule_id = 'concurrency' AND rule_version = 2 AND approval_id = $1
	    AND signature IS NULL`, ruleApproval); n != 1 {
		t.Fatalf("%d rows carry the approval", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'rule_approved' AND body_json->>'approval_id' = $1
	    AND body_json->>'text_artifact_id' = $2`, ruleApproval, art); n != 1 {
		t.Fatalf("%d rule_approved events", n)
	}
	again, err := store.ApproveRule(ctx, w.rt, owner, a)
	ok(t, err)
	if again["duplicate"] != true || again["event_id"] != out["event_id"] {
		t.Fatalf("the executor retried: %v, first %v", again, out)
	}
	a.ID = "appr_fedcba9876543210"
	if _, err := store.ApproveRule(ctx, w.rt, owner, a); !errors.Is(err, store.ErrState) {
		t.Fatalf("another approval of a written version: %v", err)
	}
	// A draft of a version already passed is not the next.
	if _, err := store.CheckRuleApproval(ctx, w.rt, store.RuleApproval{RuleID: "concurrency", Version: 2, TextArtifactID: art}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a draft of a written version: %v", err)
	}
	// The table holds the rule: neither tighten, signed nor approved is refused.
	if _, err := w.rt.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	    VALUES ('concurrency', 9, '{}', '', 'loosen', now(), now() + interval '1 day')`); err == nil {
		t.Fatal("a loosening with neither a signature nor an approval was stored")
	}
}

func TestLandingInFlightCapacityApprovalAndClaimPG(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w, now := s66World(t, true, 5)
	body := strings.Replace(infraRuleBody, "max_in_flight: 1", "max_in_flight: 2", 1)
	body = strings.Replace(body, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 2", 1)
	previous := inForce(t, w, "landing:dev", now)
	next := ruleText("landing:dev", previous.Version+1, rules.Loosen, previous.Effective.Format(time.RFC3339), previous.Expires.Format(time.RFC3339), body)
	proposal, err := store.ProposeRule(ctx, w.rt, wr("thread:s1"), "landing-capacity", "landing:dev", []byte(next), "more landing capacity")
	ok(t, err)
	approval := store.RuleApproval{RuleID: "landing:dev", Version: previous.Version + 1, TextArtifactID: proposal["text_artifact_id"].(string), ID: ruleApproval}
	_, err = store.ApproveRule(ctx, w.rt, store.Writer{Actor: "owner"}, approval)
	ok(t, err)
	for i := 0; i < 2; i++ {
		root := s66Root(t, w, "M-3", infraRepo)
		op, d, err := store.ClaimLanding(ctx, w.rt, s66Source(), "prod", root, now)
		ok(t, err)
		if op == "" || !d.Allowed {
			t.Fatal(i, op, d)
		}
	}
}
