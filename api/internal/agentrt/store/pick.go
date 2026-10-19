package store

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Picking at the claim (design v5 §5.1.2 「分流顺序」 ⑥ ⑦; W-31 2.4 slice ②).
// The route is the candidates that may take the task (①–⑤), in the order of
// their standing; a claim takes the one to use now among those this host
// runs. Quota changes by the minute, so it is read here and not in the route
// (which would be decided again on every reading): a tool whose pool is
// cooling down offers nothing (claim.go), and one whose pool is used to the
// routing rule's quota_stop_percent is passed by; the rest go active before
// probation, then the lowest pace first (the used fraction less the fraction
// of the window gone; a pool not read counts as on pace, 0), then the one
// whose executor took work longest ago (rotationWindow), then the route's
// order. What was
// picked, and why, goes into the attempt_leased event.

type pick struct {
	tool      Tool
	jc        jobCandidate
	pos, tier int
	pace      float64
	read      bool
	lastLease int64
}

// toolQuota is, for each tool of the Owner's account, the fullest of its
// pools still read for the window they are in: the used fraction and the
// pace. A reading counts until its reset, or, when it gave none, for a
// window's length after it was taken. The pace needs the fraction of the
// window gone: without a reset, or with one further off than the window's
// usual length (a month of 31 days), the pool counts as on pace, and its used
// fraction still meets the quota stop (review 286 B5).
type toolQuota struct {
	used, pace float64
	read       bool
}

func quotaByTool(ctx context.Context, tx pgx.Tx) (map[string]toolQuota, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT vendor_pool_id, used_fraction::float8, resets_at, measured_at FROM agentrt.vendor_pool
	  WHERE used_fraction IS NOT NULL ORDER BY vendor_pool_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]toolQuota{}
	for rows.Next() {
		var id string
		var used float64
		var resets, measured *time.Time
		if err = rows.Scan(&id, &used, &resets, &measured); err != nil {
			return nil, err
		}
		tool, account, window, ok := ParsePool(id)
		length := WindowLength[window]
		if !ok || account != DefaultAccount {
			continue
		}
		pace := 0.0
		switch {
		case resets != nil:
			left := resets.Sub(now)
			if left <= 0 {
				continue // its window has come round since
			}
			if left <= length {
				pace = used - (1 - float64(left)/float64(length))
			}
		case measured == nil || now.Sub(*measured) > length:
			continue
		}
		q := out[tool]
		if !q.read {
			q = toolQuota{used: used, pace: pace, read: true}
		} else {
			q.used, q.pace = max(q.used, used), max(q.pace, pace)
		}
		out[tool] = q
	}
	return out, rows.Err()
}

// quotaStop is the fraction of a window at which the tenant's routing rule
// stops a pool taking work; 1 (never) without the rule.
func quotaStop(ctx context.Context, tx pgx.Tx, tenant string) (float64, error) {
	r, err := inForce(ctx, tx, "routing:"+tenant)
	if err != nil || r == nil || r.Routing == nil {
		return 1, err
	}
	return float64(r.Routing.QuotaStopPercent) / 100, nil
}

// rotationWindow bounds what rotation reads: an executor that took no work in
// it took work longer ago than any that did.
const rotationWindow = 7 * 24 * time.Hour

// lastLeases is, for each executor that took work in the rotation window, the
// id of its latest attempt_leased event (ids grow): read by the event type and
// time index, so a claim reads the leases of a week, not all of history
// (review 286 B6).
func lastLeases(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT body_json->>'executor', max(event_id) FROM agentrt.event
	  WHERE event_type = 'attempt_leased' AND at >= clock_timestamp() - make_interval(secs => $1) GROUP BY 1`, rotationWindow.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var e string
		var n int64
		if err = rows.Scan(&e, &n); err != nil {
			return nil, err
		}
		out[e] = n
	}
	return out, rows.Err()
}

// pickState is what choose reads once per claim: the pools, each executor's
// last lease and each tenant's quota stop; a claim walks many candidates and
// picks again for the one it takes, and none of these change under it.
type pickState struct {
	loaded bool
	quota  map[string]toolQuota
	leases map[string]int64
	stops  map[string]float64
}

func (st *pickState) load(ctx context.Context, tx pgx.Tx) error {
	if st.loaded {
		return nil
	}
	var err error
	if st.quota, err = quotaByTool(ctx, tx); err != nil {
		return err
	}
	if st.leases, err = lastLeases(ctx, tx); err != nil {
		return err
	}
	st.stops, st.loaded = map[string]float64{}, true
	return nil
}

func (st *pickState) stop(ctx context.Context, tx pgx.Tx, tenant string) (float64, error) {
	if v, ok := st.stops[tenant]; ok {
		return v, nil
	}
	v, err := quotaStop(ctx, tx, tenant)
	if err == nil {
		st.stops[tenant] = v
	}
	return v, err
}

// choose picks among the options that the route and the hard checks allow:
// it returns the pick and what the attempt_leased event records of it.
func (st *pickState) choose(ctx context.Context, tx pgx.Tx, opts []pick, tenant string) (*pick, map[string]any, error) {
	if len(opts) == 0 {
		return nil, nil, nil
	}
	if err := st.load(ctx, tx); err != nil {
		return nil, nil, err
	}
	stop, err := st.stop(ctx, tx, tenant)
	if err != nil {
		return nil, nil, err
	}
	var open []pick
	passed := []any{}
	for _, o := range opts {
		q := st.quota[toolOf(o.tool.Executor)]
		o.pace, o.read, o.lastLease = q.pace, q.read, st.leases[o.tool.Executor]
		if q.read && q.used >= stop {
			passed = append(passed, o.jc.id)
			continue
		}
		open = append(open, o)
	}
	if len(open) == 0 {
		return nil, nil, nil
	}
	sort.SliceStable(open, func(i, j int) bool {
		a, b := open[i], open[j]
		switch {
		case a.tier != b.tier:
			return a.tier < b.tier
		case a.pace != b.pace:
			return a.pace < b.pace
		case a.lastLease != b.lastLease:
			return a.lastLease < b.lastLease
		}
		return a.pos < b.pos
	})
	best := open[0]
	by := "route"
	if len(open) > 1 {
		second := open[1]
		switch {
		case best.tier != second.tier:
			by = "standing"
		case best.pace != second.pace:
			by = "pace"
		case best.lastLease != second.lastLease:
			by = "rotation"
		}
	}
	why := map[string]any{"candidate": best.jc.id, "by": by, "options": int64(len(open)), "passed_by_quota": passed}
	if best.read {
		why["pace_permille"] = int64(math.Round(best.pace * 1000))
	}
	return &best, why, nil
}
