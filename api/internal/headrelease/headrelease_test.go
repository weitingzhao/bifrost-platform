package headrelease

import (
	"context"
	"strings"
	"testing"
)

// world is main, the ledger and the cluster as a test sets them.
type world struct {
	heads  map[string]string
	ci     map[string]string            // repo@commit -> Succeeded | Failed
	beside map[string]map[string]string // repo@commit -> what its CI ran with
	runs   map[string][]Run             // by pipeline, newest first
	busy   bool
	starts int
}

func (w *world) Head(_ context.Context, repo string) (string, error) { return w.heads[repo], nil }
func (w *world) CI(_ context.Context, repo, commit string) (string, string, map[string]string, error) {
	run := ""
	if w.ci[repo+"@"+commit] != "" {
		run = "ci-" + commit
	}
	return w.ci[repo+"@"+commit], run, w.beside[repo+"@"+commit], nil
}
func (w *world) Runs(_ context.Context, pipeline string) ([]Run, error) { return w.runs[pipeline], nil }
func (w *world) Start(_ context.Context, pipeline string, params map[string]string) (string, bool, error) {
	if w.busy {
		return "", true, nil
	}
	w.starts++
	name := pipeline + "-" + string(rune('0'+w.starts))
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	w.runs[pipeline] = append([]Run{{Name: name, Params: copied, Status: RunRunning}}, w.runs[pipeline]...)
	return name, false, nil
}

// green makes a commit of platform green, tested with ui u and infra i.
func (w *world) green(commit string) {
	w.ci["bifrost-platform@"+commit] = CISucceeded
	w.beside["bifrost-platform@"+commit] = map[string]string{"bifrost-ui": "u1", "bifrost-trade-infra": "i1"}
}

func (w *world) end(pipeline, status string) { w.runs[pipeline][0].Status = status }

var platform = Deliverable{Name: "platform-stg", Enabled: true, Environment: "stg", Pipeline: "bifrost-deliver-platform", Commits: []Commit{
	{Repo: "bifrost-platform", Param: "revision"},
	{Repo: "bifrost-ui", Param: "uiRevision", TestedWith: "bifrost-platform"},
	{Repo: "bifrost-trade-infra", Param: "infraRevision", TestedWith: "bifrost-platform"},
}}

func newWorld() *world {
	return &world{heads: map[string]string{}, ci: map[string]string{}, beside: map[string]map[string]string{}, runs: map[string][]Run{}}
}

func step(t *testing.T, w *world, d Deliverable, action string) Outcome {
	t.Helper()
	out, err := Step(context.Background(), w, d)
	if err != nil || out.Action != action {
		t.Fatalf("%s (%s) %v, want %s", out.Action, out.Why, err, action)
	}
	return out
}

// 2026-11-13 15:53-16:07 Chicago (ci-feedback 1): three lines each released
// their own commit while main moved; three deliveries failed with "Main
// moved". Here main moves three times while one delivery runs: nothing more
// starts until it ends, and then one delivery of the newest head starts.
func TestHeadsThatMoveDuringADeliveryMakeOneMoreDelivery(t *testing.T) {
	w := newWorld()
	w.heads["bifrost-platform"] = "a"
	w.green("a")
	first := step(t, w, platform, Started)
	// What it starts is the head, with the commits its CI ran beside it:
	// nobody reads a CI run by hand for them (ci-feedback 4).
	if first.Commits["revision"] != "a" || first.Commits["uiRevision"] != "u1" || first.Commits["infraRevision"] != "i1" {
		t.Fatalf("started %v", first.Commits)
	}
	for _, head := range []string{"b", "c", "d"} {
		w.heads["bifrost-platform"] = head
		w.green(head)
		step(t, w, platform, Waiting)
	}
	if w.starts != 1 {
		t.Fatalf("%d deliveries started while one ran", w.starts)
	}
	w.end("bifrost-deliver-platform", RunSucceeded)
	out := step(t, w, platform, Started)
	if out.Commits["revision"] != "d" || w.starts != 2 {
		t.Fatalf("after the run: %v, %d starts", out.Commits, w.starts)
	}
	// Owner 2026-11-13: a start leaves a trace that can be judged later:
	// the CI run it was judged green by and what the environment ran before.
	if out.Evidence["bifrost-platform"] != "ci-d" || out.PreviousRun != first.Run || out.Previous["revision"] != "a" {
		t.Fatalf("the trace of a start: evidence %v previous %s %v", out.Evidence, out.PreviousRun, out.Previous)
	}
	w.end("bifrost-deliver-platform", RunSucceeded)
	step(t, w, platform, Delivered)
	step(t, w, platform, Delivered)
	if w.starts != 2 {
		t.Fatalf("a delivered head was delivered again: %d starts", w.starts)
	}
}

