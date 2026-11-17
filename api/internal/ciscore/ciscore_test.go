package ciscore

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Origin of every case in this file — Owner 2026-11-16: CI scores every
// repository that uses it, and its users score CI; only what is measured is
// an item (pass, fail or unmeasured with the raw number), and the overall
// verdict is the worst item.

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// run completed an hour ago and took secs; results are name=value pairs.
func run(name string, secs int, results ...string) Run {
	done := now.Add(-time.Hour)
	r := Run{Name: name, Created: done.Add(-time.Duration(secs+1) * time.Second), Started: done.Add(-time.Duration(secs) * time.Second),
		Completed: done, Results: map[string]string{}}
	for _, kv := range results {
		k, v, _ := strings.Cut(kv, "=")
		r.Results[k] = v
	}
	return r
}

func runs(n, secs int, prefix string, results ...string) []Run {
	var out []Run
	for i := 0; i < n; i++ {
		out = append(out, run(fmt.Sprintf("%s-%d", prefix, i), secs, results...))
	}
	return out
}

func failed(rs []Run) []Run {
	for i := range rs {
		rs[i].Failed = true
	}
	return rs
}

func scope(runName, modules string) Task {
	return Task{Run: runName, Task: ScopeTask, Results: map[string]string{"run-modules": modules}}
}

func find(t *testing.T, items []Item, id string) Item {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s in %+v", id, items)
	return Item{}
}

func want(t *testing.T, it Item, verdict, value string) {
	t.Helper()
	if it.Verdict != verdict || !strings.Contains(it.Value, value) {
		t.Errorf("%s = %s %q, want %s with %q", it.ID, it.Verdict, it.Value, verdict, value)
	}
}

func repoItem(t *testing.T, r Repo, id string) Item {
	t.Helper()
	return find(t, scoreRepo(r, now).Items, id)
}

// Origin: as the file's.
func TestLandingTime(t *testing.T) {
	want(t, repoItem(t, Repo{Runs: runs(5, 100, "r")}, "landing_time"), Pass, "median 100 s")
	want(t, repoItem(t, Repo{Runs: runs(5, 121, "r")}, "landing_time"), Fail, "median 121 s")
	want(t, repoItem(t, Repo{Runs: runs(4, 100, "r")}, "landing_time"), Unmeasured, "4 runs, needs 5")

	// Only the run of a ready branch that tested it itself is a sample: not a
	// run with a base, one that reused a verdict, the daily full run, one
	// still running, or one that completed before the window.
	rs := runs(4, 100, "r")
	based := run("based", 100)
	based.Base = "0123abcd"
	reused := run("reused", 100, "tested-by=r-0")
	full := run("full", 100)
	full.Full = true
	running := run("running", 100)
	running.Completed = time.Time{}
	old := run("old", 100)
	old.Completed = now.Add(-73 * time.Hour)
	want(t, repoItem(t, Repo{Runs: append(rs, based, reused, full, running, old)}, "landing_time"), Unmeasured, "4 runs")
	// All zeros is git's "no commit": the first push of a branch has no base.
	zero := run("zero", 100)
	zero.Base = strings.Repeat("0", 40)
	want(t, repoItem(t, Repo{Runs: append(rs, zero)}, "landing_time"), Pass, "median 100 s")
}

// Origin: as the file's.
func TestFollowsChange(t *testing.T) {
	mix := func(inc, incSecs, full, fullSecs int) Repo {
		return Repo{Runs: append(runs(inc, incSecs, "i", "api-scope=incremental"), runs(full, fullSecs, "f", "api-scope=full")...)}
	}
	want(t, repoItem(t, mix(3, 60, 3, 100), "follows_change"), Pass, "0.60 (incremental 60 s / full 100 s)")
	want(t, repoItem(t, mix(3, 90, 3, 100), "follows_change"), Fail, "0.90")
	want(t, repoItem(t, mix(2, 60, 3, 100), "follows_change"), Unmeasured, "2 incremental and 3 full runs, needs 3 of each")
	want(t, repoItem(t, mix(3, 60, 2, 100), "follows_change"), Unmeasured, "3 incremental and 2 full runs")
	// A pipeline without the result (infra's) is not measured, and says why.
	want(t, repoItem(t, Repo{Runs: runs(9, 60, "r")}, "follows_change"), Unmeasured, "reports no api-scope")
}

