package store

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"time"
)

// ApplyLandingAnswer is called only after the controller reads the bound
// question. The repository stream serializes it against claims and failures.
func ApplyLandingAnswer(ctx context.Context, tx pgx.Tx, root, after, choice, reason, by string, answeredAt time.Time) error {
	if err := lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
		return err
	}
	s, err := LandingState(ctx, tx, "landing:bifrost-platform")
	if err != nil {
		return err
	}
	o, exists := s.Operations[after]
	if !exists || o.Task != root || s.LatestOperation(root) != after {
		return fmt.Errorf("answer does not name latest operation")
	}
	if answeredAt.Before(o.ClaimedAt) {
		return fmt.Errorf("answer predates operation")
	}
	r := map[string]any{"v": landing.Version, "op": after}
	switch choice {
	case "try_again":
		if o.Phase != "failed" || answeredAt.Before(o.LastAt) {
			return fmt.Errorf("retry answer requires the failure it follows")
		}
		if o.Retried {
			return nil
		}
		if reason == "" {
			reason = "try_again"
		}
		r["kind"], r["root"], r["after_op"], r["reason"], r["by"] = "retry", root, after, reason, by
	case "do_not_merge":
		if o.Outcome == "declined" {
			return nil
		}
		r["kind"], r["why"] = "failed", "declined"
	default:
		return nil
	}
	_, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing question: " + choice, "landing": r}})
	return err
}
