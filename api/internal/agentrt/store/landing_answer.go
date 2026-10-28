package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"time"
)

// ErrLandingMissionClosed: a retry answered after its mission closed. The
// mission closed first (Close holds its row while it decides), so the retry
// could never be claimed; it is refused rather than recorded as if it could
// (review 645 R1). Whether such a mission reopens is S3's policy.
var ErrLandingMissionClosed = errors.New("the root's mission is no longer active: the retry cannot be claimed")

// ApplyLandingAnswer is called only after the controller reads the bound
// question. It takes the claim's locks in the claim's order (task, then its
// mission, then the repository stream), so it is ordered against a close as a
// claim is: the close decides first and a retry is refused
// (ErrLandingMissionClosed), or the retry commits first and the close reads it
// (landingHolds). The stream serializes it against claims and failures.
func ApplyLandingAnswer(ctx context.Context, tx pgx.Tx, root, after, choice, reason, by string, answeredAt time.Time) error {
	var mission, missionState string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, root).Scan(&mission); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission).Scan(&missionState); err != nil {
		return err
	}
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
		if missionState != "active" {
			return ErrLandingMissionClosed
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
