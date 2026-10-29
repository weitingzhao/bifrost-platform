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
	_, byID, order, err := scanLandingStream(ctx, tx, stream)
	return byID, order, err
}

// scanLandingStream is the one ordered read of a landing stream: its
// protocol records and, on the repository stream, its grant records, in a
// single query (no second round trip on a stream with no grant).
func scanLandingStream(ctx context.Context, tx pgx.Tx, stream string) ([]landing.Entry, map[string]*grantView, []*grantView, error) {
	entries := []landing.Entry{}
	byID := map[string]*grantView{}
	var order []*grantView
	rows, err := tx.Query(ctx, `SELECT seq, at, body_json->'landing', body_json->'landing_grant', body_json->'landing_grant_revoked',
	    body_json->'landing_grant_use', body_json->'landing_grant_refused'
	  FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note'
	  AND (body_json ? 'landing' OR body_json ? 'landing_grant' OR body_json ? 'landing_grant_revoked' OR body_json ? 'landing_grant_refused')
	  ORDER BY seq`, stream)
	if err != nil {
		return nil, nil, nil, err
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
		var rawLanding, rawGrant, rawRevoked, rawUse, rawRefused []byte
		if err := rows.Scan(&seq, &at, &rawLanding, &rawGrant, &rawRevoked, &rawUse, &rawRefused); err != nil {
			return nil, nil, nil, err
		}
		var rec landing.Record
		if len(rawLanding) > 0 {
			v, err := parse(rawLanding)
			if err != nil {
				return nil, nil, nil, err
			}
			if rec, err = landing.Parse(v); err != nil {
				return nil, nil, nil, err
			}
			entries = append(entries, landing.Entry{At: at, Record: rec})
		}
		if stream != landingRepoStream {
			continue
		}
		if x, err := parse(rawGrant); err != nil {
			return nil, nil, nil, err
		} else if x != nil {
			g, err := parseLandingGrant(x)
			if err != nil {
				return nil, nil, nil, err
			}
			view := &grantView{LandingGrant: g, Seq: seq, At: at}
			view.approved, _ = grantTime(g.ApprovedAt)
			view.granted, _ = grantTime(g.GrantedAt)
			view.exp, _ = grantTime(g.ExpiresAt)
			byID[g.GrantID] = view
			order = append(order, view)
		}
		if x, err := parse(rawRevoked); err != nil {
			return nil, nil, nil, err
		} else if x != nil {
			r, err := parseLandingGrantRevoked(x)
			if err != nil {
				return nil, nil, nil, err
			}
			if g := byID[r.GrantID]; g != nil && g.RevokedAt.IsZero() {
				g.RevokedAt, _ = grantTime(r.At)
				g.RevokedBy = r.By
			}
		}
		if x, err := parse(rawUse); err != nil {
			return nil, nil, nil, err
		} else if x != nil {
			u, err := parseLandingGrantUse(x)
			if err != nil {
				return nil, nil, nil, err
			}
			if g := byID[u.GrantID]; g != nil && g.ConsumedSeq == 0 {
				g.ConsumedSeq, g.ConsumedAt = seq, at
				g.ConsumedOp = rec.String("op")
			}
		}
		if x, err := parse(rawRefused); err != nil {
			return nil, nil, nil, err
		} else if x != nil {
			r, err := parseLandingGrantRefused(x)
			if err != nil {
				return nil, nil, nil, err
			}
			if g := byID[r.GrantID]; g != nil && g.RefusedAt.IsZero() {
				g.RefusedAt, _ = grantTime(r.DecidedAt)
				g.RefusedWhy = r.Reason
			}
		}
	}
	return entries, byID, order, rows.Err()
}
