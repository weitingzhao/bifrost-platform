package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ExternalExecution interface {
	ListSucceededSTG(context.Context) ([]*unstructured.Unstructured, error)
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

// manualRunsNeeded says whether only a recorded manual run can move B5 on
// (R5:5.7-11): with no trusted baseline the gate has nothing to compare live
// Deployments with, and a block of one of store.StgManualRunCauses is lifted
// only by a newer source=external record. The time is the trusted baseline's
// deployed.at, zero when there is none: a run created before B5's current
// deployment cannot be what STG runs now.
func manualRunsNeeded(s store.StgState) (bool, time.Time) {
	base, known := store.StgTrustedBaseline(s)
	if !known {
		return true, time.Time{}
	}
	at := store.StgDeployedAt(s, base.String("evidence"))
	for _, b := range s.Blocks {
		if b.IsOpen() && slices.Contains(store.StgManualRunCauses, b.Opened.Record.String("why")) {
			return true, at
		}
	}
	return false, at
}

// ExternalPass observes only: no lease, live gate, Create or debit. The
// signed none rule stops it before any I/O, as required by the rollout brief.
// It is scheduled independently of Pass so a bad external read never prevents
// archiving an automatic run, and automatic/external scheduling order is safe.
// It reads the cluster only while manualRunsNeeded.
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
	needed, after := manualRunsNeeded(s)
	if !needed {
		return nil
	}
	start, listed, observed := time.Now(), 0, 0
	defer func() {
		if took := time.Since(start); took >= time.Second {
			slog.Info("STG manual run read took "+took.Round(time.Millisecond).String(), "listed", listed, "observed", observed)
		}
	}()
	var errs []error
	runs, err := x.ListSucceededSTG(ctx)
	if err != nil {
		errs = append(errs, err) // runs already archived stay archived
	}
	listed = len(runs)
	for _, run := range runs {
		id := fmt.Sprintf("%s/%s/%s", c.Cluster, run.GetNamespace(), run.GetUID())
		if !run.GetCreationTimestamp().After(after) || s.Evidence[id].Seq > 0 || automaticRun(s, run) {
			continue
		}
		observed++
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
