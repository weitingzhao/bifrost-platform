package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// StgRunObservation is a named GET, checked against the started identity by
// the delivery adapter. Unknown includes 403/timeouts; it is never a 404.
type StgRunObservation struct {
	IntentSeq, PermitSeq, BoundSeq int64
	Kind, UID                      string // found, not_found, collision, unknown
	ReadAt                         time.Time
	FinalizerGuaranteed            bool
}

// StgRecoveryDecision is pure, and does not consult live, rules or lease.
// Existing execution is always observable, including under stg_release:none.
func StgRecoveryDecision(t *StgTry, obs StgRunObservation, at time.Time) string {
	if t == nil || !t.Occupied() || obs.IntentSeq != t.Started.Seq || obs.ReadAt.IsZero() || obs.ReadAt.After(at) {
		return "wait"
	}
	if obs.Kind == "found" && obs.UID != "" {
		if t.Bound.Seq > 0 && t.Bound.Record.String("uid") != obs.UID {
			return "object_lost"
		}
		return "bound"
	}
	// A GET begun while unbound may return 404 just before another observer
	// binds a newly created object. Re-read instead of calling it lost.
	if obs.BoundSeq != t.Bound.Seq {
		return "wait"
	}
	if t.IsQuarantined() {
		return "wait"
	}
	if obs.Kind == "collision" {
		if t.Bound.Seq > 0 {
			return "object_lost"
		}
		return "collision"
	}
	if obs.Kind != "not_found" {
		return "wait"
	}
	if t.Bound.Seq > 0 {
		return "object_lost"
	}
	if obs.PermitSeq != t.Permit.Seq || obs.ReadAt.Before(t.Permit.Record.Time("deadline")) || at.Before(t.Permit.Record.Time("deadline")) {
		return "wait"
	}
	if at.Sub(obs.ReadAt) > 2*time.Minute {
		return "wait"
	}
	// A permit deadline fences dispatch, not a POST already accepted by the
	// API server. Even with retention, this GET may precede that POST's
	// persistence. Without a server-side fence proving it cannot complete,
	// neither resend nor unsent is justified. Keep the slot until readback
	// supplies terminal evidence (or the Owner explicitly closes isolation).
	return "unknown_execution"
}

// RecordStgRecovery serializes a readback with other observers and senders.
// Absence cannot grant another send or end execution occupancy. A matching
// late UID remains bindable even while quarantined and with live/none off.
func RecordStgRecovery(ctx context.Context, db DB, batch string, try int64, obs StgRunObservation) (string, error) {
	return recordStgRecovery(ctx, db, "", batch, try, obs)
}

// RecordStgControlRecovery preserves B5-c recovery semantics while honoring
// B5-e's stronger none contract, including a rule change during the GET.
func RecordStgControlRecovery(ctx context.Context, db DB, tenant, batch string, try int64, obs StgRunObservation) (string, error) {
	return recordStgRecovery(ctx, db, tenant, batch, try, obs)
}
func recordStgRecovery(ctx context.Context, db DB, tenant, batch string, try int64, obs StgRunObservation) (string, error) {
	out := "wait"
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = "wait"
		if tenant != "" {
			enabled, err := stgControlEnabled(ctx, tx, tenant, func() bool { return true })
			if err != nil || !enabled {
				return err
			}
		}
		if err := lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		b := s.Batches[batch]
		if b == nil || b.Phase == "CLOSED" || b.Try != try {
			return nil
		}
		at := DecisionAt()
		out = StgRecoveryDecision(b.Tries[try], obs, at)
		switch out {
		case "bound":
			_, err = appendStg(ctx, tx, stgBatchRecord("bound", batch, try, "uid", obs.UID))
		case "object_lost", "collision", "unknown_execution":
			if b.Tries[try].IsQuarantined() {
				out = "wait"
				return nil
			}
			_, err = appendStg(ctx, tx, stgBatchRecord("quarantined", batch, try, "why", out, "at", at.Format(time.RFC3339Nano)))
		}
		return err
	})
	return out, err
}
