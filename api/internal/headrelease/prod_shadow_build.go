package headrelease

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// ProdShadowHands is what the shadow is given by the process it runs in. It
// has no hands of its own: every one of these reads, except Audit, which
// writes the shadow's one record.
type ProdShadowHands struct {
	// Reads supplies the reads; called once the shadow is known to be on.
	Reads func() ProdReads
	// Batch reads the commits to has and from lacks in repo.
	Batch func(ctx context.Context, repo, from, to string) ProdBatch
	// Audit writes one record about target.
	Audit func(target, result, detail string)
	// Release releases to PROD by pipeline (mode on only; the server wires
	// it to the delivery service, as head-release's own runs).
	Release func(ctx context.Context, pipeline, revision string, params map[string]string) (run, wait string, err error)
	// Zone is the release hours' zone; nil sets no hours.
	Zone *time.Location
}

// NewProdShadow builds the shadow, or nil: the switch is not shadow or the
// table lacks one of its two rows. Moved unchanged out of
// server/prod_batch_shadow.go (decoupling item 5);
// server's TestProdShadowWiringCannotAct scans this file with that one.
func NewProdShadow(table Table, hands ProdShadowHands) *ProdShadowLoop {
	mode := ProdShadowMode()
	if mode != ProdShadow && mode != ProdOn {
		return nil
	}
	prod, source := ProdRows(table)
	if prod == nil || source == nil {
		slog.Error("prod batch shadow: the table lacks its rows; nothing is recorded", "prod", ProdRowName, "source", ProdSourceName)
		return nil
	}
	soak := Seconds(os.Getenv(ProdSoakEnv), DefaultProdSoak)
	gap := Seconds(os.Getenv(ProdGapEnv), DefaultProdGap)
	reads := hands.Reads()
	reads.Zone = hands.Zone
	reads.HoursFrom, reads.HoursTo = ProdHours(os.Getenv(ProdHoursEnv))
	repo := ProdRepo(prod)
	slog.Info("prod batch shadow: on", "mode", mode, "deliverable", prod.Name, "candidate_from", source.Name, "soak", soak.String(), "gap", gap.String(),
		"hours_from", reads.HoursFrom, "hours_to", reads.HoursTo)
	var release func(ctx context.Context, revision string, params map[string]string) (string, string, error)
	if mode == ProdOn && hands.Release != nil {
		pipeline := prod.Pipeline
		release = func(ctx context.Context, revision string, params map[string]string) (string, string, error) {
			return hands.Release(ctx, pipeline, revision, params)
		}
	}
	return &ProdShadowLoop{
		Release: release,
		Mode:    mode,
		// Five minutes. Each judgement asks the release records what STG and
		// what PROD run, and each of those answers lists every run of the
		// pipelines' namespace a hundred a page: on 2026-11-17, with 2,058 runs
		// kept, one judgement a minute was 327 LISTs in ten minutes under
		// head-release's name (the communication line's count). The records
		// move to a table next; until then the shadow asks less often, and a
		// decision that is five minutes late is still a shadow's decision.
		Every: 5 * time.Minute,
		Gather: func(ctx context.Context) ProdFacts {
			return reads.Gather(kubemetrics.WithSite(ctx, "prod-shadow"), *prod, *source, soak, gap)
		},
		Batch: func(ctx context.Context, from, to string) ProdBatch {
			return hands.Batch(ctx, repo, from, to)
		},
		Record: func(result, detail string) {
			slog.Info("prod batch shadow", "deliverable", prod.Name, "result", result, "record", detail)
			hands.Audit("deliverable/"+prod.Name, result, detail)
		},
	}
}
