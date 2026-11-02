package store

import (
	"context"
	"fmt"
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
// routing rule's quota_stop_percent is passed by while that reading is
// recent (quotaStopHolds); the rest go active before
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

// quotaStopHolds is how long a reading at or past the tenant's quota stop
// holds a tool's work back. A pool is read again only from the output of an
// attempt that ran on it, so a reading that holds back every claim is never
// read again before its reset: a wrong or outdated one would hold claude's
// work for up to a week (PROD 11-02: its weekly pool read 99% 45 minutes
// after its reset). Past this age the reading counts as not read (pace 0), a
// claim goes through, and its attempt reads the pool again: a pool truly at
// the stop is then held back again, and a spent one is refused by its vendor
// and cools down (pools.go), which this age does not touch. Three hours lets
// at most a round of attempts every three hours through a pool truly at the
// stop, against a wrong reading holding the work back for days.
const quotaStopHolds = 3 * time.Hour

// poolQuota is a pool of a tool of the Owner's account still read for the
// window it is in: its used fraction and pace, when it was measured and how
// long ago (age), until when a reading at the quota stop holds work back
// (quotaStopHolds after it was measured, or its reset if sooner), and whether
// it still does (holding: never for a reading of no known time). A
// reading counts until its reset, or, when it gave none, for a window's
// length after it was taken. The pace needs the fraction of the window gone:
// without a reset, or with one further off than the window's usual length (a
// month of 31 days), the pool counts as on pace, and its used fraction still
// meets the quota stop (review 286 B5).
type poolQuota struct {
	used, pace      float64
	holding         bool
	measured, holds time.Time
	age             time.Duration
}

// toolQuota is a tool's pools against a quota stop (quotaAt): the fullest
// used fraction and pace of the readings that count, and the fullest one's
// measuring.
type toolQuota struct {
	used, pace      float64
	read            bool
	measured, holds time.Time
	age             time.Duration
}

// quotaAt folds a tool's pools against stop. A reading at or past the stop
// counts only until it holds no longer (quotaStopHolds); after that it counts
// as not read, for the stop and for the pace alike.
func quotaAt(pools []poolQuota, stop float64) toolQuota {
	var q toolQuota
	for _, p := range pools {
		if p.used >= stop && !p.holding {
			continue
		}
		if !q.read || p.used > q.used {
			q.used, q.measured, q.holds, q.age = p.used, p.measured, p.holds, p.age
		}
		if !q.read || p.pace > q.pace {
			q.pace = p.pace
		}
		q.read = true
	}
	return q
}

// measuredWords says when the reading that holds a tool's work back was
// measured, and until when it holds it, so an old reading shows as old.
func (q toolQuota) measuredWords() string {
	return fmt.Sprintf("as measured at %s (%s); the reading holds work back until %s", q.measured.UTC().Format(time.RFC3339),
		agoWords(q.age), q.holds.UTC().Format(time.RFC3339))
}

// quotaByTool reads, for each tool of the Owner's account, its pools still
// read for the window they are in.
func quotaByTool(ctx context.Context, tx pgx.Tx) (map[string][]poolQuota, error) {
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
	out := map[string][]poolQuota{}
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
		p := poolQuota{used: used}
		switch {
		case resets != nil:
			left := resets.Sub(now)
			if left <= 0 {
				continue // its window has come round since
			}
			if left <= length {
				p.pace = used - (1 - float64(left)/float64(length))
			}
		case measured == nil || now.Sub(*measured) > length:
			continue
		}
		if measured != nil {
			p.measured, p.age = *measured, now.Sub(*measured)
			p.holds = measured.Add(quotaStopHolds)
			if resets != nil && resets.Before(p.holds) {
				p.holds = *resets
			}
			p.holding = now.Before(p.holds)
		}
		out[tool] = append(out[tool], p)
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
	// skip is why the last candidate passed over was not leased, for a
	// claim that leases nothing (claimIn).
	skip   string
	loaded bool
	quota  map[string][]poolQuota
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
		q := quotaAt(st.quota[toolOf(o.tool.Executor)], stop)
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
