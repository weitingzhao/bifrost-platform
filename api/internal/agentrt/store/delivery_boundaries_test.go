package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
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
	_, err := w.rt.Exec(ctx, agentrttest.ConcurrencyRule)
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
	// 2026-11-15: a rule of a kind retired with the dispatch lane is named
	// and never parsed. One is put there as the Owner once signed it; before
	// this, one row of such a kind that no longer parsed failed the whole view.
	retired := false
	for _, r := range doc.Rules {
		if r.ID == "concurrency" {
			retired = r.Status == "retired" && r.Current == nil && r.Latest == nil
		}
	}
	if !retired {
		t.Fatalf("the retired concurrency rule is not listed as retired: %+v", doc.Rules)
	}
	_, err = s.sign(w, ruleText("tuning", 1, rules.Initial, past, expires, "values: {failures_to_alert: 3}\n"))
	ok(t, err)
	doc, err = store.ReadDeliveryBoundaries(ctx, w.rt, 0, time.Time{})
	ok(t, err)
	var row store.BoundaryRule
	for _, r := range doc.Rules {
		if r.ID == "tuning" {
			row = r
		}
	}
	if row.Current["rule_version"] != 1 || row.Draft["rule_version"] != 2 || row.Latest["rule_version"] != 1 {
		t.Fatalf("versions: %+v", row)
	}
	// The UI's JSON draft must parse identically to the rule's YAML text.
	row.Draft["values"] = map[string]any{"failures_to_alert": 5}
	b, err := json.Marshal(row.Draft)
	ok(t, err)
	preview, err := store.CheckRule(ctx, w.rt, "tuning", b)
	ok(t, err)
	if preview["apply_via"] != "capacity_approval" {
		t.Fatalf("capacity: %v", preview)
	}
	row.Draft["expires_at"] = now.Add(48 * time.Hour).Format(time.RFC3339)
	b, err = json.Marshal(row.Draft)
	ok(t, err)
	preview, err = store.CheckRule(ctx, w.rt, "tuning", b)
	ok(t, err)
	if preview["apply_via"] != "signature" {
		t.Fatalf("authority: %v", preview)
	}
	_, err = s.sign(w, ruleText("tuning", 2, rules.Loosen, future, expires, "values: {failures_to_alert: 5}\n"))
	ok(t, err)
	doc, err = store.ReadDeliveryBoundaries(ctx, w.rt, 12345, time.Time{})
	ok(t, err)
	for _, r := range doc.Rules {
		if r.ID == "tuning" {
			row = r
		}
	}
	if row.Current["rule_version"] != 1 || row.Latest["rule_version"] != 2 || row.Draft["rule_version"] != 3 || doc.DiskFloor != 12345 {
		t.Fatalf("scheduled version is not effective: %+v", row)
	}
	_, err = store.CheckRule(ctx, w.rt, "tuning", b)
	wantErr(t, err, "at version 2")
	_, err = s.sign(w, ruleText("tuning", 3, rules.Loosen, past, now.Add(-time.Minute).Format(time.RFC3339), "values: {failures_to_alert: 5}\n"))
	ok(t, err)
	doc, err = store.ReadDeliveryBoundaries(ctx, w.rt, 0, time.Time{})
	ok(t, err)
	for _, r := range doc.Rules {
		if r.ID == "tuning" && (r.Current != nil || r.Status != "not_in_force") {
			t.Fatalf("expired rule fell back: %+v", r)
		}
	}
}
