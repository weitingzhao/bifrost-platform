package landingci

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func releaseCI(t *testing.T, name string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	o := greenRun(t, name, at)
	params, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
	setField(t, o, append(params, map[string]any{"name": "repo-url", "value": PlatformRepoURL}), "spec", "params")
	return o
}

// A newer run with a verdict that is not an accepted green always wins over
// an older green. A newer run with no verdict is TestReleaseCIPassesOver….
func TestReleaseCIRejectsLatestWithSuccessfulControl(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	cases := map[string]struct {
		status string
		mutate func(*testing.T, *unstructured.Unstructured)
	}{
		"failed": {"Failed", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		}},
		"missing result": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		"duplicate result": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			setField(t, o, append(r, r[0]), "status", "results")
		}},
		"illegal sha": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			r[1].(map[string]any)["value"] = "main"
			setField(t, o, r, "status", "results")
		}},
		"wrong P": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			r[0].(map[string]any)["value"] = factSHA2
			setField(t, o, r, "status", "results")
		}},
		"no uid": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) { o.SetUID("") }},
		"missing repo": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"name": "revision", "value": factSHA1}}, "spec", "params")
		}},
		"resolver": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "git", "spec", "pipelineRef", "resolver")
		}},
		"inline": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, map[string]any{}, "spec", "pipelineSpec")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
			read := func() ReleaseEvidence {
				t.Helper()
				c, _ := drillService(t, old, newest)
				got, err := c.ReadRelease(context.Background(), factSHA1)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			if got := read(); got.Status != "Succeeded" || got.Run != "new" || got.UICommit != factSHA2 || got.InfraCommit != factSHA2 {
				t.Fatal("control", got)
			}
			tc.mutate(t, newest)
			if got := read(); got.Status != tc.status || got.Run != "new" {
				t.Fatal("refusal", got)
			}
		})
	}
}
func TestReleaseCITiesAndRepositoryFilter(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	a, b := releaseCI(t, "a", at), releaseCI(t, "b", at)
	read := func() ReleaseEvidence {
		t.Helper()
		c, _ := drillService(t, b, a)
		got, err := c.ReadRelease(context.Background(), factSHA1)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); got.Run != "a" || got.Status != "Succeeded" {
		t.Fatal(got)
	}
	r, _, _ := unstructured.NestedSlice(b.Object, "status", "results")
	r[1].(map[string]any)["value"] = factSHA1
	setField(t, b, r, "status", "results")
	if got := read(); got.Status != "Unknown" || got.Reason != "ci_tie" {
		t.Fatal(got)
	}
	// An unrelated repository cannot poison this repository's CI choice.
	p, _, _ := unstructured.NestedSlice(b.Object, "spec", "params")
	for _, v := range p {
		m := v.(map[string]any)
		if m["name"] == "repo-url" {
			m["value"] = "https://other/repo.git"
		}
	}
	setField(t, b, p, "spec", "params")
	if got := read(); got.Status != "Succeeded" || got.Run != "a" {
		t.Fatal(got)
	}
}
func TestReleaseCIPaginationReadFailureAndRepeatedPage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"newest", "read error", "repeated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			at := time.Now().UTC().Add(-time.Hour)
			old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
			c, dyn := drillService(t)
			page := 0
			dyn.PrependReactor("list", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
				page++
				if page == 1 {
					return true, &unstructured.UnstructuredList{Object: map[string]any{"metadata": map[string]any{"continue": "next"}}, Items: []unstructured.Unstructured{*old}}, nil
				}
				if mode == "read error" {
					return true, nil, errors.New("unreadable")
				}
				if mode == "repeated" {
					return true, &unstructured.UnstructuredList{Object: map[string]any{"metadata": map[string]any{"continue": "next"}}}, nil
				}
				return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*newest}}, nil
			})
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if mode == "newest" {
				if err != nil || got.Run != "new" || got.Status != "Succeeded" {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("accepted unread list", got)
			}
		})
	}
	c, _ := drillService(t)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Missing" {
		t.Fatal(got, err)
	}
	if _, err := c.ReadRelease(context.Background(), "main"); err == nil {
		t.Fatal("bad revision")
	}
	// Same entry after removing an old CI still accepts its surviving successor.
	at := metav1.Now().Add(-time.Hour)
	c, _ = drillService(t, releaseCI(t, "successor", at))
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Succeeded" {
		t.Fatal(got, err)
	}
}

func TestReleaseCIDuplicateRevisionCannotReviveOlderGreen(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
	c, _ := drillService(t, old, newest)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Succeeded" || got.Run != "new" {
		t.Fatal("control", got, err)
	}
	params, _, _ := unstructured.NestedSlice(newest.Object, "spec", "params")
	setField(t, newest, append(params, map[string]any{"name": "revision", "value": factSHA2}), "spec", "params")
	c, _ = drillService(t, old, newest)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Unknown" || got.Run != "new" {
		t.Fatal(got, err)
	}
}

func runningCI(t *testing.T, o *unstructured.Unstructured, reason string) {
	t.Helper()
	setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": reason}}, "status", "conditions")
	unstructured.RemoveNestedField(o.Object, "status", "results")
}

