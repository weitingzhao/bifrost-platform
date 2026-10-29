package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// MissionLandingStatus is a mission's landing status for the Console (CUT
// §11.8 v3 item 9): each live-scope root's class and reason, the card that
// stands for it (if any), the rule as read now and the closing round still
// open (nil once its time is over). A read: no lock, no write.
type MissionLandingStatus struct {
	InScope bool
	Roots   []LandingRootStatus
	Rule    string
	Round   *closeRound
	// Closed is the close as decided then (landing_close), apart from the
	// roots as read now (review 672 N-01). nil while not closed so.
	Closed *LandingClose
}

// LandingRootStatus is one root in MissionLandingStatus.
type LandingRootStatus struct {
	LandingRootClass
	CardKind, QuestionID string
	// CardAlert names a card of this root that ended without its answer
	// being known (unconfirmed, lost): the Owner may have answered a card
	// the runtime could not read back (review 670 R-07). "" for none.
	CardAlert string
	// A parked root (review 672 R-07): since its park, its retry, the
	// status-only trigger it shows (ci_stuck, parked_long) and when a park
	// becomes parked_long. Zero / "" otherwise.
	ParkedSince, RetryAt, RemindAt time.Time
	Shown                          string
	// The Owner's answer holds the root (review 675 R-07): wait or remind
	// later on its latest answered card, until HeldUntil, on HeldQuestion.
	HeldUntil    time.Time
	HeldQuestion string
	// CardPending: a card for the root is prepared and not yet asked (the
	// card budget or quota holds it).
	CardPending bool
	Waiting     *LandingWaitStatus
	Floor       map[string]any
}

// ReadMissionLandingStatus reads a mission's landing status.
func ReadMissionLandingStatus(ctx context.Context, db DB, mission string) (MissionLandingStatus, error) {
	var st MissionLandingStatus
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		round, err := readCloseRound(ctx, tx, mission)
		if err != nil {
			return err
		}
		closed, err := readLandingClose(ctx, tx, mission)
		if err != nil {
			return err
		}
		st.Closed = closed
		at := DecisionAt()
		inScope, roots, rule, err := classifyLandingRoots(ctx, tx, landingFolds{}, mission, round, at)
		if err != nil || !inScope {
			return err
		}
		if round != nil && !at.Before(round.Until) {
			round = nil
		}
		st = MissionLandingStatus{InScope: true, Rule: rule, Round: round, Closed: closed}
		activeRepos := liveRootRepos(roots)
		for _, repo := range landing.LandingRepoNames() {
			if !activeRepos[repo] {
				continue
			}
			cards, err := LandingCards(ctx, tx, CardsStream(repo))
			if err != nil {
				return err
			}
			triggers, err := LandingCardTriggers(ctx, tx, repo, at)
			if err != nil {
				return err
			}
			state, err := LandingState(ctx, tx, "landing:"+repo)
			if err != nil {
				return err
			}
			for _, r := range roots {
				if r.Repo != repo {
					continue
				}
				rs := LandingRootStatus{LandingRootClass: r}
				if landing.LandingRepos[repo].GrantSupported {
					rs.Waiting, err = readLandingWaitStatus(ctx, tx, state, r.Op, at)
					if err != nil {
						return err
					}
				}
				if r.Reason == "failed:floor" {
					err := tx.QueryRow(ctx, `SELECT body_json->'landing_floor' FROM agentrt.event WHERE event_stream_id=$1
                      AND body_json->'landing'->>'op'=$2 AND body_json->'landing'->>'kind'='failed'
                      AND body_json ? 'landing_floor' ORDER BY seq DESC LIMIT 1`, "landing:"+repo, r.Op).Scan(&rs.Floor)
					if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
				}
				var latest, answered CardRecord
				for _, c := range cards {
					if c.Root == r.Root && c.Op == r.Op && !c.Ended() && c.N >= latest.N {
						latest = c
					}
					if c.Root == r.Root && c.Op == r.Op && c.QuestionID != "" && c.N >= answered.N {
						answered = c
					}
					if c.Root == r.Root && c.Op == r.Op && !c.Bound && !c.Ended() {
						rs.CardPending = true
					}
					if c.Root == r.Root && c.Op == r.Op && c.Closed && (c.ClosedWhy == "unconfirmed" || c.ClosedWhy == "lost") {
						rs.CardAlert = c.Prep + " " + c.ClosedWhy
					}
				}
				rs.CardKind, rs.QuestionID = latest.Kind, latest.QuestionID
				if answered.QuestionID != "" {
					o, _, found, err := storedCardRecord(ctx, tx, answered, answered.QuestionID)
					if err != nil {
						return err
					}
					if found && o.outcome == "held" && at.Before(o.remindAt) {
						rs.HeldUntil, rs.HeldQuestion = o.remindAt, answered.QuestionID
					}
				}
				if strings.HasPrefix(r.Reason, "parked:") {
					var retry *string
					err := tx.QueryRow(ctx, `SELECT at, body_json->'landing'->>'retry_at' FROM agentrt.event WHERE event_stream_id = $2
				  AND event_type = 'note' AND body_json->'landing'->>'kind' = 'parked' AND body_json->'landing'->>'op' = $1 ORDER BY seq DESC LIMIT 1`, r.Op, "landing:"+repo).Scan(&rs.ParkedSince, &retry)
					if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
					if retry != nil {
						rs.RetryAt, _ = time.Parse(time.RFC3339Nano, *retry)
					}
					if why := strings.TrimPrefix(r.Reason, "parked:"); why != "ci_stuck" && why != "rule" && !rs.ParkedSince.IsZero() {
						rs.RemindAt = rs.ParkedSince.Add(ParkedLongAfter)
					}
					for _, tr := range triggers {
						if tr.Op == r.Op && slices.Contains(statusOnlyKinds, tr.Kind) {
							rs.Shown = tr.Kind
						}
					}
				}
				st.Roots = append(st.Roots, rs)
			}
		}
		return nil
	})
	return st, err
}
