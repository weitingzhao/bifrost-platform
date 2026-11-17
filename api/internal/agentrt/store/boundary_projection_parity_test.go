//bifrost:owner delivery

package store

import (
	"bytes"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"reflect"
	"testing"
	"time"
)

func frozenBoundaryRule(id string, current, latest *StoredRule, now time.Time) BoundaryRule {
	v := BoundaryRule{ID: id, Status: "not_in_force", Notes: []string{}}
	if current != nil {
		v.Current = current.Doc()
		v.Status = "in_force"
	}
	if latest != nil {
		v.Latest = latest.Doc()
		v.Draft = latest.Doc()
		v.Draft["rule_version"] = latest.Version + 1
		v.Draft["direction"] = rules.Loosen
		if latest.Effective.After(now) {
			v.Notes = append(v.Notes, "The latest version is scheduled; the effective version is shown separately.")
		}
		if !latest.Expires.After(now) {
			v.Notes = append(v.Notes, "The latest version has expired. Review its expiry before proposing a replacement.")
		}
	}
	if id == rules.KindTuning && latest == nil {
		v.Status = "code_defaults"
		values := map[string]int{}
		for _, key := range rules.TuningKeys() {
			values[key.Name] = key.Default
		}
		v.Draft = map[string]any{"rule_id": id, "rule_version": 1, "direction": rules.Initial, "effective_at": now.UTC().Format(time.RFC3339), "expires_at": "", "values": values}
		v.Notes = append(v.Notes, "No tuning rule is installed. Defaults below are read from the runtime registry. The first version requires a signature and an explicit expiry.")
	}
	return v
}

func TestBoundaryProjectionFrozenParity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{rules.KindTuning, rules.KindPlan, "unknown"} {
		for _, c := range []bool{false, true} {
			for _, l := range []bool{false, true} {
				for _, offset := range []time.Duration{-time.Hour, 0, time.Hour} {
					var current, latest *StoredRule
					makeRule := func() *StoredRule {
						return &StoredRule{Rule: rules.Rule{Header: rules.Header{ID: id, Version: 3, Direction: rules.Initial, Effective: now.Add(offset), Expires: now.Add(offset)}, Kind: rules.KindTuning, Tuning: &rules.Tuning{Values: map[string]int{"failures_to_alert": 3}}}}
					}
					if c {
						current = makeRule()
					}
					if l {
						latest = makeRule()
					}
					want := frozenBoundaryRule(id, current, latest, now)
					got := boundaryRule(id, current, latest, now)
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("%s %v %v %v: %#v != %#v", id, c, l, offset, want, got)
					}
					oldJSON, _ := json.Marshal(want)
					newJSON, _ := json.Marshal(got)
					if !bytes.Equal(oldJSON, newJSON) {
						t.Fatal("json differs")
					}
					if got.Draft != nil && got.Latest != nil {
						got.Draft["rule_version"] = 99
						if got.Latest["rule_version"] == 99 {
							t.Fatal("draft aliases latest")
						}
					}
				}
			}
		}
	}
}
