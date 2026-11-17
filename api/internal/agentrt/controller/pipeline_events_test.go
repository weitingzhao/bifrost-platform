package controller_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// testClock is a clock the test moves.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// 2026-11-16: bus step 2 — pipeline records woken by ci.run.finished record
// the run the event names at once, through the same pipeline_seen and
// evidence path as the loop: a failed CI run is asked what it failed on once,
// a second event for it writes nothing, and the loop's pass after it finds
// the run recorded (no second record, no second RunFailure read).
func TestRecordPipelineRunRecordsOneRunOnce(t *testing.T) {
	pool := setup(t)
	red := ciRun("ci-platform-red-ev", sha("a"), sha("c"))
	red.Result, red.Reason = "Failed", "Failed"
	asked, gets, told := 0, 0, 0
	c := controller.Controller{DB: pool, PipelineRecorded: func() { told++ }, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { return []seenfacts.PipelineRun{red}, nil },
		PipelineRun: func(_ context.Context, name string) (seenfacts.PipelineRun, bool, error) {
			gets++
			if name != red.Run {
				return seenfacts.PipelineRun{}, false, nil
			}
			return red, true, nil
		},
		CommitTree: func(context.Context, string, string) (string, error) { return sha("e"), nil },
		RunFailure: func(context.Context, string) ([]string, []string, string, error) {
			asked++
			return []string{"test-api"}, []string{"example.com/m/a.TestA"}, evidence.FailClassTest, nil
		}}}
	byEvent0, missed0 := controller.PipelineEventCountsForTest()
	for i := 0; i < 2; i++ {
		recorded, err := c.RecordPipelineRun(ctx, red.Run)
		if err != nil || recorded != (i == 0) {
			t.Fatalf("event %d: recorded %v, err %v", i, recorded, err)
		}
	}
	if recorded, err := c.RecordPipelineRun(ctx, "gone"); recorded || err != nil {
		t.Fatalf("a run the cluster no longer keeps: %v %v", recorded, err)
	}
	if n, err := c.PipelineRecords(ctx); n != 0 || err != nil {
		t.Fatalf("the loop after the event: recorded %d, err %v", n, err)
	}
	if got := pipelineSeenCount(t, pool, red.Run); got != 1 {
		t.Fatalf("pipeline_seen %d times", got)
	}
	if rows := evidenceRows(t, pool); len(rows) != 1 || rows[0].run != red.Run {
		t.Fatalf("evidence %v", rows)
	}
	if asked != 1 || told != 1 || gets != 3 {
		t.Fatalf("RunFailure asked %d, told %d, run read %d", asked, told, gets)
	}
	byEvent, missed := controller.PipelineEventCountsForTest()
	if byEvent-byEvent0 != 1 || missed != missed0 {
		t.Fatalf("by event +%d, missed +%d", byEvent-byEvent0, missed-missed0)
	}
	// A read that fails says so and records nothing.
	away := errors.New("cluster away")
	c.Sources.PipelineRun = func(context.Context, string) (seenfacts.PipelineRun, bool, error) {
		return seenfacts.PipelineRun{}, false, away
	}
	if _, err := c.RecordPipelineRun(ctx, "x"); !errors.Is(err, away) {
		t.Fatalf("a failed read: %v", err)
	}
}

// 2026-11-16: bus step 2 — while gate-cluster names the finished runs the
// records' 30-second loop is only the reconciliation (bus/RULES.md §8): its
// pass over every run comes once per PipelineReconcileEvery; without the bus
// every tick is a pass, as before.
func TestPipelineTickReconcilesEveryFiveMinutesWhileLive(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	live, lists := false, 0
	c := controller.Controller{DB: noDB{}, Now: clock.now, PipelineLive: func() bool { return live }, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { lists++; return nil, nil }}}
	tick := func(want int) {
		t.Helper()
		if _, err := c.PipelineTickForTest(ctx); err != nil {
			t.Fatal(err)
		}
		if lists != want {
			t.Fatalf("lists %d, want %d", lists, want)
		}
		clock.add(30 * time.Second)
	}
	tick(1) // without the bus: every tick lists
	tick(2)
	live = true
	for i := 0; i < 9; i++ { // 30 s .. 4 min 30 s after the last pass
		tick(2)
	}
	tick(3) // 5 minutes after: the reconciliation
	tick(3)
	live = false
	tick(4) // the bus gone: every tick lists again
	tick(5)
}

// 2026-11-16: bus step 2 — the side-by-side reading: a run the event path
// recorded is not counted by the reconciliation; one it missed (its
// ci.run.finished never came) is recorded by the reconciliation and counted,
// by name, as missed by the events. Runs the first pass of the process
// finds are not counted: nothing was there to miss them.
func TestReconciliationRecordsAndCountsWhatTheEventsMissed(t *testing.T) {
	pool := setup(t)
	clock := &testClock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	runs := []seenfacts.PipelineRun{completedRun("deliver-old")}
	var infos []string
	c := controller.Controller{DB: pool, Now: clock.now, PipelineLive: func() bool { return true },
		Infof: func(format string, args ...any) { infos = append(infos, format) },
		Sources: controller.Sources{
			PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { return runs, nil },
			PipelineRun: func(_ context.Context, name string) (seenfacts.PipelineRun, bool, error) {
				for _, p := range runs {
					if p.Run == name {
						return p, true, nil
					}
				}
				return seenfacts.PipelineRun{}, false, nil
			}}}
	_, missed0 := controller.PipelineEventCountsForTest()
	if _, err := c.PipelineTickForTest(ctx); err != nil { // the first pass of the process
		t.Fatal(err)
	}
	runs = append(runs, completedRun("deliver-by-event"), completedRun("deliver-missed"))
	if recorded, err := c.RecordPipelineRun(ctx, "deliver-by-event"); !recorded || err != nil {
		t.Fatalf("event: %v %v", recorded, err)
	}
	clock.add(controller.PipelineReconcileEvery)
	n, err := c.PipelineTickForTest(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reconciliation recorded %d, err %v", n, err)
	}
	for _, run := range []string{"deliver-old", "deliver-by-event", "deliver-missed"} {
		if got := pipelineSeenCount(t, pool, run); got != 1 {
			t.Fatalf("%s recorded %d times", run, got)
		}
	}
	if _, missed := controller.PipelineEventCountsForTest(); missed-missed0 != 1 {
		t.Fatalf("missed +%d, want 1 (deliver-missed)", missed-missed0)
	}
	if len(infos) != 1 || !strings.Contains(infos[0], "had not") {
		t.Fatalf("logged %v", infos)
	}
	// The next reconciliation finds nothing new: nothing more is counted.
	clock.add(controller.PipelineReconcileEvery)
	if _, err := c.PipelineTickForTest(ctx); err != nil {
		t.Fatal(err)
	}
	if _, missed := controller.PipelineEventCountsForTest(); missed-missed0 != 1 {
		t.Fatalf("missed +%d after a quiet reconciliation", missed-missed0)
	}
}
