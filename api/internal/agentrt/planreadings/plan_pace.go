package planreadings

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"time"
)

// PlanPace reads a round's pace at now: the steps under root still planned
// or active, and its plan stream's moves into and out of done in the last
// PaceDays local days. A root there is not is pgx.ErrNoRows. spansOf reads
// registered work (see WorkSpans).
func PlanPace(ctx context.Context, db eventlog.DB, root string, now time.Time, spansOf WorkSpans) (planpace.Pace, error) {
	loc, lerr := time.LoadLocation(planpace.BriefZone)
	if lerr != nil {
		return planpace.Pace{}, lerr
	}
	var out planpace.Pace
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = Pace(ctx, tx, root, "plan:"+root, now, loc, spansOf)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return planpace.Pace{}, fmt.Errorf("store: no plan %s: %w", root, err)
	}
	return out, err
}
