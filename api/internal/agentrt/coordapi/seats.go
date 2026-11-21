package coordapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// SeatStatusWindow is how far back GET /seats looks for a seat's status.
const SeatStatusWindow = 30 * 24 * time.Hour

// HandleSeatStatuses is every seat's latest status message of the last 30 days,
// newest first (GET /seats). A seat writes its status with the existing
// message route (POST /messages, a "status" envelope to coord.StatusBoard);
// one that does not parse is listed with its problem.
func (rt Routes) HandleSeatStatuses(w http.ResponseWriter, r *http.Request) {
	recs, err := coord.SeatStatuses(r.Context(), rt.Host.DB(), time.Now().Add(-SeatStatusWindow))
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	if recs == nil {
		recs = []coord.SeatStatusRecord{}
	}
	rt.compareSeatRecords(r.Context(), recs)
	rt.Host.JSON(w, http.StatusOK, map[string]any{"seats": recs, "board": coord.StatusBoard})
}

// compareSeatRecords reads the seat records beside the board and says where
// they differ (2026-11-18, side by side before the board's read is replaced).
// The answer stays the board's.
func (rt Routes) compareSeatRecords(ctx context.Context, board []coord.SeatStatusRecord) {
	records, err := coord.SeatRecords(ctx, rt.Host.DB(), time.Now().Add(-SeatStatusWindow))
	if err != nil {
		slog.Warn("seat records not read", "error", err)
		return
	}
	if diff := coord.SeatRecordDiff(board, records); len(diff) > 0 {
		slog.Warn("seat records differ from the board", "seats", diff)
		return
	}
	if len(records) > 0 {
		readerseen.Saw("seat-records")
	}
}
