package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func TestDeliveryBoundariesVersionsAndPreview(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)
	_, err := s.sign(w, ruleText("concurrency", 1, rules.Initial, past, expires, "per_host: 3\n"))
	ok(t, err)
	doc, err := store.ReadDeliveryBoundaries(ctx, w.rt, 0, time.Time{})
	ok(t, err)
	if len(doc.Tuning) != len(rules.TuningKeys()) || doc.DiskFloor != store.DefaultDiskFloor {
		t.Fatalf("defaults: %+v", doc)
	}
	for _, v := range doc.Tuning {
		if v.Value != v.Default || v.Source != "code_default" {
			t.Fatalf("default: %+v", v)
		}
	}
	var row store.BoundaryRule
	for _, r := range doc.Rules {
		if r.ID == "concurrency" {
			row = r
		}
	}
	if row.Current["rule_version"] != 1 || row.Draft["rule_version"] != 2 || row.Latest["rule_version"] != 1 {
		t.Fatalf("versions: %+v", row)
	}
	// The UI's JSON draft must parse identically to the rule's YAML text.
	row.Draft["per_host"] = 5
	b, err := json.Marshal(row.Draft)
	ok(t, err)
	preview, err := store.CheckRule(ctx, w.rt, "concurrency", b)
	ok(t, err)
	if preview["apply_via"] != "capacity_approval" {
		t.Fatalf("capacity: %v", preview)
	}
	row.Draft["expires_at"] = now.Add(48 * time.Hour).Format(time.RFC3339)
	b, err = json.Marshal(row.Draft)
	ok(t, err)
	preview, err = store.CheckRule(ctx, w.rt, "concurrency", b)
	ok(t, err)
	if preview["apply_via"] != "signature" {
		t.Fatalf("authority: %v", preview)
	}
	_, err = s.sign(w, ruleText("concurrency", 2, rules.Loosen, future, expires, "per_host: 5\n"))
	ok(t, err)
	doc, err = store.ReadDeliveryBoundaries(ctx, w.rt, 12345, time.Time{})
	ok(t, err)
	for _, r := range doc.Rules {
		if r.ID == "concurrency" {
			row = r
		}
	}
	if row.Current["rule_version"] != 1 || row.Latest["rule_version"] != 2 || row.Draft["rule_version"] != 3 || doc.DiskFloor != 12345 {
		t.Fatalf("scheduled version is not effective: %+v", row)
	}
	_, err = store.CheckRule(ctx, w.rt, "concurrency", b)
	wantErr(t, err, "at version 2")
	_, err = s.sign(w, ruleText("concurrency", 3, rules.Loosen, past, now.Add(-time.Minute).Format(time.RFC3339), "per_host: 5\n"))
	ok(t, err)
	doc, err = store.ReadDeliveryBoundaries(ctx, w.rt, 0, time.Time{})
	ok(t, err)
	for _, r := range doc.Rules {
		if r.ID == "concurrency" && (r.Current != nil || r.Status != "not_in_force") {
			t.Fatalf("expired rule fell back: %+v", r)
		}
	}
	landing := ruleText("landing:dev", 1, rules.Initial, past, expires, `tenant: dev
repos:
  - repo: bifrost-trade-infra
    allow: ["agent-config/work/multi-agent/REPORT-*.md"]
    deny: []
    stg_release: none
limits: {max_files: 100, max_changed_lines: 3000, max_landings_per_day: 100, max_in_flight_per_repo: 1}
`)
	preview, err = store.CheckRule(ctx, w.rt, "landing:dev", []byte(landing))
	ok(t, err)
	effects := preview["proposed_landing_effects"].([]map[string]any)
	if len(effects) != 1 || effects[0]["configured_changed_lines"] != 3000 || effects[0]["max_changed_lines"] != 0 || effects[0]["code_clamped"] != false {
		t.Fatalf("preview hid downstream clamp: %v", effects)
	}
}

// Serial: FreezeEventClock
func TestBoundaryPrecheckReadOnlyAndSameClaimGate(t *testing.T) {
	w, roots, at := batchRoots(t, 1)
	var before, after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&before))
	src := c4Source(precheckPath)
	out, err := store.ReadBoundaryPrecheck(ctx, w.rt, store.GrantSources{Facts: src}, roots[0])
	ok(t, err)
	if out["state"] != "claim_eligible" {
		t.Fatalf("precheck: %v", out)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&after))
	if after != before {
		t.Fatalf("read wrote events: %d -> %d", before, after)
	}
	unknown, err := store.ReadBoundaryPrecheck(ctx, w.rt, store.GrantSources{}, roots[0])
	ok(t, err)
	if unknown["state"] != "unknown" {
		t.Fatalf("missing readers: %v", unknown)
	}
	_, decision, err := store.ClaimLanding(ctx, w.rt, src, "prod", roots[0], at)
	ok(t, err)
	if !decision.Allowed {
		t.Fatalf("execution disagreed: %+v", decision)
	}
}