// PROD ledger, 2026-11-13: bifrost-platform c53ea00d has two passing CI rows,
// ci-platform-5d6bb beside infra cf70db50 and ci-platform-pgdsv beside infra
// 1d5a1842 (the daily full run reads a newer infra). A delivered head whose
// ledger row changes to the other one is not delivered again.
func TestAnotherCIRowOfTheSameHeadIsNotANewDelivery(t *testing.T) {
	w := newWorld()
	w.heads["bifrost-platform"] = "a"
	w.green("a")
	step(t, w, platform, Started)
	w.end("bifrost-deliver-platform", RunSucceeded)
	w.beside["bifrost-platform@a"] = map[string]string{"bifrost-ui": "u1", "bifrost-trade-infra": "i2"}
	step(t, w, platform, Delivered)
	if w.starts != 1 {
		t.Fatalf("%d starts", w.starts)
	}
}

// 2026-11-13 15:45-15:58 Chicago (ci-feedback 2, 8): a window held by a
// person for ten minutes. The loop does not take it from them; it waits and
// starts when the window is free.
func TestAHeldWindowIsWaitedFor(t *testing.T) {
	w := newWorld()
	w.heads["bifrost-platform"] = "a"
	w.green("a")
	w.busy = true
	step(t, w, platform, Waiting)
	step(t, w, platform, Waiting)
	w.busy = false
	step(t, w, platform, Started)
	if w.starts != 1 {
		t.Fatalf("%d starts", w.starts)
	}
}

// The ledger decides (2026-11-13: the gate reads the CI runs' rows): a head
// with no verdict yet waits, a red head is not released, and a set of commits
// whose delivery failed is not delivered again until the head moves.
func TestOnlyAGreenHeadIsReleasedAndAFailedOneNotTwice(t *testing.T) {
	w := newWorld()
	w.heads["bifrost-platform"] = "a"
	step(t, w, platform, Waiting) // CI still running
	w.ci["bifrost-platform@a"] = CIFailed
	step(t, w, platform, Refused)
	if w.starts != 0 {
		t.Fatal("a head that is not green was released")
	}
	w.heads["bifrost-platform"] = "b"
	w.green("b")
	step(t, w, platform, Started)
	w.end("bifrost-deliver-platform", RunFailed)
	step(t, w, platform, Refused)
	step(t, w, platform, Refused)
	if w.starts != 1 {
		t.Fatalf("a failed delivery was started again: %d", w.starts)
	}
	w.heads["bifrost-platform"] = "c"
	w.green("c")
	step(t, w, platform, Started)
	// A CI that does not say what it ran beside the head names no delivery.
	w.end("bifrost-deliver-platform", RunSucceeded)
	w.heads["bifrost-platform"] = "e"
	w.ci["bifrost-platform@e"] = CISucceeded
	step(t, w, platform, Refused)
	// A deliverable that is not enabled starts nothing.
	off := platform
	off.Enabled = false
	w.green("e")
	step(t, w, off, Waiting)
	if w.starts != 2 {
		t.Fatalf("%d starts", w.starts)
	}
}

// Owner 2026-11-13: the structure holds the hardest case from the start. A
// deliverable of several repositories is one set of heads: it is released
// when every one is green, once, and a move of any of them is a new set.
func TestADeliverableOfSeveralHeadsIsReleasedAsOneSet(t *testing.T) {
	set := Deliverable{Name: "several", Enabled: true, Environment: "stg", Pipeline: "deliver-several", Commits: []Commit{
		{Repo: "core", Param: "coreRevision"}, {Repo: "api", Param: "apiRevision"}, {Repo: "ui", Param: "uiRevision", TestedWith: "api"},
	}}
	w := newWorld()
	w.heads["core"], w.heads["api"] = "c1", "a1"
	w.ci["core@c1"] = CISucceeded
	step(t, w, set, Waiting) // api has no verdict
	w.ci["api@a1"] = CISucceeded
	w.beside["api@a1"] = map[string]string{"ui": "u7"}
	out := step(t, w, set, Started)
	if out.Commits["coreRevision"] != "c1" || out.Commits["apiRevision"] != "a1" || out.Commits["uiRevision"] != "u7" {
		t.Fatalf("the set: %v", out.Commits)
	}
	w.end("deliver-several", RunSucceeded)
	step(t, w, set, Delivered)
	w.heads["core"] = "c2"
	w.ci["core@c2"] = CISucceeded
	if out = step(t, w, set, Started); out.Commits["coreRevision"] != "c2" || out.Commits["apiRevision"] != "a1" {
		t.Fatalf("after core moved: %v", out.Commits)
	}
}

