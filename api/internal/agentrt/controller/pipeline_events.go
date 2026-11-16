package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// PipelineReconcileEvery is the pipeline records loop's pass interval while
// the bus names the finished runs (Controller.PipelineLive): the pass over
// every retained run is then only the reconciliation (bus/RULES.md §8). The
// missed-by-events count (bifrost_bus_records_missed_by_events_total) decides
// whether it can grow.
const PipelineReconcileEvery = 5 * time.Minute

// pipelineTick is the loop's call: a pass every PipelineEvery while the bus
// does not name the finished runs, else a reconciliation pass once per
// PipelineReconcileEvery.
func (c *Controller) pipelineTick(ctx context.Context) (int, error) {
	if c.PipelineLive == nil || !c.PipelineLive() {
		return c.PipelineRecords(ctx)
	}
	m := &c.pipelines
	m.mu.Lock()
	last := m.last
	m.mu.Unlock()
	if !last.IsZero() && c.clock().Sub(last) < PipelineReconcileEvery {
		return 0, nil
	}
	return c.pipelineRecords(ctx, true)
}

// startPipelineEvents records each run PipelineFinished names, one at a time,
// until ctx ends.
func (c *Controller) startPipelineEvents(ctx context.Context, wg *sync.WaitGroup) {
	if c.PipelineFinished == nil || c.Sources.PipelineRun == nil {
		return
	}
	wg.Add(1)
	safego.Go("agentrt-pipeline-records-events", func() {
		defer wg.Done()
		for {
			var run string
			select {
			case <-ctx.Done():
				return
			case run = <-c.PipelineFinished:
			}
			completed := safego.Do("agentrt-pipeline-records-event", func() {
				if _, err := c.RecordPipelineRun(ctx, run); err != nil && ctx.Err() == nil {
					c.logf("agentrt-pipeline-records: run %s from ci.run.finished: %v", run, err)
				}
			})
			if !completed {
				c.logf("agentrt-pipeline-records: recording run %s from ci.run.finished panicked", run)
			}
		}
	})
}

// RecordPipelineRun records the one run a ci.run.finished names, as a pass
// records each listed run: its pipeline_seen record (store.RecordExternal
// compares it with the database by run and uid, so a run the loop recorded
// is not written twice) and its evidence, with what a failed run failed on
// (Sources.RunFailure). It says whether this call wrote the record; a run the
// cluster no longer keeps, or not completed yet, is left to the loop.
func (c *Controller) RecordPipelineRun(ctx context.Context, name string) (bool, error) {
	if c.Sources.PipelineRun == nil {
		return false, nil
	}
	type result struct {
		p   store.PipelineRun
		ok  bool
		err error
	}
	r := bounded(c, ctx, func(ctx context.Context) result {
		p, ok, err := c.Sources.PipelineRun(ctx, name)
		return result{p, ok, err}
	})
	if r.err != nil {
		return false, fmt.Errorf("pipeline_seen: %w", r.err)
	}
	if !r.ok {
		return false, nil
	}
	p := r.p
	b := p.Body()
	if err := store.CheckExternal("pipeline_seen", b); err != nil {
		return false, fmt.Errorf("run %s: %w", p.Run, err)
	}
	doc, err := canon.Encode(b)
	if err != nil {
		return false, fmt.Errorf("run %s: %w", p.Run, err)
	}
	key := p.Run + "\x00" + p.UID
	m := &c.pipelines
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.docs[key] == string(doc) {
		if m.known == nil {
			m.known = map[string]bool{}
		}
		m.known[key] = true
		return false, nil
	}
	n, err := store.RecordExternal(ctx, c.DB, "pipeline_seen", []map[string]any{b})
	if err != nil {
		return false, err
	}
	if m.docs == nil {
		m.docs = map[string]string{}
	}
	if m.known == nil {
		m.known = map[string]bool{}
	}
	m.docs[key], m.known[key] = string(doc), true
	if n > 0 {
		metricRecordsByEvent.Add(1)
		if c.PipelineRecorded != nil {
			c.PipelineRecorded()
		}
	}
	if m.evidenced[key] {
		return n > 0, nil
	}
	if m.evidenced == nil {
		m.evidenced = map[string]bool{}
	}
	return n > 0, errors.Join(c.recordEvidence(ctx, m, map[string]store.PipelineRun{key: p}, m.evidenced)...)
}

// countMissedByEvents counts and names the runs a reconciliation pass
// recorded that neither the event path nor the pass before had: what the
// events missed. Called under the memo's lock after the write succeeded.
func (c *Controller) countMissedByEvents(m *pipelineMemo, sent map[string]string) {
	var missed []string
	for key := range sent {
		if !m.known[key] {
			run, _, _ := strings.Cut(key, "\x00")
			missed = append(missed, run)
		}
	}
	if len(missed) == 0 {
		return
	}
	sort.Strings(missed)
	metricRecordsMissedByEvents.Add(int64(len(missed)))
	c.infof("agentrt-pipeline-records: the reconciliation recorded %d run(s) ci.run.finished had not: %s", len(missed), strings.Join(missed, ", "))
}

var metricRecordsByEvent, metricRecordsMissedByEvents atomic.Int64

// WritePipelineEventMetrics writes the side-by-side reading of the event path
// and the reconciliation; a process that runs neither writes zeros.
func WritePipelineEventMetrics(b *strings.Builder) {
	for _, m := range []struct {
		name, help string
		v          int64
	}{
		{"bifrost_bus_records_by_event_total", "Completed pipeline runs the records recorded when ci.run.finished named them", metricRecordsByEvent.Load()},
		{"bifrost_bus_records_missed_by_events_total", "Completed pipeline runs the records' reconciliation pass recorded that ci.run.finished had not named", metricRecordsMissedByEvents.Load()},
	} {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", m.name, m.help, m.name, m.name, m.v)
	}
}
