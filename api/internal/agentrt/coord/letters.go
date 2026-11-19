package coord

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/observe"
)

// ReadLetters reads the letters and the latest status report of each actor
// since the given time, for the Bus page's picture of the team
// (observe.Handler.Letters). The messages are this package's table; the
// picture is foundation's and reads them through this, not by SQL of its own
// (2026-11-19, OWNER_LAW 28: moved here unchanged from bus/observe).
func ReadLetters(ctx context.Context, db observe.Querier, since time.Time) ([]observe.Letter, map[string]time.Time, error) {
	rows, err := db.Query(ctx, `SELECT from_actor_id, to_actor_id, created_at, acked_at FROM agentrt.message
	  WHERE created_at >= $1 ORDER BY created_at LIMIT 20000`, since)
	if err != nil {
		return nil, nil, fmt.Errorf("observe: letters: %w", err)
	}
	defer rows.Close()
	var letters []observe.Letter
	status := map[string]time.Time{}
	for rows.Next() {
		var l observe.Letter
		if err := rows.Scan(&l.From, &l.To, &l.At, &l.Acked); err != nil {
			return nil, nil, err
		}
		if l.To == observe.StatusBoard {
			if l.At.After(status[l.From]) {
				status[l.From] = l.At
			}
			continue
		}
		letters = append(letters, l)
	}
	return letters, status, rows.Err()
}