// What a line asks instead of releasing: is my commit in what runs there.
func TestWhereReleased(t *testing.T) {
	w := newWorld()
	w.runs["bifrost-deliver-platform"] = []Run{
		{Name: "r3", Params: map[string]string{"revision": "c"}, Status: RunFailed},
		{Name: "r2", Params: map[string]string{"revision": "b"}, Status: RunSucceeded},
		{Name: "r1", Params: map[string]string{"revision": "a"}, Status: RunSucceeded},
	}
	within := func(_ context.Context, _, ancestor, commit string) (bool, error) { return ancestor <= commit, nil }
	table := Table{Deliverables: []Deliverable{platform}}
	for commit, want := range map[string]bool{"a": true, "b": true, "c": false} {
		got, err := WhereReleased(context.Background(), w, table, "bifrost-platform", commit, within)
		if err != nil || len(got) != 1 || got[0].Commit != "b" || got[0].Run != "r2" || got[0].Contains != want {
			t.Errorf("commit %s: %+v %v", commit, got, err)
		}
	}
	if got, _ := WhereReleased(context.Background(), w, table, "another-repo", "a", within); len(got) != 0 {
		t.Errorf("a repository no deliverable holds: %+v", got)
	}
}

// The table that ships: it parses, the platform's row is the one enabled,
// and no row releases to PROD or holds a Trade repository enabled (Owner
// 2026-11-13: nothing of trading is connected).
func TestTheTableThatShips(t *testing.T) {
	table, _, err := LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, d := range table.Deliverables {
		for _, c := range d.Commits {
			if d.Enabled && strings.HasPrefix(c.Repo, "bifrost-trade-") && c.TestedWith == "" {
				t.Errorf("%s releases the head of %s", d.Name, c.Repo)
			}
		}
		if d.Enabled {
			enabled++
		}
	}
	if enabled != 1 || table.Deliverables[0].Pipeline != "bifrost-deliver-platform" {
		t.Errorf("the enabled rows: %d", enabled)
	}
	// The PROD row is there to be read, and a step never starts it: the
	// release policy alone releases to PROD (design release-head.md, PROD).
	for _, d := range table.Deliverables {
		if !d.prod() {
			continue
		}
		w := newWorld()
		w.heads["bifrost-platform"] = "a"
		w.green("a")
		d.Enabled = true // even a row someone enabled past the parser
		if out, err := Step(context.Background(), w, d); err != nil || out.Action != Waiting || w.starts != 0 {
			t.Errorf("%s: a step on a PROD row: %+v %v, started %d", d.Name, out, err, w.starts)
		}
	}
	for name, doc := range map[string]string{
		"a row for PROD":            "deliverables: [{name: x, enabled: true, environment: prod, pipeline: p, commits: [{repo: r, param: revision}]}]",
		"a PROD pipeline":           "deliverables: [{name: x, enabled: true, environment: stg, pipeline: deliver-prod, commits: [{repo: r, param: revision}]}]",
		"no head":                   "deliverables: [{name: x, environment: stg, pipeline: p, commits: [{repo: r, param: revision, tested_with: q}]}]",
		"tested with a non-head":    "deliverables: [{name: x, environment: stg, pipeline: p, commits: [{repo: r, param: revision}, {repo: u, param: ui, tested_with: q}]}]",
		"a parameter named twice":   "deliverables: [{name: x, environment: stg, pipeline: p, commits: [{repo: r, param: revision}, {repo: q, param: revision}]}]",
		"an unknown field in a row": "deliverables: [{name: x, environment: stg, pipeline: p, auto_prod: true, commits: [{repo: r, param: revision}]}]",
	} {
		if _, err := ParseTable([]byte(doc)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