// R5:5.7-2: main's push CI still running over the landing CI of the same
// commit no longer hides it. Only a strictly newer run with no verdict and a
// valid identity is passed over; a newer verdict, red or unusable, still wins.
func TestReleaseCIPassesOverUndecidedNewerRuns(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	cases := map[string]struct {
		mutate              func(*testing.T, *unstructured.Unstructured)
		run, status, reason string
		undecided           int
	}{
		"running":            {func(t *testing.T, o *unstructured.Unstructured) { runningCI(t, o, "Running") }, "old", "Succeeded", "", 1},
		"pending":            {func(t *testing.T, o *unstructured.Unstructured) { runningCI(t, o, "PipelineRunPending") }, "old", "Succeeded", "", 1},
		"cancelling finally": {func(t *testing.T, o *unstructured.Unstructured) { runningCI(t, o, "CancelledRunFinally") }, "old", "Succeeded", "", 1},
		"no status":          {func(t *testing.T, o *unstructured.Unstructured) { delete(o.Object, "status") }, "old", "Succeeded", "", 1},
		"failed": {func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False", "reason": "Failed"}}, "status", "conditions")
		}, "new", "Failed", "", 0},
		"cancelled": {func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False", "reason": "Cancelled"}}, "status", "conditions")
		}, "new", "Failed", "", 0},
		"completed without verdict": {func(t *testing.T, o *unstructured.Unstructured) {
			runningCI(t, o, "Running")
			setField(t, o, at.Format(time.RFC3339), "status", "completionTime")
		}, "new", "Running", "", 0},
		"two conditions": {func(t *testing.T, o *unstructured.Unstructured) {
			c := map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}
			setField(t, o, []any{c, c}, "status", "conditions")
		}, "new", "Unknown", "", 0},
		"running inline": {func(t *testing.T, o *unstructured.Unstructured) {
			runningCI(t, o, "Running")
			setField(t, o, map[string]any{}, "spec", "pipelineSpec")
		}, "new", "Unknown", "ci_identity", 0},
		"running resolver": {func(t *testing.T, o *unstructured.Unstructured) {
			runningCI(t, o, "Running")
			setField(t, o, "git", "spec", "pipelineRef", "resolver")
		}, "new", "Unknown", "ci_identity", 0},
		"running other repo label": {func(t *testing.T, o *unstructured.Unstructured) {
			runningCI(t, o, "Running")
			o.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-trade-infra"})
		}, "new", "Unknown", "ci_identity", 0},
		"running duplicate revision": {func(t *testing.T, o *unstructured.Unstructured) {
			runningCI(t, o, "Running")
			p, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
			setField(t, o, append(p, map[string]any{"name": "revision", "value": factSHA2}), "spec", "params")
		}, "new", "Unknown", "ci_identity", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
			tc.mutate(t, newest)
			c, _ := drillService(t, old, newest)
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil {
				t.Fatal(err)
			}
			if got.Run != tc.run || got.Status != tc.status || got.Reason != tc.reason || len(got.Undecided) != tc.undecided {
				t.Fatalf("got run=%s status=%s reason=%s undecided=%v", got.Run, got.Status, got.Reason, got.Undecided)
			}
			_, recErr := got.Record()
			if (recErr == nil) != (tc.status == "Succeeded") {
				t.Fatal("accepted evidence must be exactly the decided green", recErr)
			}
		})
	}
}

