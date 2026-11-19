package gatecluster

import (
	"fmt"
	"testing"
	"time"
)

// 2026-11-16: bus step 2 — pipeline records woken by ci.run.finished:
// gate-cluster's OnFinished queues the run for the controller and returns at
// once, also when the queue is full (that run is left to the reconciliation,
// which counts it missed); the controller is wired to the queue.
func TestRunFinishedQueuesWithoutBlocking(t *testing.T) {
	var b FinishedRuns
	q := b.Queue()
	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(q)+10; i++ {
			b.Add(fmt.Sprintf("run-%d", i), "bifrost-ci-platform")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnFinished blocked on a full queue")
	}
	if len(q) != cap(q) || <-q != "run-0" {
		t.Fatalf("queue %d of %d", len(q), cap(q))
	}
}
