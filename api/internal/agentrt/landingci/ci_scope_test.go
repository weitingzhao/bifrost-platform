package landingci

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func withResults(t *testing.T, o *unstructured.Unstructured, pairs ...string) *unstructured.Unstructured {
	t.Helper()
	r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
	for i := 0; i+1 < len(pairs); i += 2 {
		r = append(r, map[string]any{"name": pairs[i], "value": pairs[i+1]})
	}
	setField(t, o, r, "status", "results")
	return o
}

func scopedGreen(t *testing.T, at time.Time) *unstructured.Unstructured {
	return withResults(t, greenRun(t, "green", at), APIScopeResult, APIScopeIncremental,
		APIPackagesResult, "./internal/agentrt/landing ./internal/agentrt/store", APIStoreRunResult, "TestLanding", APIScopeWhyResult, "the change reaches landing")
}

func readScope(t *testing.T, at time.Time, objects ...runtime.Object) (landing.CIRun, landing.CIRun) {
	t.Helper()
	c, _ := drillService(t, objects...)
	legacy, err := c.LandingCI(context.Background(), factSHA1, at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := c.Read(context.Background(), pinnedKey(), at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return legacy, pinned.CIRun
}

// A platform run's four scope results come with its CIRun and its ci_seen.
func TestCIScopeReadFromAPlatformRun(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	want := landing.CIScope{Scope: "incremental", Packages: "./internal/agentrt/landing ./internal/agentrt/store", StoreRun: "TestLanding", Why: "the change reaches landing"}
	legacy, pinned := readScope(t, at, scopedGreen(t, at))
	if legacy.Scope != want || pinned.Scope != want {
		t.Fatal(legacy.Scope, pinned.Scope)
	}
	r := landing.CIRecord("land:1", legacy)
	if r["api_scope"] != want.Scope || r["api_packages"] != want.Packages || r["api_store_run"] != want.StoreRun || r["api_scope_why"] != want.Why {
		t.Fatal(r)
	}
	if _, err := landing.Parse(r); err != nil {
		t.Fatal(err)
	}
}

// A run without the results has no scope, and its ci_seen is today's.
func TestCIScopeAbsentFromARunWithoutIt(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	legacy, pinned := readScope(t, at, greenRun(t, "green", at))
	if legacy.Scope != (landing.CIScope{}) || pinned.Scope != (landing.CIScope{}) {
		t.Fatal(legacy.Scope, pinned.Scope)
	}
	r := landing.CIRecord("land:1", legacy)
	if len(r) != 7 {
		t.Fatal(r)
	}
	for _, k := range []string{"api_scope", "api_packages", "api_store_run", "api_scope_why"} {
		if _, ok := r[k]; ok {
			t.Fatal(k, r)
		}
	}
}

// A duplicated result is not read, nor an api-scope outside the three; the
// others still are.
func TestCIScopeDuplicateOrUnknownNotRead(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	dup := withResults(t, scopedGreen(t, at), APIPackagesResult, "./other", APIStoreRunResult, "TestLanding")
	legacy, _ := readScope(t, at, dup)
	if want := (landing.CIScope{Scope: "incremental", Why: "the change reaches landing"}); legacy.Scope != want {
		t.Fatal(legacy.Scope)
	}
	unknown := withResults(t, greenRun(t, "green", at), APIScopeResult, "partial", APIScopeWhyResult, "why")
	legacy, _ = readScope(t, at, unknown)
	if want := (landing.CIScope{Why: "why"}); legacy.Scope != want {
		t.Fatal(legacy.Scope)
	}
	twice := withResults(t, greenRun(t, "green", at), APIScopeResult, APIScopeFull, APIScopeResult, APIScopeFull)
	if legacy, _ = readScope(t, at, twice); legacy.Scope != (landing.CIScope{}) {
		t.Fatal(legacy.Scope)
	}
}

// A value over 1500 bytes is cut, never inside a character.
func TestCIScopeOverlongValueCut(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	long := strings.Repeat("./internal/x ", 200)
	why := strings.Repeat("a", landing.CIScopeMax-1) + "é and more"
	legacy, _ := readScope(t, at, withResults(t, greenRun(t, "green", at), APIScopeResult, APIScopeFull, APIPackagesResult, long, APIScopeWhyResult, why))
	if legacy.Scope.Packages != long[:landing.CIScopeMax] || legacy.Scope.Why != why[:landing.CIScopeMax-1] {
		t.Fatal(len(legacy.Scope.Packages), len(legacy.Scope.Why))
	}
	if _, err := landing.Parse(landing.CIRecord("land:1", legacy)); err != nil {
		t.Fatal(err)
	}
}

// An infra run's results are its own: it gets no api scope.
func TestCIScopeOnlyForPlatformRuns(t *testing.T) {
	t.Parallel()
	o := withResults(t, infraRun(t, "infra", s65At), APIScopeResult, APIScopeFull, APIScopeWhyResult, "why")
	if got := readInfra(t, o); got.Scope != (landing.CIScope{}) {
		t.Fatal(got.Scope)
	}
}
