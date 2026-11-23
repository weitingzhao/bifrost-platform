// Moved from server on 2026-11-23

package assembly

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
)

func TestPipelineRecordsDurationFromEnv(t *testing.T) {
	for _, name := range []string{pipelineRecordsEveryEnv, pipelineRecordsFullEveryEnv} {
		for value, want := range map[string]time.Duration{
			"":      0, // the controller's default
			"15s":   15 * time.Second,
			" 1m ":  time.Minute,
			"5s":    5 * time.Second,
			"4s":    0, // under the floor: the default holds
			"-1m":   0,
			"30":    0, // not a duration
			"often": 0,
		} {
			t.Setenv(name, value)
			if got := pipelineRecordsDuration(name); got != want {
				t.Errorf("%s=%q: %s, want %s", name, value, got, want)
			}
		}
	}
	if controller.DefaultPipelineEvery < pipelineRecordsEveryMin || controller.DefaultPipelineFullEvery < controller.DefaultPipelineEvery {
		t.Fatalf("defaults: every %s, full every %s, floor %s", controller.DefaultPipelineEvery, controller.DefaultPipelineFullEvery, pipelineRecordsEveryMin)
	}
}

// The controller is given the intervals. The release recorder is told of a
// finished run by the bus (consumeReleaseRecords, N21), not by this loop.
func TestWirePipelineRecords(t *testing.T) {
	t.Setenv(pipelineRecordsEveryEnv, "20s")
	t.Setenv(pipelineRecordsFullEveryEnv, "10m")
	c := &controller.Controller{}
	finished := make(chan string)
	pipelineRecords(c, finished)
	if c.PipelineEvery != 20*time.Second || c.PipelineFullEvery != 10*time.Minute || c.PipelineRecorded != nil {
		t.Fatalf("every %s, full every %s, hook set %v", c.PipelineEvery, c.PipelineFullEvery, c.PipelineRecorded != nil)
	}
	// 2026-11-16: bus step 2: the controller is wired to gate-cluster's queue
	// of finished runs (gatecluster.FinishedRuns); server lends the queue.
	if c.PipelineFinished != (<-chan string)(finished) {
		t.Fatal("the controller is not wired to the queue it was lent")
	}
}
