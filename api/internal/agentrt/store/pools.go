package store

import (
	"context"
	"regexp"
	"sort"
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