// Origin: as the file's.
func TestDeclaresUnits(t *testing.T) {
	want(t, repoItem(t, Repo{Declaration: Declaration{Declared: true, Units: 7}}, "declares_units"), Pass, "7 modules")
	want(t, repoItem(t, Repo{Declaration: Declaration{}}, "declares_units"), Fail, "no bifrost.units.yaml")
	want(t, repoItem(t, Repo{Declaration: Declaration{Declared: true}}, "declares_units"), Fail, "0 modules")
	// 2026-11-16, STG: the first version built the whole plan under a 20 s limit, the build
	// timed out, and bifrost-platform (two modules declared) was scored "declares 0 units".
	want(t, repoItem(t, Repo{Declaration: Declaration{Declared: true, Invalid: "yaml: line 3"}}, "declares_units"), Fail, "does not parse: yaml: line 3")
	want(t, repoItem(t, Repo{Declaration: Declaration{Err: "gitea: HTTP 502"}}, "declares_units"), Unmeasured, "gitea: HTTP 502")
}

// Origin: as the file's.
func TestSidesSeparated(t *testing.T) {
	with := func(n int, modules ...string) Repo {
		r := Repo{Runs: runs(n, 100, "r")}
		for i, m := range modules {
			r.Tasks = append(r.Tasks, scope(fmt.Sprintf("r-%d", i), m))
		}
		return r
	}
	want(t, repoItem(t, with(5, "*", "*", " api repo ", " api console ", "*"), "sides_separated"), Pass, "console skipped in 1 of 5 runs")
	want(t, repoItem(t, with(5, "*", "*", " console ", " api console ", "*"), "sides_separated"), Fail, "console skipped in 0 of 5 runs")
	want(t, repoItem(t, with(5, " api ", " api ", " api ", " api "), "sides_separated"), Unmeasured, "console skipped in 4 of 4 runs, needs 5")
	// No scope task, or none with the result (infra): not measured.
	want(t, repoItem(t, with(5), "sides_separated"), Unmeasured, "no changes task reports run-modules")
	noResult := with(5)
	noResult.Tasks = []Task{{Run: "r-0", Task: ScopeTask, Results: map[string]string{}}, {Run: "r-1", Task: "clone", Results: map[string]string{"run-modules": " api "}}}
	want(t, repoItem(t, noResult, "sides_separated"), Unmeasured, "no changes task reports run-modules")
	// A run that reused a verdict ran nothing: its scope task is no sample.
	reused := with(5, "*", "*", "*", "*", "*")
	reused.Runs = append(reused.Runs, run("again", 10, "tested-by=r-0"))
	reused.Tasks = append(reused.Tasks, scope("again", " api "))
	want(t, repoItem(t, reused, "sides_separated"), Fail, "0 of 5")
	unread := with(5, "*")
	unread.TasksErr = "context deadline exceeded"
	want(t, repoItem(t, unread, "sides_separated"), Unmeasured, "context deadline exceeded")
}

// Origin: as the file's.
func TestRedRatio(t *testing.T) {
	want(t, repoItem(t, Repo{Runs: append(runs(9, 10, "g"), failed(runs(1, 10, "r"))...)}, "red_ratio"), Pass, "1 failed of 10")
	want(t, repoItem(t, Repo{Runs: append(runs(8, 10, "g"), failed(runs(2, 10, "r"))...)}, "red_ratio"), Fail, "2 failed of 10")
	want(t, repoItem(t, Repo{}, "red_ratio"), Unmeasured, "no completed run")
}

