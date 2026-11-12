package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// ExternalRecords records every retained card, independently of watched rules.
func (c *Controller) ExternalRecords(ctx context.Context) (int, error) {
	n := 0
	var errs []error
	if c.Sources.Approvals != nil {
		k, err := recordList(c, ctx, "approval_seen", c.Sources.Approvals, func(a store.ApprovalFact) (map[string]any, error) { return a.Body(), nil })
		n += k
		errs = append(errs, err)
	}
	if c.Sources.Questions != nil {
		k, err := recordList(c, ctx, "question_seen", c.Sources.Questions, store.Question.Body)
		n += k
		errs = append(errs, err)
	}
	return n, errors.Join(errs...)
}

// DefaultPipelineEvery is how often the retained pipeline runs are listed
// when Controller.PipelineEvery is unset. A run that completed is recorded
// within one interval; the delivery strip and an agent checking its release
// read that record. Each pass lists every retained run (1.5 MB for 98 runs on
// 2026-11-12, several times that when a week of them is retained), so the
// interval is no shorter than the wait is worth.
const DefaultPipelineEvery = 30 * time.Second

// DefaultPipelineFullEvery is how often a pass forgets what it remembers and
// compares every listed run with the database, when
// Controller.PipelineFullEvery is unset: the interval of every pass before
// the memo. A record the database lost (a failover behind its primary) is
// written again within it.
const DefaultPipelineFullEvery = 5 * time.Minute

// pipelineMemo is the completed runs this process knows are recorded, each
// by run and uid with the fields recorded. A completed run does not change,
// so a pass over an unchanged list reads the list and not the database. It
// holds only the runs of the last list: a run the cluster no longer retains
// leaves it. full is when it was last emptied for a pass over every run.
type pipelineMemo struct {
	mu   sync.Mutex
	docs map[string]string
	full time.Time
}

// PipelineRecords records completed runs; the source omits unfinished runs.
// Only the runs the memo does not hold are compared with the database, and a
// run enters the memo once the database has it; every PipelineFullEvery the
// memo is emptied first. A run whose fields the vocabulary refuses is
// reported at every pass and never sent.
func (c *Controller) PipelineRecords(ctx context.Context) (int, error) {
	if c.Sources.PipelineRuns == nil {
		return 0, nil
	}
	// The startup backfill and the loop do not list or record at once.
	m := &c.pipelines
	m.mu.Lock()
	defer m.mu.Unlock()
	full := c.PipelineFullEvery
	if full <= 0 {
		full = DefaultPipelineFullEvery
	}
	if now := time.Now(); now.Sub(m.full) >= full {
		m.docs, m.full = nil, now
	}
	type result struct {
		runs []store.PipelineRun
		err  error
	}
	r := bounded(c, ctx, func(ctx context.Context) result {
		runs, err := c.Sources.PipelineRuns(ctx)
		return result{runs, err}
	})
	if r.err != nil {
		return 0, fmt.Errorf("pipeline_seen: %w", r.err)
	}
	held := make(map[string]string, len(r.runs))
	sent := map[string]string{}
	var facts []map[string]any
	var errs []error
	for _, p := range r.runs {
		b := p.Body()
		if err := store.CheckExternal("pipeline_seen", b); err != nil {
			errs = append(errs, fmt.Errorf("run %s: %w", p.Run, err))
			continue
		}
		doc, err := canon.Encode(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("run %s: %w", p.Run, err))
			continue
		}
		key := p.Run + "\x00" + p.UID
		if m.docs[key] == string(doc) {
			held[key] = string(doc)
			continue
		}
		if _, twice := sent[key]; twice {
			continue
		}
		facts, sent[key] = append(facts, b), string(doc)
	}
	n := 0
	if len(facts) > 0 {
		var err error
		if n, err = store.RecordExternal(ctx, c.DB, "pipeline_seen", facts); err != nil {
			// What was recorded of this batch is not known: none of it is
			// remembered, and the next pass compares it again.
			errs = append(errs, err)
		} else {
			for key, doc := range sent {
				held[key] = doc
			}
		}
	}
	m.docs = held
	if n > 0 && c.PipelineRecorded != nil {
		c.PipelineRecorded()
	}
	return n, errors.Join(errs...)
}

func recordList[T any](c *Controller, ctx context.Context, kind string, list func(context.Context) ([]T, error), body func(T) (map[string]any, error)) (int, error) {
	type result struct {
		facts []T
		err   error
	}
	r := bounded(c, ctx, func(ctx context.Context) result {
		facts, err := list(ctx)
		return result{facts, err}
	})
	if r.err != nil {
		return 0, fmt.Errorf("%s: %w", kind, r.err)
	}
	facts := make([]map[string]any, 0, len(r.facts))
	var errs []error
	for _, f := range r.facts {
		b, err := body(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		facts = append(facts, b)
	}
	n, err := store.RecordExternal(ctx, c.DB, kind, facts)
	return n, errors.Join(append(errs, err)...)
}

// startExternalRecords backfills on startup, sharing the loops' shutdown wait.
func (c *Controller) startExternalRecords(ctx context.Context, wg *sync.WaitGroup) {
	if c.Sources.Approvals == nil && c.Sources.Questions == nil && c.Sources.PipelineRuns == nil {
		return
	}
	wg.Add(1)
	safego.Go("agentrt-external-backfill", func() {
		defer wg.Done()
		_, cards := c.ExternalRecords(ctx)
		_, runs := c.PipelineRecords(ctx)
		if err := errors.Join(cards, runs); err != nil {
			c.logf("agentrt external backfill: %v", err)
		}
	})
}
