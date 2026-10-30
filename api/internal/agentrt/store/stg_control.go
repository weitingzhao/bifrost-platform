package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// StgControlRule reads the signed rule without creating a stream. A disabled
// controller can call this supervisor read without producing any B5 records.
func StgControlRule(ctx context.Context, db DB, tenant string) (*rules.Rule, error) {
	var out *rules.Rule
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := effectiveRule(ctx, tx, "landing:"+tenant, DecisionAt(), true)
		if r != nil {
			out = &r.Rule
		}
		return err
	})
	return out, err
}

// CommitStgControl is a compare-and-append of one observed decision. All
// external reads precede this transaction; a changed stream causes a reread,
// never a retry of network effects inside InTx. none is rechecked while the
// rule lock is held, before even creating an empty stream.
func CommitStgControl(ctx context.Context, db DB, tenant string, live func() bool, expected int64, records []StgRecord) (bool, error) {
	written := false
	if len(records) == 0 {
		return false, nil
	}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		written = false
		enabled, err := stgControlEnabled(ctx, tx, tenant, live)
		if err != nil || !enabled {
			return err
		}
		if err = lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil || s.LastSeq != expected {
			return err
		}
		for _, r := range records {
			if _, err = appendStg(ctx, tx, r); err != nil {
				return err
			}
		}
		written = true
		return nil
	})
	return written && err == nil, err
}

func stgControlEnabled(ctx context.Context, tx pgx.Tx, tenant string, live func() bool) (bool, error) {
	if live == nil || !live() {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1,hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
		return false, err
	}
	r, err := effectiveRule(ctx, tx, "landing:"+tenant, DecisionAt(), true)
	if err != nil || r == nil || r.Landing == nil {
		return false, err
	}
	repo, ok := r.Landing.Repo("bifrost-platform")
	return ok && repo.STGRelease == "bifrost-deliver-platform" && live(), nil
}

// OpenStgBatch records the first enablement fence and collects the pending
// results. Admission is deliberately weaker than release authorization: the
// complete base..P range, binding locks and spend remain B5-c's final gate.
func OpenStgBatch(ctx context.Context, db DB, tenant string, live func() bool) (bool, error) {
	changed := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		changed = false
		enabled, err := stgControlEnabled(ctx, tx, tenant, live)
		if err != nil || !enabled {
			return err
		}
		if err = lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		current, err := CurrentRule(ctx, tx, "landing:"+tenant, DecisionAt())
		if err != nil || current == nil || !stgRuleEnabled(&current.Rule, DecisionAt()) {
			return err
		}
		if s.Legacy.STGEnabledAt.IsZero() {
			r, err := CurrentRule(ctx, tx, "landing:"+tenant, DecisionAt())
			if err != nil {
				return err
			}
			_, err = Append(ctx, tx, Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "STG release enabled", "landing": map[string]any{"v": landing.Version, "kind": "stg_enabled", "op": "stg-enabled:" + tenant, "repo": "bifrost-platform", "rule_version": r.Rule.Version}}})
			changed = err == nil
			return err
		}
		for _, b := range s.Batches {
			if b.Phase != "CLOSED" {
				return nil
			}
		}
		for _, b := range s.Blocks {
			if b.IsOpen() {
				return nil
			}
		}
		at := DecisionAt()
		pending := map[string]any{}
		members := []StgMemberKey{}
		active := false
		for op, o := range s.Legacy.Operations {
			active = active || o.Active()
			if !o.EverLanded || o.DryRun || o.Reverted || o.Phase != "landed" || s.Legacy.LatestOperation(o.Task) != op {
				continue
			}
			m := StgMemberKey{o.Task, op, o.R}
			old := s.Members[m]
			if !s.available(m, s.generation(m.R)) || s.covered(m) || old.To != "" && old.To != "released" {
				continue
			}
			var newest StgEvent
			for key, e := range s.Validity {
				if key.Member == m && e.Seq > newest.Seq {
					newest = e
				}
			}
			if newest.Record.String("validity") != "effective" || newest.Record["has_r"] != true {
				continue
			}
			var mission, memberTenant string
			if err = tx.QueryRow(ctx, `SELECT m.state,m.tenant FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) WHERE t.task_id=$1`, m.Root).Scan(&mission, &memberTenant); err != nil {
				return err
			}
			if memberTenant != tenant {
				continue
			}
			landed := time.Time{}
			for _, e := range s.Records {
				if e.Record.String("kind") == "landed" && e.Record.String("op") == op {
					landed = e.At
				}
			}
			if landed.IsZero() || !landed.After(s.Legacy.STGEnabledAt) && mission != "active" {
				continue
			}
			first := old.FirstPendingAt
			if first.IsZero() {
				first = landed
				if first.Before(s.Legacy.STGEnabledAt) {
					first = s.Legacy.STGEnabledAt
				}
			}
			members = append(members, m)
			pending[m.R] = first.UTC().Format(time.RFC3339Nano)
		}
		if len(members) == 0 {
			return nil
		}
		oldest := at
		for _, v := range pending {
			first, _ := time.Parse(time.RFC3339Nano, v.(string))
			if first.Before(oldest) {
				oldest = first
			}
		}
		if active && at.Sub(oldest) < 20*time.Minute {
			return nil
		}
		sortStgMembers(members)
		values := stgMemberValues(members)
		for i, m := range members {
			values[i].(map[string]any)["gen"] = s.generation(m.R)
		}
		_, err = appendStg(ctx, tx, StgRecord{"v": "1", "kind": "opened", "scope": "batch", "batch": fmt.Sprintf("stg:bifrost-platform:%d", s.LastBatch+1), "members": values, "first_pending_at": pending})
		changed = err == nil
		return err
	})
	return changed && err == nil, err
}
