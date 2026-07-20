package operatequeue

import "time"

// EvidenceFunc adapts a closure to EvidenceSource.
type EvidenceFunc func() (EvidenceBundle, error)

func (f EvidenceFunc) LoadEvidence() (EvidenceBundle, error) {
	if f == nil {
		return EvidenceBundle{Now: time.Now().UTC(), QuietNominal: true}, nil
	}
	return f()
}

// BundleFromSignals builds EvidenceBundle from cached probe rows.
func BundleFromSignals(signals []EvidenceSignal, now time.Time) EvidenceBundle {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := EvidenceBundle{
		Now:          now,
		QuietNominal: true,
		Signals:      append([]EvidenceSignal(nil), signals...),
	}
	if len(out.Signals) == 0 {
		// Empty cache: do not claim nominal (avoids false 48h auto-dismiss).
		out.QuietNominal = false
		return out
	}
	for _, s := range out.Signals {
		if s.Signal == "fail" || s.Signal == "degraded" {
			out.QuietNominal = false
			break
		}
	}
	return out
}