func TestReleaseCIUndecidedNeverRevivesPastAVerdict(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	read := func(t *testing.T, objs ...*unstructured.Unstructured) ReleaseEvidence {
		t.Helper()
		in := make([]runtime.Object, 0, len(objs))
		for _, o := range objs {
			in = append(in, o)
		}
		c, _ := drillService(t, in...)
		got, err := c.ReadRelease(context.Background(), factSHA1)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// running over red over green: the red is the newest verdict.
	green, red, running := releaseCI(t, "green", at.Add(-2*time.Minute)), releaseCI(t, "red", at.Add(-time.Minute)), releaseCI(t, "running", at)
	setField(t, red, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
	runningCI(t, running, "Running")
	if got := read(t, green, red, running); got.Run != "red" || got.Status != "Failed" || len(got.Undecided) != 1 || got.Undecided[0] != "running" {
		t.Fatal("older green revived past a newer red", got.Run, got.Status, got.Undecided)
	}
	// Nothing decided: the newest is reported as before, nothing passed over.
	a, b := releaseCI(t, "a", at.Add(-time.Minute)), releaseCI(t, "b", at)
	runningCI(t, a, "Running")
	runningCI(t, b, "Running")
	if got := read(t, a, b); got.Run != "b" || got.Status != "Running" || len(got.Undecided) != 0 {
		t.Fatal("all undecided", got.Run, got.Status, got.Undecided)
	}
	// Two undecided over one green: both passed over.
	g, u1, u2 := releaseCI(t, "g", at.Add(-2*time.Minute)), releaseCI(t, "u1", at.Add(-time.Minute)), releaseCI(t, "u2", at)
	runningCI(t, u1, "Running")
	delete(u2.Object, "status")
	if got := read(t, g, u1, u2); got.Run != "g" || got.Status != "Succeeded" || len(got.Undecided) != 2 {
		t.Fatal("two undecided", got.Run, got.Status, got.Undecided)
	}
	// An undecided run at the same creation time as the green is a tie,
	// whichever name sorts first, not a newer run.
	for _, names := range [][2]string{{"a-green", "b-run"}, {"b-green", "a-run"}} {
		g, u := releaseCI(t, names[0], at), releaseCI(t, names[1], at)
		runningCI(t, u, "Running")
		older := releaseCI(t, "older", at.Add(-time.Minute))
		if got := read(t, g, u, older); got.Status != "Unknown" || got.Reason != "ci_tie" {
			t.Fatal("same-time undecided was treated as newer", names, got.Run, got.Status, got.Reason)
		}
	}
}

func withScope(t *testing.T, o *unstructured.Unstructured, scopes ...string) *unstructured.Unstructured {
	t.Helper()
	r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
	for _, s := range scopes {
		r = append(r, map[string]any{"name": APIScopeResult, "value": s})
	}
	setField(t, o, r, "status", "results")
	return o
}

// R5:5.7-1 runs only part of the Go tests for a console-only change and says
// so in api-scope. Such a green (normally the landing CI) is set aside: it
// cannot release P, cannot hide a red, and does not stand in for main's full
// run. A run without the result predates it and is full.
func TestReleaseCIOnlyFullScopeGreensRelease(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	red := func(o *unstructured.Unstructured) *unstructured.Unstructured {
		setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		return o
	}
	running := func(o *unstructured.Unstructured) *unstructured.Unstructured { runningCI(t, o, "Running"); return o }
	for name, tc := range map[string]struct {
		objs                []runtime.Object
		run, status, reason string
		partial, undecided  int
	}{
		"console green over full green":              {[]runtime.Object{releaseCI(t, "full", at.Add(-time.Minute)), withScope(t, releaseCI(t, "console", at), "console")}, "full", "Succeeded", "", 1, 0},
		"console green over full red":                {[]runtime.Object{red(releaseCI(t, "red", at.Add(-time.Minute))), withScope(t, releaseCI(t, "console", at), "console")}, "red", "Failed", "", 1, 0},
		"console green alone":                        {[]runtime.Object{withScope(t, releaseCI(t, "console", at), "console")}, "", "Missing", "", 1, 0},
		"main running over console green":            {[]runtime.Object{withScope(t, releaseCI(t, "console", at.Add(-time.Minute)), "console"), running(releaseCI(t, "main", at))}, "main", "Running", "", 1, 0},
		"main full green over console green":         {[]runtime.Object{withScope(t, releaseCI(t, "console", at.Add(-time.Minute)), "console"), withScope(t, releaseCI(t, "main", at), "full")}, "main", "Succeeded", "", 1, 0},
		"console red over full green":                {[]runtime.Object{releaseCI(t, "full", at.Add(-time.Minute)), withScope(t, red(releaseCI(t, "console", at)), "console")}, "console", "Failed", "", 0, 0},
		"running over full green over console green": {[]runtime.Object{withScope(t, releaseCI(t, "console", at.Add(-2*time.Minute)), "console"), withScope(t, releaseCI(t, "land", at.Add(-time.Minute)), "full"), running(releaseCI(t, "main", at))}, "land", "Succeeded", "", 1, 1},
		"unknown scope":                              {[]runtime.Object{releaseCI(t, "full", at.Add(-time.Minute)), withScope(t, releaseCI(t, "odd", at), "partial")}, "odd", "Unknown", "ci_results", 0, 0},
		"duplicate scope":                            {[]runtime.Object{releaseCI(t, "full", at.Add(-time.Minute)), withScope(t, releaseCI(t, "twice", at), "full", "console")}, "twice", "Unknown", "ci_results", 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := drillService(t, tc.objs...)
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil {
				t.Fatal(err)
			}
			if got.Run != tc.run || got.Status != tc.status || got.Reason != tc.reason || len(got.Partial) != tc.partial || len(got.Undecided) != tc.undecided {
				t.Fatalf("got run=%q status=%s reason=%q partial=%v undecided=%v", got.Run, got.Status, got.Reason, got.Partial, got.Undecided)
			}
			if _, recErr := got.Record(); (recErr == nil) != (tc.status == "Succeeded") {
				t.Fatal("accepted evidence must be exactly a full-scope green", recErr)
			}
		})
	}
	// Record refuses a console-scope green even if a caller hands one over.
	c, _ := drillService(t, releaseCI(t, "full", at))
	got, err := c.ReadRelease(context.Background(), factSHA1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := got.Record(); err != nil {
		t.Fatal("control", err)
	}
	got.Results[APIScopeResult] = APIScopeConsole
	if _, err := got.Record(); err == nil {
		t.Fatal("a console-scope green became accepted evidence")
	}
}
