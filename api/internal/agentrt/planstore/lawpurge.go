package planstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Physical deletion of repealed laws (legislation: a repealed law does not
// linger; after seven days, or at once when the Owner asks, it may be deleted
// physically, LAW-1). A law repealed seven days ago is deleted by the laws pass
// (PurgeRepealedLaws); one the Owner wants gone at once is deleted by an
// approved law_amend card with op purge. The database does the deletion and
// its checks (eventlog.PurgeRepealedLaw, migration 0047).
//
// What stays is a tombstone on law-purged:<n> (law_purged): the number and
// the approval ids its records carried, none of its text. Without it an
// executed card would be written into the register again on the next pass,
// and the number would be given to a new law.

// LawPurgeAfter is how long a repealed law stays before it is deleted.
const LawPurgeAfter = 7 * 24 * time.Hour

// LawPurgedStream is the tombstone's stream of law n.
func LawPurgedStream(n string) string { return "law-purged:" + n }

var errLawNotPurged = errors.New("law not purged")

// purgeLaw deletes law n's records and writes its tombstone, in tx. False
// when the database refused (not repealed, or too recent and not forced).
func purgeLaw(ctx context.Context, tx pgx.Tx, n, reason string, cardID string, forced bool) (bool, error) {
	law, have, err := lastLaw(ctx, tx, n)
	if err != nil || !have || law.Status != RepealedStatus {
		return false, err
	}
	deleted, ids, err := eventlog.PurgeRepealedLaw(ctx, tx, LawStream(n), forced)
	if err != nil || deleted == 0 {
		return false, err
	}
	if cardID != "" && !slices.Contains(ids, cardID) {
		ids = append(ids, cardID)
	}
	body := map[string]any{"law": n, "events": deleted, "reason": reason, "repealed_at": law.At.UTC().Format(time.RFC3339)}
	if law.Code != "" {
		body["code"] = law.Code
	}
	if len(ids) > 0 {
		list := make([]any, len(ids))
		for i, id := range ids {
			list[i] = id
		}
		body["approval_ids"] = list
	}
	_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: LawPurgedStream(n), Type: "law_purged", Actor: "controller", Body: body})
	return err == nil, err
}

// PurgeRepealedLaws deletes every law repealed at least LawPurgeAfter ago and
// returns the numbers it deleted. Each law goes in its own transaction.
func PurgeRepealedLaws(ctx context.Context, db eventlog.DB, now time.Time) ([]string, error) {
	laws, err := CurrentLaws(ctx, db)
	if err != nil {
		return nil, err
	}
	var gone []string
	var errs []error
	for _, l := range laws {
		if l.Status != RepealedStatus || now.Sub(l.At) < LawPurgeAfter {
			continue
		}
		err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			if err := lockRegister(ctx, tx); err != nil {
				return err
			}
			ok, err := purgeLaw(ctx, tx, l.Law, "废止满七天", "", false)
			if err == nil && !ok {
				return errLawNotPurged
			}
			return err
		})
		switch {
		case err == nil:
			gone = append(gone, l.Law)
		case !errors.Is(err, errLawNotPurged):
			errs = append(errs, fmt.Errorf("purge law %s: %w", l.Law, err))
		}
	}
	return gone, errors.Join(errs...)
}

// purgeByCard deletes the laws an approved purge card names, once (by its
// approval id). A law the register no longer holds, or holds unrepealed, is
// skipped: the card was checked at creation, and what changed since is not
// deleted on its word.
func purgeByCard(ctx context.Context, tx pgx.Tx, c LawCard) error {
	if done, err := eventlog.HasBody(ctx, tx, "law_purged", "", map[string]any{"approval_ids": []any{c.ApprovalID}}); err != nil || done {
		return err
	}
	for _, rp := range c.Repeals {
		if _, err := purgeLaw(ctx, tx, rp.Law, "law_amend "+c.ApprovalID+"："+rp.Reason, c.ApprovalID, true); err != nil {
			return err
		}
	}
	return nil
}

// purgedLaws are the tombstones: the numbers of the laws deleted, and every
// approval id their records carried.
func purgedLaws(ctx context.Context, tx pgx.Tx) (numbers []int, approvals map[string]bool, err error) {
	rows, err := eventlog.LatestOfStreams(ctx, tx, "law_purged", "law-purged:", time.Time{})
	if err != nil {
		return nil, nil, err
	}
	approvals = map[string]bool{}
	for _, r := range rows {
		n, _ := r.Body["law"].(string)
		numbers = append(numbers, lawNumber(n))
		ids, _ := r.Body["approval_ids"].([]any)
		for _, id := range ids {
			if s, ok := id.(string); ok {
				approvals[s] = true
			}
		}
	}
	return numbers, approvals, nil
}

// PurgeQueue is what waits to be deleted, as numbers and times only: a
// repealed law is not in the register's view (LAW-1), but LAW-1
// s5 measures that none outlives LawPurgeAfter, so the laws route says how
// many wait, how many are overdue and when the oldest was repealed.
type PurgeQueue struct {
	Waiting int `json:"waiting"`
	// Overdue is the repealed laws older than LawPurgeAfter still kept: the
	// daily pass deletes them, so anything but 0 for a day is a fault.
	Overdue          int        `json:"overdue"`
	OldestRepealedAt *time.Time `json:"oldest_repealed_at,omitempty"`
	NextPurgeAt      *time.Time `json:"next_purge_at,omitempty"`
}

// LawPurgeQueue reads the purge queue at now.
func LawPurgeQueue(ctx context.Context, db eventlog.DB, now time.Time) (PurgeQueue, error) {
	var q PurgeQueue
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		laws, err := currentLaws(ctx, tx)
		if err != nil {
			return err
		}
		for _, l := range laws {
			if l.Status != RepealedStatus {
				continue
			}
			q.Waiting++
			if now.Sub(l.At) >= LawPurgeAfter {
				q.Overdue++
			}
			if q.OldestRepealedAt == nil || l.At.Before(*q.OldestRepealedAt) {
				at := l.At
				q.OldestRepealedAt = &at
				next := at.Add(LawPurgeAfter)
				q.NextPurgeAt = &next
			}
		}
		return nil
	})
	return q, err
}
