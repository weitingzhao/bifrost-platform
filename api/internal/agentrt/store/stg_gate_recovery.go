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

func stgValidNotFound(t *StgTry, p StgNotFoundProof, at time.Time) bool {
	return t != nil && t.Occupied() && !t.IsQuarantined() && t.Bound.Seq == 0 && p.FinalizerGuaranteed && p.IntentSeq == t.Started.Seq && p.PermitSeq == t.Permit.Seq && !p.ReadAt.IsZero() && !p.ReadAt.Before(t.Permit.Record.Time("deadline")) && !p.ReadAt.After(at) && at.Sub(p.ReadAt) <= 2*time.Minute
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
	if !obs.FinalizerGuaranteed {
		return "unknown_execution"
	}
	if at.Sub(obs.ReadAt) > 2*time.Minute {
		return "wait"
	}
	return "resend"
}

// RecordStgRecovery serializes a readback with other observers and senders.
// It returns resend only as a request to run the entire final gate again;
// this result itself is never a sending licence and never debits a budget.
func RecordStgRecovery(ctx context.Context, db DB, batch string, try int64, obs StgRunObservation) (string, error) {
	out := "wait"
	err := InTx(ctx, db, func(tx pgx.Tx) error {
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
