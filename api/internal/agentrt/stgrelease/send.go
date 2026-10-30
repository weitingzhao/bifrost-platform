// Package stgrelease supplies B5's opt-in STG release controller, durable
// reconciliation, fenced send/recovery operations and window holders.
package stgrelease

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Runs interface {
	ReadNamedDeliverRun(context.Context, delivery.NamedDeliverInput) (*unstructured.Unstructured, error)
	CreatePermittedNamedDeliverRun(context.Context, delivery.NamedDeliverInput, func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error)
}

type Sender struct {
	DB   store.DB
	Runs Runs
	Gate store.StgGateSource
	// B5-d holder for the current try. B5-e supplies this together with its
	// Generation/Observe callbacks; nil retains the one-shot B5-c API.
	Window *WindowHolder
	// Set by B5-e to recheck none under the rule lock on recovery writes.
	ControlTenant string
	// True only after the admission/retention probes succeeded (§14).
	FinalizerGuaranteed bool
}

func input(batch string, try, intent int64, inputs store.StgRecord) delivery.NamedDeliverInput {
	n, _ := strconv.ParseInt(strings.TrimPrefix(batch, "stg:bifrost-platform:"), 10, 64)
	return delivery.NamedDeliverInput{Batch: n, Try: try, IntentSeq: intent, P: inputs.String("revision"), U: inputs.String("uiRevision"), Infra: inputs.String("infraRevision")}
}
func object(r store.StgRecord, key string) store.StgRecord {
	m, _ := r[key].(map[string]any)
	return store.StgRecord(m)
}

// Start grants and spends once, then sends outside the transaction. Only the
// caller that committed the returned permit may send it. An ambiguous commit
// or response is recovered by GET, never by repeating this method's POST.
func (s Sender) Start(ctx context.Context, batch string, try int64) (store.StgGateDecision, error) {
	p, d, err := store.AuthorizeStgRelease(ctx, s.DB, batch, try, s.Gate, nil)
	if err != nil || !d.Allowed {
		return d, err
	}
	return d, s.send(ctx, p)
}

func (s Sender) send(ctx context.Context, p store.StgSendPermit) error {
	if s.Runs == nil || s.Gate.Generation == nil {
		return errors.New("STG sender unavailable")
	}
	in := input(p.Batch, p.Try, p.IntentSeq, p.Inputs)
	check := func(ctx context.Context) error {
		current, err := store.StgPermitCurrent(ctx, s.DB, p, s.Gate.Generation(), store.DecisionAt())
		if err != nil {
			return err
		}
		if s.Window != nil {
			if s.Window.batch != p.Batch || s.Window.try != p.Try {
				return errors.New("STG send window belongs to another try")
			}
			w, err := s.Window.Observe(ctx)
			if err != nil {
				return err
			}
			if w.Generation != p.Generation {
				return errors.New("STG send window generation changed")
			}
		}
		// The read itself can stall across the deadline or a lease takeover.
		if !current || !store.DecisionAt().Before(p.Deadline) || s.Gate.Generation() != p.Generation {
			return errors.New("STG send permit expired")
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return err
	}
	_, obj, err := s.Runs.CreatePermittedNamedDeliverRun(ctx, in, check)
	if err != nil {
		// Create's 409/lost response is resolved by the same bounded GET path.
		obs, readErr := s.observe(ctx, p.IntentSeq, p.PermitSeq, 0, in)
		if readErr != nil {
			return readErr
		}
		action, readErr := s.recordRecovery(ctx, p.Batch, p.Try, obs)
		if readErr != nil {
			return readErr
		}
		if action == "bound" {
			return nil
		}
		return err
	}
	if err = delivery.MatchNamedDeliverRun(obj, in, true); err != nil {
		_, e := s.recordRecovery(ctx, p.Batch, p.Try, store.StgRunObservation{IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, Kind: "collision", ReadAt: store.DecisionAt()})
		return e
	}
	_, err = s.recordRecovery(ctx, p.Batch, p.Try, store.StgRunObservation{IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, Kind: "found", UID: string(obj.GetUID()), ReadAt: store.DecisionAt()})
	return err
}

func (s Sender) observe(ctx context.Context, intent, permit, bound int64, in delivery.NamedDeliverInput) (store.StgRunObservation, error) {
	obs := store.StgRunObservation{IntentSeq: intent, PermitSeq: permit, BoundSeq: bound, FinalizerGuaranteed: s.FinalizerGuaranteed, Kind: "unknown"}
	if s.Runs == nil {
		return obs, errors.New("STG run reader unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	obj, err := s.Runs.ReadNamedDeliverRun(ctx, in)
	obs.ReadAt = store.DecisionAt()
	switch {
	case apierrors.IsNotFound(err):
		obs.Kind = "not_found"
	case errors.Is(err, delivery.ErrSTGCollision):
		obs.Kind = "collision"
	case err == nil:
		if delivery.MatchNamedDeliverRun(obj, in, true) != nil {
			obs.Kind = "collision"
		} else {
			obs.Kind = "found"
			obs.UID = string(obj.GetUID())
		}
	}
	return obs, nil // transport failure remains an unknown observation
}

// Recover observes even with live off/none. An expired, unbound 404 isolates
// execution: a previously dispatched POST may still complete. Recovery never
// sends again; matching late objects remain observable while quarantined.
func (s Sender) Recover(ctx context.Context, batch string, try int64) (string, error) {
	state, err := store.ReadStgRelease(ctx, s.DB)
	if err != nil {
		return "", err
	}
	b := state.Batches[batch]
	if b == nil || b.Try != try || b.Tries[try] == nil || !b.Tries[try].Occupied() {
		return "wait", nil
	}
	t := b.Tries[try]
	in := input(batch, try, t.Started.Seq, object(t.Snapshot.Record, "inputs"))
	obs, err := s.observe(ctx, t.Started.Seq, t.Permit.Seq, t.Bound.Seq, in)
	if err != nil {
		return "", err
	}
	return s.recordRecovery(ctx, batch, try, obs)
}

func (s Sender) recordRecovery(ctx context.Context, batch string, try int64, obs store.StgRunObservation) (string, error) {
	if s.ControlTenant != "" {
		return store.RecordStgControlRecovery(ctx, s.DB, s.ControlTenant, batch, try, obs)
	}
	return store.RecordStgRecovery(ctx, s.DB, batch, try, obs)
}
