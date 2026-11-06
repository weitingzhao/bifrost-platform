package stgrelease

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// R5:5.7 B5 fix: the reason names the first part of WindowPolicy.enabled that
// failed, with the values read; enabled is exactly "no reason".
func TestWindowPolicyDisabledReason(t *testing.T) {
	t.Parallel()
	for mode, want := range map[string]map[string]any{
		"enabled":      nil,
		"stg":          {"part": "environment", "environment": "stg"},
		"live off":     {"part": "live", "live": false},
		"missing":      {"part": "no_rule"},
		"kind":         {"part": "kind_or_version", "kind": "issue", "landing": true, "rule_version": 1},
		"version":      {"part": "kind_or_version", "kind": string(rules.KindLanding), "landing": true, "rule_version": 0},
		"no landing":   {"part": "kind_or_version", "kind": string(rules.KindLanding), "landing": false, "rule_version": 1},
		"future":       {"part": "not_yet_effective", "effective_at": "2026-10-29T12:01:00Z", "at": "2026-10-29T12:00:00Z"},
		"expired":      {"part": "expired", "expires_at": "2026-10-29T12:00:00Z", "at": "2026-10-29T12:00:00Z"},
		"missing repo": {"part": "stg_release", "repo": false, "rule_version": 1},
		"none":         {"part": "stg_release", "repo": true, "stg_release": "none", "rule_version": 1},
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg, _, policy := leaseFixture()
			p := policy.Load()
			now := cfg.Clock.Now()
			switch mode {
			case "stg":
				p.Environment = "stg"
			case "live off":
				p.Live = false
			case "missing":
				p.Rule = nil
			case "kind":
				p.Rule.Kind = "issue"
			case "version":
				p.Rule.Version = 0
			case "no landing":
				p.Rule.Landing = nil
			case "future":
				p.Rule.Effective = now.Add(time.Minute)
			case "expired":
				p.Rule.Expires = now
			case "missing repo":
				p.Rule.Landing.Repos = nil
			case "none":
				p.Rule.Landing.Repos[0].STGRelease = "none"
			}
			got := p.disabled(now)
			if p.enabled(now) != (got == nil) {
				t.Fatal("enabled and its reason disagree", got)
			}
			if want == nil {
				if got != nil {
					t.Fatal("enabled policy has a reason", got)
				}
				return
			}
			if want["part"] != "environment" && want["part"] != "live" && want["part"] != "no_rule" {
				want["rule_id"] = p.Rule.ID
				if _, ok := want["rule_version"]; !ok {
					want["rule_version"] = p.Rule.Version
				}
			}
			if len(got) != len(want) {
				t.Fatalf("reason %v, want %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Fatalf("reason %v, want %v", got, want)
				}
			}
		})
	}
}

// A rule block opened on a misread records why and logs it once. It stays open
// while the policy reads disabled, then lifts on an enabled read of the same
// rule version, or of a newer one.
func TestControlRuleBlockReasonAndSameVersionLift(t *testing.T) {
	t.Parallel()
	for _, next := range []string{"same", "newer"} {
		t.Run(next, func(t *testing.T) {
			t.Parallel()
			c, l, _, _ := controlFixture(t, "Succeeded")
			l.entries = l.entries[:3] // OPEN with P selected
			var mu sync.Mutex
			var notices []string
			c.notice = func(msg string, args ...any) {
				mu.Lock()
				defer mu.Unlock()
				notices = append(notices, fmt.Sprint(append([]any{msg}, args...)...))
			}
			good := c.Policy()
			bad := good
			rule := *good.Rule
			rule.Expires = l.clock.Now().Add(-time.Second)
			bad.Rule = &rule
			var pmu sync.Mutex
			current := bad
			c.Policy = func() WindowPolicy { pmu.Lock(); defer pmu.Unlock(); return current }
			set := func(p WindowPolicy) { pmu.Lock(); current = p; pmu.Unlock() }

			pass(t, c)
			s := l.state(t)
			blocks := openBlocks(s)
			if len(blocks) != 1 {
				t.Fatal("no rule block", s.Blocks)
			}
			opened := blocks[0].Opened.Record
			reason := object(object(opened, "detail"), "rule_disabled")
			if opened.String("why") != "rule" || reason.String("part") != "expired" || reason.String("rule_id") != "landing:dev" || reason.Int("rule_version") != 1 || reason.String("expires_at") == "" {
				t.Fatal("rule block does not say why", opened)
			}
			for range 5 {
				l.clock.Step(time.Second)
				pass(t, c)
			}
			if len(openBlocks(l.state(t))) != 1 {
				t.Fatal("a disabled read lifted the rule block")
			}
			for _, r := range l.state(t).Records {
				if r.Record.String("kind") == "rule_observed" {
					t.Fatal("a disabled read was recorded as a rule observation", r.Record)
				}
			}
			mu.Lock()
			if len(notices) != 1 || !strings.Contains(notices[0], "expired") {
				t.Fatal("the rule block was not logged exactly once", notices)
			}
			mu.Unlock()

			lifted := good
			if next == "newer" {
				r := *good.Rule
				r.Version = 2
				lifted.Rule = &r
			}
			set(lifted)
			l.clock.Step(time.Second)
			pass(t, c)
			s = l.state(t)
			if len(openBlocks(s)) != 0 {
				t.Fatal("an enabled read did not lift the rule block")
			}
			block := s.Blocks[opened.String("block")]
			proof := s.Records[block.Closed.Record.Int("ref")].Record
			if block.Closed.Record.String("why") != "rule_changed" || proof.String("kind") != "rule_observed" || proof["enabled"] != true || proof.Int("rule_version") != int64(lifted.Rule.Version) {
				t.Fatal("rule block lifted without an enabled observation", block.Closed.Record, proof)
			}
		})
	}
}
