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

// Physical deletion of repealed laws (legislation, Owner 2026-11-24: a
// repealed law must not linger as poison; "满七天或者我直接要求删..都可以物理
// 删除"). A law repealed seven days ago is deleted by the laws pass
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
