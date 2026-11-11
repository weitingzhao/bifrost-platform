package landingci

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func reuseValue(t *testing.T, o *unstructured.Unstructured, name, value string) {
	t.Helper()
	entries, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
	for _, entry := range entries {
		m := entry.(map[string]any)
		if m["name"] == name {
			m["value"] = value
			setField(t, o, entries, "status", "results")
			return
		}
	}
	setField(t, o, append(entries, map[string]any{"name": name, "value": value}), "status", "results")
}

func TestReuseReaders(t *testing.T) {
	for _, tc := range []string{"full", "incremental", "missing", "red", "tree", "ui", "infra", "chain", "empty original", "later", "console", "copied scope", "invalid tree"} {
		t.Run(tc, func(t *testing.T) {
			at := time.Now().UTC().Add(-time.Hour)
			original := releaseCI(t, "original", at)
			reuse := releaseCI(t, "reuse", at.Add(time.Minute))
			// The original tested a different commit with the same tree.
			params, _, _ := unstructured.NestedSlice(original.Object, "spec", "params")
			for _, entry := range params {
				m := entry.(map[string]any)
				if m["name"] == "revision" {
					m["value"] = factSHA2
				}
			}
			setField(t, original, params, "spec", "params")
			reuseValue(t, original, "platform-commit", factSHA2)
			for _, o := range []*unstructured.Unstructured{original, reuse} {
				reuseValue(t, o, "platform-tree", factSHA1)
			}
			reuseValue(t, reuse, "tested-by", "original")
			objects := []*unstructured.Unstructured{original, reuse}
			switch tc {
			case "incremental":
				for _, o := range objects {
					reuseValue(t, o, APIScopeResult, APIScopeIncremental)
				}
				full := releaseCI(t, "full", at.Add(-time.Minute))
				objects = append(objects, full)
			case "missing":
				objects = objects[1:]
			case "red":
				setField(t, original, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
			case "tree":
				reuseValue(t, original, "platform-tree", factSHA2)
			case "ui":
				reuseValue(t, original, "ui-commit", factSHA1)
			case "infra":
				reuseValue(t, original, "infra-commit", factSHA1)
			case "chain":
				reuseValue(t, original, "tested-by", "another")
			case "empty original":
				// TD-533: a run that reused nothing writes an empty tested-by;
				// it is an original like any other.
				reuseValue(t, original, "tested-by", "")
			case "later":
				original.SetCreationTimestamp(reuse.GetCreationTimestamp())
			case "console":
				for _, o := range objects {
					reuseValue(t, o, APIScopeResult, APIScopeConsole)
				}
			case "copied scope":
				reuseValue(t, reuse, APIScopeWhyResult, "different")
			case "invalid tree":
				for _, o := range objects {
					reuseValue(t, o, "platform-tree", "bad")
				}
			}
			var listed []runtime.Object
			for _, o := range objects {
				listed = append(listed, o)
			}
			c, _ := drillService(t, listed...)
			recorded := 0
			c.ReuseAccepted = func(_ context.Context, revision, tree, run, uid string) error {
				recorded++
				if revision != factSHA1 || tree != factSHA1 || run != "original" || uid != string(original.GetUID()) {
					t.Fatalf("reuse body: %s %s %s %s", revision, tree, run, uid)
				}
				return nil
			}
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil {
				t.Fatal(err)
			}
			if tc == "console" {
				if recorded != 0 {
					t.Fatal("partial reuse recorded")
				}
				if len(got.Partial) != 1 {
					t.Fatal(got)
				}
				return
			}
			accepted := tc == "full" || tc == "incremental" || tc == "empty original"
			wantRecords := 0
			if accepted {
				wantRecords = 1
			}
			if recorded != wantRecords {
				t.Fatalf("recorded %d, want %d", recorded, wantRecords)
			}
			if accepted {
				if got.Status != "Succeeded" || got.TestedBy != "original" || !strings.Contains(got.ScopeNote(), "tested by run original (same tree)") {
					t.Fatal(got)
				}
				record, err := got.Record()
				if err != nil || record["tested_by"] != "original" {
					t.Fatal(record, err)
				}
			} else if got.Status != "Unknown" || got.Reason != "ci_reuse" {
				t.Fatal(got)
			}
			legacy, err := c.LandingCI(context.Background(), factSHA1, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			key := Key{Repo: "bifrost-platform", Revision: factSHA1, Base: factSHA2, Siblings: map[string]string{"bifrost-ui": factSHA2, "bifrost-trade-infra": factSHA2}}
			pinned, err := c.Read(context.Background(), key, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if recorded != 3*wantRecords {
				t.Fatalf("reader records %d, want %d", recorded, 3*wantRecords)
			}
			if accepted {
				if legacy.Status != "Succeeded" || pinned.Status != "Succeeded" {
					t.Fatal(legacy, pinned)
				}
			} else if legacy.Status != "Unknown" || pinned.Reason != "ci_reuse" {
				t.Fatal(legacy, pinned)
			}
		})
	}
}

func TestLandingReuseBaseAndFreshness(t *testing.T) {
	for _, scope := range []string{APIScopeFull, APIScopeIncremental} {
		for _, base := range []string{factSHA1, factSHA2} {
			t.Run(scope+base[:1], func(t *testing.T) {
				at := time.Now().UTC().Add(-time.Hour)
				original, reuse := releaseCI(t, "original", at), releaseCI(t, "reuse", at.Add(time.Minute))
				for _, o := range []*unstructured.Unstructured{original, reuse} {
					reuseValue(t, o, "platform-tree", factSHA1)
					reuseValue(t, o, APIScopeResult, scope)
					params, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
					value := factSHA1
					if o == original {
						value = base
					}
					setField(t, o, append(params, map[string]any{"name": "base", "value": value}), "spec", "params")
				}
				reuseValue(t, reuse, "tested-by", "original")
				full := releaseCI(t, "full", at.Add(-time.Minute))
				items := []unstructured.Unstructured{*original, *reuse, *full}
				got := landingReuse(*reuse, items, factSHA1)
				want := scope == APIScopeFull || base == factSHA1
				if (got.Status == "Succeeded") != want {
					t.Fatal(got)
				}
				if scope == APIScopeIncremental {
					full.SetCreationTimestamp(original.GetCreationTimestamp())
					items = items[:2]
					got = landingReuse(*reuse, items, factSHA1)
					if got.Reason != "ci_reuse" {
						t.Fatal(got)
					}
				}
			})
		}
	}
}

// Reuse must not renew the full-test freshness window.
func TestReuseDoesNotRefreshFull(t *testing.T) {
	at := time.Now().UTC().Add(-FullMaxAge - time.Hour)
	original := releaseCI(t, "original", at)
	reuse := releaseCI(t, "reuse", time.Now().UTC().Add(-time.Minute))
	for _, o := range []*unstructured.Unstructured{original, reuse} {
		reuseValue(t, o, "platform-tree", factSHA1)
	}
	reuseValue(t, reuse, "tested-by", "original")
	c, _ := drillService(t, original, reuse)
	got, err := c.ReadRelease(context.Background(), factSHA1)
	if err != nil || got.Status != "Succeeded" || got.LastFullRun != "original" || got.FullFresh() {
		t.Fatal(got, err)
	}
	for _, o := range []*unstructured.Unstructured{original, reuse} {
		reuseValue(t, o, APIScopeResult, APIScopeIncremental)
	}
	c, _ = drillService(t, original, reuse)
	got, err = c.ReadRelease(context.Background(), factSHA1)
	if err != nil || got.Status != "Succeeded" || got.Reason != "" {
		t.Fatal(got, err)
	}
}

// TD-533: the reuse step writes an empty tested-by when it reused nothing. Only
// a tested-by naming a run claims a reuse; a repeated one still does (and the
// reuse checks refuse it as malformed).
func TestClaimsReuseNeedsANamedRun(t *testing.T) {
	at := time.Now().UTC().Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		values []string
		want   bool
	}{
		{"absent", nil, false},
		{"empty", []string{""}, false},
		{"blank", []string{"  "}, false},
		{"named", []string{"original"}, true},
		{"repeated", []string{"", ""}, true},
	} {
		o := releaseCI(t, "run", at)
		entries, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
		for _, v := range tc.values {
			entries = append(entries, map[string]any{"name": "tested-by", "value": v})
		}
		setField(t, o, entries, "status", "results")
		if got := ClaimsReuse(*o); got != tc.want {
			t.Errorf("%s: ClaimsReuse %v, want %v", tc.name, got, tc.want)
		}
		if tc.want {
			continue
		}
		items := []unstructured.Unstructured{*o}
		if ValidReuse(*o, items, factSHA1) {
			t.Errorf("%s: an ordinary run passed as a reuse", tc.name)
		}
		if got, want := reuseResult(*o, items, factSHA1), releaseResult(*o, factSHA1); got.Status != want.Status || got.Reason != want.Reason {
			t.Errorf("%s: reuse result %s/%s, want the run's own %s/%s", tc.name, got.Status, got.Reason, want.Status, want.Reason)
		}
	}
}