// Origin: as the file's.
func TestTier(t *testing.T) {
	for tier, rs := range map[string][]Run{
		"selective": append(runs(3, 10, "f", "api-scope=full"), run("i", 10, "api-scope=incremental")),
		"full":      runs(3, 10, "f"),
		"none":      nil,
	} {
		if got := scoreRepo(Repo{Runs: rs}, now).Tier; got != tier {
			t.Errorf("tier = %s, want %s", got, tier)
		}
	}
}

// Origin: as the file's — the overall verdict is the worst item: fail before
// unmeasured before pass.
func TestWorst(t *testing.T) {
	p, f, u := Item{Verdict: Pass}, Item{Verdict: Fail}, Item{Verdict: Unmeasured}
	for _, c := range []struct {
		items []Item
		want  string
	}{{[]Item{p, p}, Pass}, {[]Item{p, u}, Unmeasured}, {[]Item{u, f, p}, Fail}, {[]Item{f, u}, Fail}} {
		if got := Worst(c.items); got != c.want {
			t.Errorf("Worst(%v) = %s, want %s", c.items, got, c.want)
		}
	}
	// And of a repository: four passing items and an unread declaration.
	r := Repo{Runs: append(runs(3, 50, "i", "api-scope=incremental"), runs(3, 100, "f", "api-scope=full")...), Declaration: Declaration{Err: "x"}}
	for i := 0; i < 6; i++ {
		r.Tasks = append(r.Tasks, scope(r.Runs[i].Name, " api "))
	}
	if got := scoreRepo(r, now); got.Overall != Unmeasured {
		t.Errorf("overall = %s of %+v, want unmeasured", got.Overall, got.Items)
	}
	r.Declaration = Declaration{Declared: true, Units: 2}
	if got := scoreRepo(r, now); got.Overall != Pass {
		t.Errorf("overall = %s of %+v, want pass", got.Overall, got.Items)
	}
	r.Declaration = Declaration{}
	if got := scoreRepo(r, now); got.Overall != Fail {
		t.Errorf("overall = %s of %+v, want fail", got.Overall, got.Items)
	}
}

func providerItem(t *testing.T, in Input, id string) Item {
	t.Helper()
	in.Now = now
	return find(t, scoreProvider(in).Items, id)
}

// Origin: as the file's.
func TestPushToVerdict(t *testing.T) {
	want(t, providerItem(t, Input{Land: runs(3, 150, "l", "outcome=landed")}, "push_to_verdict"), Pass, "median 150 s")
	want(t, providerItem(t, Input{Land: runs(3, 151, "l", "outcome=landed")}, "push_to_verdict"), Fail, "median 151 s")
	// A merge that did not land is no sample.
	want(t, providerItem(t, Input{Land: runs(3, 10, "l", "outcome=refused")}, "push_to_verdict"), Unmeasured, "no merge landed")
}

// Origin: as the file's.
func TestStartLag(t *testing.T) {
	with := func(lag time.Duration) Input {
		rs := runs(3, 100, "r")
		in := Input{Repos: []Repo{{Runs: rs}}}
		for _, r := range rs {
			// The earliest of the tasks read is the first; a later one does not count.
			in.Repos[0].Tasks = append(in.Repos[0].Tasks, Task{Run: r.Name, Task: ScopeTask, Started: r.Created.Add(lag + time.Minute)},
				Task{Run: r.Name, Task: "clone", Started: r.Created.Add(lag)})
		}
		return in
	}
	want(t, providerItem(t, with(15*time.Second), "start_lag"), Pass, "median 15 s")
	want(t, providerItem(t, with(16*time.Second), "start_lag"), Fail, "median 16 s")
	want(t, providerItem(t, Input{Repos: []Repo{{Runs: runs(3, 100, "r")}}}, "start_lag"), Unmeasured, "no run with a started first task")
	unread := with(time.Second)
	unread.Repos[0].TasksErr = "forbidden"
	want(t, providerItem(t, unread, "start_lag"), Unmeasured, "forbidden")
}

