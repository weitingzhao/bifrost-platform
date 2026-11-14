package store

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Vendor pools and the budget ledger (design v5 §5.1.2 ⑥; W-31 2.4 slice ①,
// Owner 2026-10-18 「额度自动加手填」「账号写进池名」).
//
// A pool is one quota window of one vendor account, named
// tool:account:window (codex:owner:weekly): the account is in the name and has
// no column of its own. What a pool knows comes from three sources
// (measure_source): a reading the vendor's own output gives (reading: codex
// writes its rate limits into its session file), the refusal an executor exited
// with (exit_text), and a reading the Owner types in from the vendor's usage
// page (owner). A pool the vendor refused cools down until the reset it named,
// or for unknownResetCooldown when it named none; a claim offers no tool whose
// account has a pool cooling down. An attempt's usage goes to the ledger when
// it is computed: its wall time as the platform measured it, and the tokens and
// cost its vendor reported.

var poolRE = regexp.MustCompile(`^([a-z][a-z0-9-]{0,31}):([a-z0-9][a-z0-9-]{0,31}):(short|weekly|monthly)$`)

// DefaultAccount is the account of a tool whose host names none: today every
// vendor login is the Owner's own.
const DefaultAccount = "owner"

// WindowLength is how long each kind of window lasts; the pace of a pool is
// its used fraction less the fraction of its window already gone.
var WindowLength = map[string]time.Duration{"short": 5 * time.Hour, "weekly": 7 * 24 * time.Hour, "monthly": 30 * 24 * time.Hour}

// PoolReading is what an executor's output, or the Owner, says about one pool.
type PoolReading struct {
	Pool         string     `json:"pool"`
	UsedFraction *float64   `json:"used_fraction,omitempty"`
	ResetsAt     *time.Time `json:"resets_at,omitempty"`
	Limited      bool       `json:"limited,omitempty"` // the vendor refused work for this window
	Text         string     `json:"text,omitempty"`    // the refusal, on one line
}

// Usage is what an attempt used, as its vendor reported it. Tokens leave out
// cache reads; USD is the vendor's own estimate (claude's total_cost_usd).
type Usage struct {
	Pool   string   `json:"pool,omitempty"` // the pool charged
	Tokens *int64   `json:"tokens,omitempty"`
	USD    *float64 `json:"usd,omitempty"`
}

// ParsePool splits a pool id into its tool, account and window.
func ParsePool(id string) (tool, account, window string, ok bool) {
	m := poolRE.FindStringSubmatch(id)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// toolOf is the tool part of an executor's actor id: exec:codex is codex.
func toolOf(executor string) string { return strings.TrimPrefix(executor, "exec:") }

// poolLock is the lock of each pool, keyed by its name: every write of a
// pool's row or stream takes the locks of all the pools it touches first, in
// name order, so a computed and the Owner's reading never wait on each other
// in turn (review 274 B5).
const poolLock = 742110

func lockPools(ctx context.Context, tx pgx.Tx, ids ...string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for i, id := range sorted {
		if id == "" || i > 0 && id == sorted[i-1] {
			continue
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, poolLock, id); err != nil {
			return err
		}
	}
	return nil
}

// PoolView is a pool as the quota screen shows it.
type PoolView struct {
	ID            string             `json:"vendor_pool_id"`
	Tool          string             `json:"tool"`
	Account       string             `json:"account"`
	Window        string             `json:"window"`
	UsedFraction  *float64           `json:"used_fraction"`
	ResetsAt      *time.Time         `json:"resets_at"`
	MeasuredAt    *time.Time         `json:"measured_at"`
	Source        string             `json:"measure_source"`
	CooldownUntil *time.Time         `json:"cooldown_until"`
	Cooling       bool               `json:"cooling"`
	Pace          *float64           `json:"pace"` // used fraction less the fraction of the window gone
	Used          map[string]float64 `json:"used"` // the ledger since the window began, by unit
	Since         time.Time          `json:"used_since"`
}

func listPools(ctx context.Context, tx pgx.Tx) ([]PoolView, error) {
	var out []PoolView
	err := func() error {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT vendor_pool_id, used_fraction::float8, resets_at, measured_at,
		  COALESCE(measure_source, ''), cooldown_until FROM agentrt.vendor_pool ORDER BY vendor_pool_id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v PoolView
			if err = rows.Scan(&v.ID, &v.UsedFraction, &v.ResetsAt, &v.MeasuredAt, &v.Source, &v.CooldownUntil); err != nil {
				rows.Close()
				return err
			}
			var ok bool
			if v.Tool, v.Account, v.Window, ok = ParsePool(v.ID); !ok {
				continue
			}
			v.Cooling = v.CooldownUntil != nil && v.CooldownUntil.After(now)
			length := WindowLength[v.Window]
			v.Since = now.Add(-length)
			if v.ResetsAt != nil && v.ResetsAt.After(now) && v.ResetsAt.Sub(now) <= length {
				v.Since = v.ResetsAt.Add(-length)
				if v.UsedFraction != nil {
					gone := 1 - float64(v.ResetsAt.Sub(now))/float64(length)
					pace := *v.UsedFraction - gone
					v.Pace = &pace
				}
			}
			out = append(out, v)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for i := range out {
			out[i].Used = map[string]float64{}
			r, err := tx.Query(ctx, `SELECT unit, sum(amount)::float8 FROM agentrt.budget_ledger
			  WHERE vendor_pool_id = $1 AND at >= $2 GROUP BY unit`, out[i].ID, out[i].Since)
			if err != nil {
				return err
			}
			for r.Next() {
				var unit string
				var sum float64
				if err = r.Scan(&unit, &sum); err != nil {
					r.Close()
					return err
				}
				out[i].Used[unit] = sum
			}
			r.Close()
			if err = r.Err(); err != nil {
				return err
			}
		}
		return nil
	}()
	return out, err
}
