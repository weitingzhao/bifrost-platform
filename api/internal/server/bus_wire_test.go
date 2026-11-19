package server

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
)

// 2026-11-16: bus step 2: the controller is wired to gate-cluster's queue of
// finished runs (gatecluster.FinishedRuns).
func TestPipelineRecordsAreWiredToFinishedRuns(t *testing.T) {
	s := &Server{}
	c := &controller.Controller{}
	s.wirePipelineRecords(c)
	if c.PipelineFinished == nil {
		t.Fatal("the controller is not wired to gate-cluster's queue")
	}
}
