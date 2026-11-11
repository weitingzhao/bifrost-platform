package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

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

// PipelineRecords records completed runs; the source omits unfinished runs.
func (c *Controller) PipelineRecords(ctx context.Context) (int, error) {
	if c.Sources.PipelineRuns == nil {
		return 0, nil
	}
	return recordList(c, ctx, "pipeline_seen", c.Sources.PipelineRuns, func(p store.PipelineRun) (map[string]any, error) { return p.Body(), nil })
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
