package controller

import (
	"context"
	"sync"
	"time"
)

// RunLoopForTest runs pass as the controller runs a loop named name, every d,
// until ctx ends, and waits for it.
func (c *Controller) RunLoopForTest(ctx context.Context, name string, d time.Duration, pass func() error) {
	var wg sync.WaitGroup
	c.every(ctx, &wg, name, d, pass)
	wg.Wait()
}

// ReadTuningForTest reads the tuning rule as a pass does.
func (c *Controller) ReadTuningForTest(ctx context.Context) { c.readTuning(ctx) }

// PipelineTickForTest is the pipeline records loop's call.
func (c *Controller) PipelineTickForTest(ctx context.Context) (int, error) {
	return c.pipelineTick(ctx)
}

// PipelineEventCountsForTest reads the two side-by-side counters.
func PipelineEventCountsForTest() (byEvent, missed int64) {
	return metricRecordsByEvent.Load(), metricRecordsMissedByEvents.Load()
}
