package landing

import (
	"maps"
	"strings"
	"testing"
	"time"
)

func scopedRun() CIRun {
	return CIRun{Run: "ci-1", Status: "Succeeded", R: R, CreatedAt: epoch.Add(2 * time.Minute),
		Scope: CIScope{Scope: "incremental", Packages: "./internal/agentrt/landing ./internal/agentrt/store", StoreRun: "TestLanding", Why: "the change reaches landing"}}
}

// A platform run's api scope rides on its ci_seen record, folds and is read
// back; CIChanged does not compare it.
func TestCIScopeRecordCarriesTheRunsScope(t *testing.T) {
	t.Parallel()
	ci := scopedRun()
	r := CIRecord("land:1", ci)
	want := map[string]any{"api_scope": "incremental", "api_packages": ci.Scope.Packages, "api_store_run": "TestLanding", "api_scope_why": ci.Scope.Why}
	for k, v := range want {
		if r[k] != v {
			t.Fatal(k, r[k])
		}
	}
	s := mustFold(t, append(records()[:5], Record(r)))
	o := s.Operations["land:1"]
	if o.CI.String("api_scope") != "incremental" || o.CI.String("api_packages") != ci.Scope.Packages || o.CI.String("api_store_run") != "TestLanding" || o.CI.String("api_scope_why") != ci.Scope.Why {
		t.Fatal(o.CI)
	}
	other := ci
	other.Scope = CIScope{Scope: "full"}
	if CIChanged(o.CI, other) || CIChanged(o.CI, CIRun{Run: ci.Run, Status: ci.Status, R: R, CreatedAt: ci.CreatedAt}) {
		t.Fatal("CIChanged compares the scope")
	}
	// An empty store pattern (all of its tests) is a value too.
	ci.Scope.StoreRun = ""
	if _, err := Parse(CIRecord("land:1", ci)); err != nil {
		t.Fatal(err)
	}
}

// A run without the results records today's ci_seen, key for key.
func TestCIScopeRecordWithoutScopeIsUnchanged(t *testing.T) {
	t.Parallel()
	ci := scopedRun()
	ci.Scope = CIScope{}
	r := CIRecord("land:1", ci)
	want := map[string]any{"v": Version, "kind": "ci_seen", "op": "land:1", "run": "ci-1", "status": "Succeeded", "R": R, "created_at": ci.CreatedAt.Format(time.RFC3339Nano)}
	if !maps.Equal(r, want) {
		t.Fatal(r)
	}
	if _, err := Parse(r); err != nil {
		t.Fatal(err)
	}
}

// ci_seen accepts the four scope keys, all of them together, and nothing else.
func TestCIScopeRecordRefusesOtherKeys(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(map[string]any){
		"unknown key":    func(r map[string]any) { r["api_other"] = "x" },
		"missing key":    func(r map[string]any) { delete(r, "api_scope_why") },
		"unknown scope":  func(r map[string]any) { r["api_scope"] = "partial" },
		"not a string":   func(r map[string]any) { r["api_packages"] = 1 },
		"overlong value": func(r map[string]any) { r["api_scope_why"] = strings.Repeat("x", CIScopeMax+1) },
		"older version":  func(r map[string]any) { r["v"] = "1.1" },
	} {
		r := CIRecord("land:1", scopedRun())
		change(r)
		if _, err := Parse(r); err == nil {
			t.Fatal(name, "accepted", r)
		}
	}
}
