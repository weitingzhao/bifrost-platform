package releasepolicy_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const (
	anchorFile    = "internal/releasepolicy/engine.go"
	ownerOnlyFile = "internal/releasepolicy/mustrefuse/cases_test.go"
)

// selfChecked is the fixture's policy with the fields the Owner signs to let
// an author prove a trust-anchor change himself.
func selfChecked(doc map[string]any) {
	doc["conditions"].(map[string]any)["trust_anchor_self_checked"] = true
	doc["self_checked_repos"] = []string{"repo-app"}
	doc["paths"].(map[string]any)["owner_only"] = []map[string]any{
		{"repo": "repo-app", "paths": []string{"internal/releasepolicy/mustrefuse/**"}},
	}
}

// newSelfCheckFixture: a release of repo-app that changes a trust anchor,
// with CI green for the released commit.
func newSelfCheckFixture(t *testing.T, edit func(doc map[string]any)) *fixture {
	t.Helper()
	f := newFixture(t)
	if edit == nil {
		edit = selfChecked
	}
	f.signPolicy(t, f.key, rptest.PolicyTextWith("rp-self", now.Add(-time.Hour), 90, edit, pipeline))
	f.facts.Files["repo-app"] = []string{"docs/readme.md", anchorFile}
	return f
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). The positive control
// of every case below: a trust-anchor change whose commit has a Succeeded CI
// run and whose diff leaves the owner-only paths alone.
func TestASelfCheckedTrustAnchorChangeIsCovered(t *testing.T) {
	f := newSelfCheckFixture(t, nil)
	d := f.decide(nil)
	wantAuto(t, d)
	if got, want := d.ClauseText(), "signature,unexpired,not_frozen,allow:"+pipeline+
		",window_held_by_requester,revision,ci_succeeded,no_ddl,no_d10_paths,trust_anchor_self_checked,owner_only"; got != want {
		t.Fatalf("clauses = %s, want %s", got, want)
	}
	if want := []string{"repo-app@" + rptest.SHA("new")[:12]}; !reflect.DeepEqual(d.SelfCheckedAnchors, want) {
		t.Fatalf("self-checked = %q, want %q", d.SelfCheckedAnchors, want)
	}
	// A release that touches no trust anchor names the plain clause.
	f.facts.Files["repo-app"] = []string{"docs/readme.md"}
	d = f.decide(nil)
	wantAuto(t, d)
	if !strings.HasSuffix(d.ClauseText(), ",no_trust_anchor_change,owner_only") || len(d.SelfCheckedAnchors) != 0 {
		t.Fatalf("clauses = %s self-checked = %q", d.ClauseText(), d.SelfCheckedAnchors)
	}
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). Each case is the
// covered release with one thing wrong.
func TestASelfCheckWithOneThingWrongWaits(t *testing.T) {
	plain := "hits no_trust_anchor_change: " + anchorFile
	needsCI := plain + " (self-checked only with a Succeeded ci run of " + rptest.SHA("new")[:12] + " under ci_succeeded)"
	ownerOnly := "touches owner-only paths: " + ownerOnlyFile + " (only the Owner changes these)"
	cases := map[string]struct {
		policy func(doc map[string]any)
		world  func(f *fixture)
		want   string
	}{
		"an owner-only path": {world: func(f *fixture) {
			f.facts.Files["repo-app"] = []string{anchorFile, ownerOnlyFile}
		}, want: ownerOnly},
		// The must-refuse set is test files: leaving tests out of the
		// trust-anchor hits must not leave them out of the owner-only paths.
		"an owner-only test file while tests are ignored": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["conditions"].(map[string]any)["trust_anchor_ignore_tests"] = true
		}, world: func(f *fixture) { f.facts.Files["repo-app"] = []string{ownerOnlyFile} }, want: ownerOnly},
		"an owner-only path only in the complete difference of a large diff": {world: func(f *fixture) {
			f.facts.Files["repo-app"] = append(manyFiles(100), anchorFile)
			f.facts.TreeFiles = map[string][]string{"repo-app": append(manyFiles(100), anchorFile, ownerOnlyFile)}
		}, want: ownerOnly},
		// The group stands on its own: it refuses with the self-check off.
		"an owner-only path, self-check off": {policy: func(doc map[string]any) {
			selfChecked(doc)
			delete(doc["conditions"].(map[string]any), "trust_anchor_self_checked")
		}, world: func(f *fixture) { f.facts.Files["repo-app"] = []string{"docs/readme.md", ownerOnlyFile} }, want: ownerOnly},
		"CI did not succeed": {world: func(f *fixture) { f.facts.Green = nil }, want: needsCI},
		// The self-check rests on ci_succeeded: a policy that switches CI
		// off, or does not list the repo for CI, has no machine fact left.
		"ci_succeeded switched off": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["conditions"].(map[string]any)["ci_succeeded"] = false
		}, want: needsCI},
		"the repo is not one CI is read for": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["ci_repos"] = []string{"repo-lib"}
		}, want: needsCI},
		"the repo is not self-checked": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["self_checked_repos"] = []string{"repo-lib"}
		}, want: plain},
		"no self-checked repos": {policy: func(doc map[string]any) {
			selfChecked(doc)
			delete(doc, "self_checked_repos")
		}, want: plain},
		"condition false": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["conditions"].(map[string]any)["trust_anchor_self_checked"] = false
		}, want: plain},
		"condition not a boolean": {policy: func(doc map[string]any) {
			selfChecked(doc)
			doc["conditions"].(map[string]any)["trust_anchor_self_checked"] = "true"
		}, want: plain},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSelfCheckFixture(t, tc.policy)
			if tc.world != nil {
				tc.world(f)
			}
			d := f.decide(nil)
			wantWait(t, d, tc.want)
			if len(d.SelfCheckedAnchors) != 0 || strings.Contains(d.ClauseText(), "trust_anchor_self_checked") {
				t.Fatalf("a refused self-check is named as passed: %+v", d)
			}
		})
	}
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). The policy signed on
// 2026-11-15 carries trust_anchor_reviewed, reviewable_repos and
// trust_anchor_ignore_tests. None of them is read any more: a trust-anchor
// change, test files included, is refused exactly as under a policy that
// never had them, until the Owner signs the new fields.
func TestThePolicySignedForReviewsRefusesATrustAnchorChange(t *testing.T) {
	signedToday := func(doc map[string]any) {
		c := doc["conditions"].(map[string]any)
		c["trust_anchor_reviewed"], c["trust_anchor_ignore_tests"] = true, true
		doc["reviewable_repos"] = []string{"repo-app"}
	}
	for name, files := range map[string][]string{
		"a trust anchor":       {"docs/readme.md", anchorFile},
		"only its test files":  {"internal/releasepolicy/engine_test.go", "internal/releasepolicy/testdata/key.pub"},
		"an owner-only path":   {ownerOnlyFile},
		"no trust anchor":      {"docs/readme.md"},
		"a trust anchor, huge": append(manyFiles(100), anchorFile),
	} {
		t.Run(name, func(t *testing.T) {
			decide := func(edit func(doc map[string]any)) releasepolicy.Decision {
				f := newSelfCheckFixture(t, edit)
				f.facts.Files["repo-app"] = files
				f.facts.TreeFiles = map[string][]string{"repo-app": files}
				return f.decide(nil)
			}
			got, want := decide(signedToday), decide(func(map[string]any) {})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decision = %+v\nwant      %+v", got, want)
			}
			if name == "no trust anchor" {
				wantAuto(t, got)
				if !strings.HasSuffix(got.ClauseText(), ",no_trust_anchor_change") {
					t.Fatalf("clauses = %s", got.ClauseText())
				}
				return
			}
			wantWait(t, got, "repo-app "+rptest.SHA("old")[:12]+".."+rptest.SHA("new")[:12]+" hits no_trust_anchor_change: ")
		})
	}
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). Test files are left
// out of the trust-anchor hits only when the policy says so, and only where
// the self-check applies.
func TestTrustAnchorTestFilesAreIgnoredOnlyWhenThePolicySaysSo(t *testing.T) {
	testFiles := []string{"internal/releasepolicy/engine_test.go", "internal/releasepolicy/testdata/key.pub"}
	ignoring := func(doc map[string]any) {
		selfChecked(doc)
		doc["conditions"].(map[string]any)["trust_anchor_ignore_tests"] = true
	}
	t.Run("ignored: only test files changed, nothing to self-check", func(t *testing.T) {
		f := newSelfCheckFixture(t, ignoring)
		f.facts.Files["repo-app"] = testFiles
		d := f.decide(nil)
		wantAuto(t, d)
		if !strings.HasSuffix(d.ClauseText(), ",no_trust_anchor_change,owner_only") || len(d.SelfCheckedAnchors) != 0 {
			t.Fatalf("clauses = %s", d.ClauseText())
		}
	})
	t.Run("not ignored by default: test files are self-checked like any other", func(t *testing.T) {
		f := newSelfCheckFixture(t, nil)
		f.facts.Files["repo-app"] = testFiles
		d := f.decide(nil)
		wantAuto(t, d)
		if !strings.Contains(d.ClauseText(), "trust_anchor_self_checked") {
			t.Fatalf("clauses = %s", d.ClauseText())
		}
	})
	t.Run("ignoring does nothing where no self-check is allowed", func(t *testing.T) {
		f := newSelfCheckFixture(t, func(doc map[string]any) {
			doc["conditions"].(map[string]any)["trust_anchor_ignore_tests"] = true
		})
		f.facts.Files["repo-app"] = testFiles
		wantWait(t, f.decide(nil), "hits no_trust_anchor_change")
	})
}
