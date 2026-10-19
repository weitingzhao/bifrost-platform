package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func benchRun(id string, passes, fails int) store.BenchmarkRun {
	r := store.BenchmarkRun{RunID: id, Executor: "exec:codex", Provider: "openai", Model: "gpt-x",
		Questions: strings.Repeat("0f", 32), StartedAt: time.Now().Add(-time.Hour)}
	for i := 0; i < passes+fails; i++ {
		outcome := "pass"
		if i >= passes {
			outcome = "fail"
		}
		r.Results = append(r.Results, store.BenchmarkResult{Question: fmt.Sprintf("Q%02d", i+1), Class: "go", Outcome: outcome, WallS: 60 * (i + 1)})
	}
	r.Results = append(r.Results, store.BenchmarkResult{Question: "Q99", Class: "ts", Outcome: "unrunnable", Reason: "no node here"})
	for _, x := range r.Results {
		r.QuestionIDs = append(r.QuestionIDs, x.Question)
	}
	return r
}

// W-31 2.4 slice ⑤: a run is recorded once, one row a class it has a sample
// of (an unrunnable question counts for nobody), and refused when it does not
// read as a run.
func TestBenchmarkRecord(t *testing.T) {
	w := newKernelWorld(t)
	run := benchRun("run-a-000001", 2, 1)
	out, err := store.RecordBenchmark(ctx, w.rt, "h1", run)
	ok(t, err)
	if out["duplicate"] != false || out["rows"] != 1 {
		t.Fatalf("record: %v", out)
	}
	var attempts, firstPass int
	var source, class string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempts, first_pass, source, task_class FROM agentrt.capability_score WHERE source = 'benchmark'`).
		Scan(&attempts, &firstPass, &source, &class))
	if attempts != 3 || firstPass != 2 || class != "go" {
		t.Fatalf("row: %d attempts, %d first pass, %s %s", attempts, firstPass, source, class)
	}
	again, err := store.RecordBenchmark(ctx, w.rt, "h1", run)
	ok(t, err)
	if again["duplicate"] != true || again["event_id"] != out["event_id"] {
		t.Fatalf("again: %v", again)
	}
	if _, err = store.RecordBenchmark(ctx, w.rt, "h1", benchRun("run-a-000001", 0, 3)); err == nil {
		t.Fatal("other results under the same run id were taken")
	}
	for name, mutate := range map[string]func(*store.BenchmarkRun){
		"a deterministic run":   func(r *store.BenchmarkRun) { r.Executor = "exec:deterministic" },
		"an unknown executor":   func(r *store.BenchmarkRun) { r.Executor = "exec:nobody" },
		"a class with no tests": func(r *store.BenchmarkRun) { r.Results[0].Class = "doc" },
		"a question twice":      func(r *store.BenchmarkRun) { r.Results[1].Question = r.Results[0].Question },
		"an outcome unknown":    func(r *store.BenchmarkRun) { r.Results[0].Outcome = "maybe" },
		"a run from the future": func(r *store.BenchmarkRun) { r.StartedAt = time.Now().Add(time.Hour) },
		"no question set":       func(r *store.BenchmarkRun) { r.Questions = "" },
		"a reason of two lines": func(r *store.BenchmarkRun) { r.Results[0].Reason = "a\nb" },
		"no results":            func(r *store.BenchmarkRun) { r.Results = nil },
		"a question left out":   func(r *store.BenchmarkRun) { r.Results = r.Results[1:] },
		"a question not asked":  func(r *store.BenchmarkRun) { r.QuestionIDs[0] = "Q77" },
	} {
		r := benchRun("run-b-"+strings.ReplaceAll(name, " ", "-"), 1, 1)
		mutate(&r)
		if _, err := store.RecordBenchmark(ctx, w.rt, "h1", r); err == nil {
			t.Errorf("%s: recorded", name)
		}
	}
}

// ⑤: the latest run of an executor counts with its in-job samples in the
// competence step: codex failing every go question is not routed go work,
// and routed again once a newer run passes them.
func TestBenchmarkCountsForCompetence(t *testing.T) {
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, 30, 3))
	_, err := store.RecordBenchmark(ctx, w.rt, "h1", benchRun("run-c-000001", 0, 5))
	ok(t, err)
	task := w.ready(t, store.Task{})
	body := w.lastRoute(t, task)
	if strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("codex at 0 of 5 routed: %v", body["candidates"])
	}
	_, err = store.RecordBenchmark(ctx, w.rt, "h1", benchRun("run-c-000002", 5, 0))
	ok(t, err)
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	if body = w.lastRoute(t, task); n == 0 || !strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("codex at 5 of 5 (%d rerouted): %v", n, body["excluded"])
	}
}

// Review 287 B11, B12: a run is one per host and id, the same one again is
// the first record and another under its id is refused; the run that started
// last stands, whenever it was handed in.
func TestBenchmarkIdentityAndOrder(t *testing.T) {
	w := newKernelWorld(t)
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h2", "dev"))
	first, err := store.RecordBenchmark(ctx, w.rt, "h1", benchRun("run-d-000001", 1, 1))
	ok(t, err)
	other, err := store.RecordBenchmark(ctx, w.rt, "h2", benchRun("run-d-000001", 2, 0))
	ok(t, err)
	if other["duplicate"] != false || other["event_id"] == first["event_id"] {
		t.Fatalf("another host's run of the same id: %v", other)
	}
	if _, err = store.RecordBenchmark(ctx, w.rt, "h1", benchRun("run-d-000001", 2, 0)); err == nil {
		t.Fatal("other results under a run id already recorded")
	}
	w.legacy(t, routingRule(1, 30, 3))
	newer := benchRun("run-d-000002", 5, 0)
	newer.StartedAt = time.Now().Add(-time.Hour)
	_, err = store.RecordBenchmark(ctx, w.rt, "h1", newer)
	ok(t, err)
	late := benchRun("run-d-000003", 0, 5)
	late.StartedAt = time.Now().Add(-3 * 24 * time.Hour)
	_, err = store.RecordBenchmark(ctx, w.rt, "h1", late)
	ok(t, err)
	task := w.ready(t, store.Task{})
	if body := w.lastRoute(t, task); !strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("an older run handed in late stood for the newer one: %v", body["excluded"])
	}
}
