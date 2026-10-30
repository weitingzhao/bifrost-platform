package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ExternalExecution interface {
	ListSucceededSTG(context.Context) ([]*unstructured.Unstructured, error)
}

type CoverageLedger interface {
	Cover(context.Context, int64, string, store.StgMemberKey, store.MainEvidence) (bool, error)
}

func (l PostgresLedger) Cover(ctx context.Context, seq int64, id string, m store.StgMemberKey, ev store.MainEvidence) (bool, error) {
	return store.CommitStgCoverage(ctx, l.DB, l.Tenant, seq, id, m, ev)
}

// A matching started intent owns a run even before bound is durable. A
// bound UID stays reserved even if labels/annotations are later damaged.
func automaticRun(s store.StgState, run *unstructured.Unstructured) bool {
	for _, b := range s.Batches {
		for _, t := range b.Tries {
			if t.Bound.Record.String("uid") == string(run.GetUID()) {
				return true
			}
			a := run.GetAnnotations()
			if t.Started.Seq > 0 && a["bifrost.io/stg-release-intent"] == strconv.FormatInt(t.Started.Seq, 10) &&
				a["bifrost.io/stg-release-batch"] == b.Opened.Record.String("batch") &&
				run.GetName() == t.Started.Record.String("run") && run.GetNamespace() == object(t.Snapshot.Record, "inputs").String("namespace") {
				return true
			}
		}
	}
	return false
}

// ExternalPass observes only: no lease, live gate, Create or debit. The
// signed none rule stops it before any I/O, as required by the rollout brief.
// It is scheduled independently of Pass so a bad external read never prevents
// archiving an automatic run, and automatic/external scheduling order is safe.
func (c *Controller) ExternalPass(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.configured() {
		return nil
	}
	x, ok := c.Runs.(ExternalExecution)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	s, err := c.ledger().Read(ctx)
	if err != nil {
		return err
	}
	var errs []error
	runs, err := x.ListSucceededSTG(ctx)
	if err != nil {
		errs = append(errs, err) // already archived evidence can still cover members
	}
	for _, run := range runs {
		id := fmt.Sprintf("%s/%s/%s", c.Cluster, run.GetNamespace(), run.GetUID())
		if s.Evidence[id].Seq > 0 || automaticRun(s, run) {
			continue
		}
		r, err := c.externalEvidence(ctx, run, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		if err := c.write(ctx, s, r); err != nil {
			errs = append(errs, err)
		}
		s, err = c.ledger().Read(ctx)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	ledger, ok := c.ledger().(CoverageLedger)
	if ok && c.Sources.ReleaseEvidence != nil {
		for _, m := range store.StgCoverageCandidates(s) {
			for _, evidence := range orderedEvidence(s) {
				id, p := evidence.Record.String("evidence"), evidence.Record.String("P")
				at := c.now()
				ev, err := c.Sources.ReleaseEvidence(ctx, s, m, p)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if ev.Head != p || ev.Unreadable || !ev.HasR || (s.InMain[m.Op] != m.R && ev.Validity != "effective") {
					continue
				}
				if !c.configured() {
					return errors.Join(errs...)
				}
				ev.ReadAt = at
				_, err = ledger.Cover(ctx, s.LastSeq, id, m, ev)
				if err != nil {
					errs = append(errs, err)
				}
				s, err = c.ledger().Read(ctx)
				if err != nil {
					return errors.Join(append(errs, err)...)
				}
				break
			}
		}
	}
	// Pass alone settles batches. A terminal archive is no longer occupied,
	// but still owes input verification and the remaining members' budget
	// decision. Coverage must never bypass either by closing the batch here.
	return errors.Join(errs...)
}

func (c *Controller) externalEvidence(ctx context.Context, run *unstructured.Unstructured, id string) (store.StgRecord, error) {
	obs, err := c.Runs.ObserveRun(ctx, delivery.STGRunIdentity{Namespace: run.GetNamespace(), Name: run.GetName(), UID: run.GetUID()})
	if err != nil {
		return nil, err
	}
	content, err := obs.ExternalContent()
	if err != nil {
		return nil, err
	}
	workloads, err := c.Runs.ObserveWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	if !workloadsMatch(workloads, content.APIDigest, content.ConsoleDigest) {
		return nil, delivery.ErrSTGEvidence
	}
	if _, err := c.Runs.ReadPin(ctx, content.APIDigest, content.ConsoleDigest); err != nil {
		return nil, err
	}
	health := c.Runs.ReadHealth(ctx)
	if health.Outcome != "passed" {
		return nil, fmt.Errorf("external STG health %s", health.Outcome)
	}
	return store.StgRecord{"v": "1", "kind": "evidence", "scope": "ext", "evidence": id, "source": "external", "run": run.GetName(),
		"P": content.P, "U": content.U, "infra": content.Infra, "content": map[string]any{"api_digest": content.APIDigest, "console_digest": content.ConsoleDigest},
		"deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads, "at": c.now().Format(time.RFC3339Nano)}, "health": health.Evidence}, nil
}
