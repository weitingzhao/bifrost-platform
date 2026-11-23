package server

import (
	"testing"

	verificationassembly "github.com/weitingzhao/bifrost-platform/api/internal/verification/assembly"
)

// 2026-11-16: bus step 2: the controller is wired to gate-cluster's queue of
// finished runs (gatecluster.FinishedRuns); the server lends the queue
// (verification/assembly sets it on the controller).
func TestPipelineRecordsAreWiredToFinishedRuns(t *testing.T) {
	s := &Server{}
	var lent verificationassembly.ControllerLent
	s.lendController(&lent)
	if lent.PipelineFinished == nil {
		t.Fatal("the controller is not lent gate-cluster's queue")
	}
}
