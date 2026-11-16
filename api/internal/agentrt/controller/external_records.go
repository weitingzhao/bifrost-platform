package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
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
	// evidenced is the listed runs whose evidence is settled (in the ledger,
	// or the run is none), by run and uid; trees is the tree of each commit
	// read for them, by repository and commit. Neither is emptied with docs:
	// a completed run and a commit's tree do not change.
	evidenced map[string]bool
	trees     map[string]string
	// known is the completed runs, by run and uid, the last pass listed or
	// the event path recorded since (RecordPipelineRun); listed says a pass
	// has listed once in this process; last is when the last pass began
	// (pipelineTick). A reconciliation pass counts a run it records that
	// known lacks as missed by the events.
	known  map[string]bool
	listed bool
	last   time.Time
}

// PipelineRecords records completed runs; the source omits unfinished runs.
// Only the runs the memo does not hold are compared with the database, and a
// run enters the memo once the database has it; every PipelineFullEvery the
// memo is emptied first. A run whose fields the vocabulary refuses is
// reported at every pass and never sent.
func (c *Controller) PipelineRecords(ctx context.Context) (int, error) {
	return c.pipelineRecords(ctx, false)
}

// pipelineRecords is a pass; a reconciliation pass (the bus wakes the
// records) is always a full one and counts what the events missed.
func (c *Controller) pipelineRecords(ctx context.Context, reconcile bool) (int, error) {
	if c.Sources.PipelineRuns == nil {
		return 0, nil
	}
	// The startup backfill, the loop and the event path do not list or
	// record at once.
	m := &c.pipelines
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = c.clock()
	full := c.PipelineFullEvery
	if full <= 0 {
		full = DefaultPipelineFullEvery
	}
	if now := time.Now(); reconcile || now.Sub(m.full) >= full {
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
	byKey := make(map[string]store.PipelineRun, len(r.runs))
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
		byKey[key] = p
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
			if reconcile && m.listed {
				c.countMissedByEvents(m, sent)
			}
		}
	}
	m.docs = held
	m.known, m.listed = make(map[string]bool, len(byKey)), true
	for key := range byKey {
		m.known[key] = true
	}
	if n > 0 && c.PipelineRecorded != nil {
		c.PipelineRecorded()
	}
	// The evidence of the runs this pass compared with the database: every
	// new run, and every listed run on a full pass, so one that failed here
	// is tried again within PipelineFullEvery. It is written beside the
	// pipeline_seen records and changes nothing about them.
	evidenced := make(map[string]bool, len(byKey))
	for key := range byKey {
		if m.evidenced[key] {
			evidenced[key] = true
		}
	}
	todo := map[string]store.PipelineRun{}
	for key, p := range byKey {
		if !evidenced[key] && sent[key] != "" && held[key] != "" {
			todo[key] = p
		}
	}
	errs = append(errs, c.recordEvidence(ctx, m, todo, evidenced)...)
	m.evidenced = evidenced
	return n, errors.Join(errs...)
}

// recordEvidence writes the evidence of the runs in todo (by run and uid)
// beside their pipeline_seen records and marks each settled in evidenced. A
// tree that cannot be read stops it: the rest wait for the next full pass
// rather than each wait out a read of its own.
func (c *Controller) recordEvidence(ctx context.Context, m *pipelineMemo, todo map[string]store.PipelineRun, evidenced map[string]bool) []error {
	var errs []error
	var rows []evidence.EvidenceRow
	var rowKeys []string
	for key, p := range todo {
		e, is, err := c.runEvidence(ctx, m, &p)
		if err != nil {
			errs = append(errs, fmt.Errorf("evidence of run %s: %w", p.Run, err))
			break
		}
		if !is {
			evidenced[key] = true
			continue
		}
		rows, rowKeys = append(rows, e), append(rowKeys, key)
		if c.Sources.RunEvidenced != nil {
			// Before the row is written: the reader can still ask the ledger
			// whether it knew the run.
			c.Sources.RunEvidenced(ctx, p)
		}
	}
	if len(rows) > 0 {
		if _, err := evidence.RecordEvidence(ctx, c.DB, rows); err != nil {
			errs = append(errs, err)
		} else {
			for _, key := range rowKeys {
				evidenced[key] = true
			}
		}
	}
	return errs
}

// runEvidence is the evidence a completed run is (evidence.RunEvidence), after
// reading from Sources.CommitTree the tree of each commit the run reports
// without one. The trees and what a failed run failed on are written to p.
func (c *Controller) runEvidence(ctx context.Context, m *pipelineMemo, p *store.PipelineRun) (evidence.EvidenceRow, bool, error) {
	if p.Kind != "ci" || len(p.Inputs) == 0 {
		return evidence.EvidenceRow{}, false, nil
	}
	inputs := append([]store.RepoAt(nil), p.Inputs...)
	for i, in := range inputs {
		if in.Tree != "" || in.Commit == "" {
			continue
		}
		if c.Sources.CommitTree == nil {
			return evidence.EvidenceRow{}, false, nil
		}
		at := in.Repo + "@" + in.Commit
		tree, ok := m.trees[at]
		if !ok {
			type result struct {
				tree string
				err  error
			}
			r := bounded(c, ctx, func(ctx context.Context) result {
				tree, err := c.Sources.CommitTree(ctx, in.Repo, in.Commit)
				return result{tree, err}
			})
			if r.err != nil {
				return evidence.EvidenceRow{}, false, r.err
			}
			if m.trees == nil {
				m.trees = map[string]string{}
			}
			tree, m.trees[at] = r.tree, r.tree
		}
		inputs[i].Tree = tree
	}
	p.Inputs = inputs
	if p.Result == "Failed" && p.Reason == "Failed" && c.Sources.RunFailure != nil {
		type result struct {
			tasks, tests []string
			class        string
			err          error
		}
		r := bounded(c, ctx, func(ctx context.Context) result {
			tasks, tests, class, err := c.Sources.RunFailure(ctx, p.Run)
			return result{tasks, tests, class, err}
		})
		if r.err != nil {
			return evidence.EvidenceRow{}, false, r.err
		}
		p.FailedTasks, p.FailedTests, p.FailClass = r.tasks, r.tests, r.class
	}
	return evidence.RunEvidence(EvidenceRun(*p))
}

// EvidenceRun is the store's record of a pipeline run as the evidence ledger
// reads it (evidence.Run). It is the one conversion between the two: every
// field of evidence.Run is set here (TestEvidenceRunCarriesEveryField).
func EvidenceRun(p store.PipelineRun) evidence.Run {
	var inputs []evidence.RepoAt
	for _, in := range p.Inputs {
		inputs = append(inputs, evidence.RepoAt(in))
	}
	return evidence.Run{Kind: p.Kind, Repo: p.Repo, Run: p.Run, UID: p.UID, Pipeline: p.Pipeline,
		Result: p.Result, Reason: p.Reason, Scope: p.Scope, Base: p.Base, TestedBy: p.TestedBy, CompletedAt: p.CompletedAt,
		Inputs: inputs, Audit: p.Audit, FailedTasks: p.FailedTasks, FailedTests: p.FailedTests, FailClass: p.FailClass}
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