// Origin: as the file's.
func TestOwnFailures(t *testing.T) {
	in := Input{Repos: []Repo{{Runs: append(failed(runs(2, 10, "red")), runs(5, 10, "green")...)}}, Land: failed(runs(1, 10, "land"))}
	in.FailedTasks = []Task{{Run: "red-0", Task: "test-api", Failed: true}, {Run: "red-0", Task: "clone", Failed: false},
		{Run: "red-1", Task: "test-console", Failed: true}, {Run: "land-0", Task: "land", Failed: true}}
	want(t, providerItem(t, in, "own_failures"), Pass, "0 of 3 failed runs failed in CI's own tasks")
	// One of CI's own tasks failing is CI's failure, whichever it is.
	for _, task := range []string{"clone", "clone-ui", "reuse", "changes", "resolve", "freeze-check"} {
		own := in
		own.FailedTasks = append(append([]Task{}, in.FailedTasks...), Task{Run: "red-1", Task: task, Failed: true})
		want(t, providerItem(t, own, "own_failures"), Fail, "1 of 3 failed runs")
	}
	// A failed run none of whose tasks failed did not get to a test.
	none := in
	none.FailedTasks = in.FailedTasks[:3]
	want(t, providerItem(t, none, "own_failures"), Fail, "1 of 3 failed runs")
	unread := in
	unread.FailedTasksErr = "forbidden"
	want(t, providerItem(t, unread, "own_failures"), Unmeasured, "forbidden")
	want(t, providerItem(t, Input{}, "own_failures"), Pass, "0 of 0 failed runs")
}

// Origin: as the file's; the cleanup item is from 2026-11-15, when the job
// that deletes old runs had stopped for 43 hours from 04:21Z and the 5,900
// pods left behind took Prometheus down.
func TestCleanupKeepsUp(t *testing.T) {
	want(t, providerItem(t, Input{Cleanup: now.Add(-2 * time.Hour)}, "cleanup_keeps_up"), Pass, "2.0 h since it last succeeded")
	// Positive control of the case above: the stall of 2026-11-15 must fail.
	want(t, providerItem(t, Input{Cleanup: now.Add(-43 * time.Hour)}, "cleanup_keeps_up"), Fail, "43.0 h since it last succeeded")
	want(t, providerItem(t, Input{CleanupErr: "cronjobs.batch is forbidden"}, "cleanup_keeps_up"), Unmeasured, "forbidden")
	want(t, providerItem(t, Input{}, "cleanup_keeps_up"), Unmeasured, "no successful run")
}

// Origin: as the file's — the pass lines are published with the answer, one
// for every item, and CI is scored by the same rule as its users.
func TestScore(t *testing.T) {
	out := Score(Input{Now: now, Repos: []Repo{{Repo: "a", Pipeline: "ci-a"}}, Cleanup: now.Add(-43 * time.Hour)})
	if out.WindowDays != 3 || out.GeneratedAt != "2026-11-16T12:00:00Z" || len(out.Repos) != 1 || out.Repos[0].Repo != "a" {
		t.Fatalf("answer = %+v", out)
	}
	if out.Provider.Overall != Fail {
		t.Errorf("provider overall = %s, want fail (the cleanup stalled)", out.Provider.Overall)
	}
	published := map[string]bool{}
	for _, s := range out.Standards {
		if s.Label == "" || s.PassWhen == "" {
			t.Errorf("standard %+v lacks a label or a pass line", s)
		}
		published[s.ID] = true
	}
	items := append(append([]Item{}, out.Repos[0].Items...), out.Provider.Items...)
	if len(items) != 9 || len(out.Standards) != 9 {
		t.Fatalf("%d items and %d standards, want 9 of each", len(items), len(out.Standards))
	}
	for _, it := range items {
		if !published[it.ID] || it.Label == it.ID {
			t.Errorf("item %s has no published standard", it.ID)
		}
	}
}
