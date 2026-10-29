package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// readLandingGrants folds the stream's grant records, in stream order. It
// shares LandingState's one ordered read (review 694 R-04).
func readLandingGrants(ctx context.Context, tx pgx.Tx, stream string) (map[string]*grantView, []*grantView, error) {
	r, err := scanLandingStream(ctx, tx, stream)
	return r.byID, r.order, err
}

// scanLandingStream is the one ordered read of a landing stream: its
// protocol records and, on the repository stream, its grant records, in a
// single query (no second round trip on a stream with no grant).
type landingScan struct {
	entries []landing.Entry
	byID    map[string]*grantView
	order   []*grantView
	// ciKeys are the operations' pinned CI keys, from their ci_kick or
	// ci_rerun (review 698 R-05: no separate read to know an operation has
	// none).
	ciKeys map[string]GrantCIKey
}

func scanLandingStream(ctx context.Context, tx pgx.Tx, stream string) (landingScan, error) {
	entries := []landing.Entry{}
	byID := map[string]*grantView{}
	ciKeys := map[string]GrantCIKey{}
	var order []*grantView
	fail := func(err error) (landingScan, error) { return landingScan{}, err }
	rows, err := tx.Query(ctx, `SELECT seq, at, body_json->'landing', body_json->'landing_grant', body_json->'landing_grant_revoked',
	    body_json->'landing_grant_use', body_json->'landing_grant_refused', body_json->'landing_ci_key'
	  FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note'
	  AND (body_json ? 'landing' OR body_json ? 'landing_grant' OR body_json ? 'landing_grant_revoked' OR body_json ? 'landing_grant_refused')
	  ORDER BY seq`, stream)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	parse := func(raw []byte) (any, error) {
		if len(raw) == 0 {
			return nil, nil
		}
		return canon.Parse(raw)
	}
	for rows.Next() {
		var seq int64
		var at time.Time
		var rawLanding, rawGrant, rawRevoked, rawUse, rawRefused, rawKey []byte
		if err := rows.Scan(&seq, &at, &rawLanding, &rawGrant, &rawRevoked, &rawUse, &rawRefused, &rawKey); err != nil {
			return fail(err)
		}
		var rec landing.Record
		if len(rawLanding) > 0 {
			v, err := parse(rawLanding)
			if err != nil {
				return fail(err)
			}
			if rec, err = landing.Parse(v); err != nil {
				return fail(err)
			}
			entries = append(entries, landing.Entry{At: at, Record: rec})
		}
		if stream != landingRepoStream {
			continue
		}
		if x, err := parse(rawKey); err != nil {
			return fail(err)
		} else if x != nil {
			k, err := parseLandingCIKey(x)
			if err != nil {
				return fail(err)
			}
			if _, seen := ciKeys[k.Op]; !seen {
				ciKeys[k.Op] = k.GrantCIKey
			}
		}
		if x, err := parse(rawGrant); err != nil {
			return fail(err)
		} else if x != nil {
			g, err := parseLandingGrant(x)
			if err != nil {
				return fail(err)
			}
			view := &grantView{LandingGrant: g, Seq: seq, At: at}
			view.approved, _ = grantTime(g.ApprovedAt)
			view.granted, _ = grantTime(g.GrantedAt)
			view.exp, _ = grantTime(g.ExpiresAt)
			byID[g.GrantID] = view
			order = append(order, view)
		}
		if x, err := parse(rawRevoked); err != nil {
			return fail(err)
		} else if x != nil {
			r, err := parseLandingGrantRevoked(x)
			if err != nil {
				return fail(err)
			}
			if g := byID[r.GrantID]; g != nil && g.RevokedAt.IsZero() {
				g.RevokedAt, _ = grantTime(r.At)
				g.RevokedBy = r.By
			}
		}
		if x, err := parse(rawUse); err != nil {
			return fail(err)
		} else if x != nil {
			u, err := parseLandingGrantUse(x)
			if err != nil {
				return fail(err)
			}
			if g := byID[u.GrantID]; g != nil && g.ConsumedSeq == 0 {
				g.ConsumedSeq, g.ConsumedAt = seq, at
				g.ConsumedOp = rec.String("op")
			}
		}
		if x, err := parse(rawRefused); err != nil {
			return fail(err)
		} else if x != nil {
			r, err := parseLandingGrantRefused(x)
			if err != nil {
				return fail(err)
			}
			if g := byID[r.GrantID]; g != nil && g.RefusedAt.IsZero() {
				g.RefusedAt, _ = grantTime(r.DecidedAt)
				g.RefusedWhy = r.Reason
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	return landingScan{entries: entries, byID: byID, order: order, ciKeys: ciKeys}, nil
}

// LandingStateCIKeys is LandingState with the operations' pinned CI keys,
// from the same single read (the controller's CI step).
func LandingStateCIKeys(ctx context.Context, tx pgx.Tx, stream string) (landing.State, map[string]GrantCIKey, error) {
	r, err := scanLandingStream(ctx, tx, stream)
	if err != nil {
		return landing.State{}, nil, err
	}
	st, err := landing.FoldWithGrants(r.entries, grantPeriods(r.order))
	return st, r.ciKeys, err
}
