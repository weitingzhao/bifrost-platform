package store

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

// unknownResetCooldown: a refusal that names no reset (cursor's "You've hit
// your usage limit", measured 10-18) is believed for this long, and again on
// the next refusal, unless the Owner's reading says otherwise.
const unknownResetCooldown = 6 * time.Hour

// maxReset: a reset further ahead than a monthly window and a little is not
// believed.
const maxReset = 35 * 24 * time.Hour

// maxReadings and maxPoolText bound what one computed may say about pools.
const (
	maxReadings = 4
	maxPoolText = 300
)

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

// checkPoolReport refuses what an executor of this kind may not say: another
// vendor's pools, a fraction outside 0..1, a reset that is not believable.
func checkPoolReport(executor string, u *Usage, rs []PoolReading) error {
	tool := toolOf(executor)
	// Every vendor login on a host is the Owner's today, and a host states no
	// account of its own: a report names the Owner's pools of its own tool
	// alone (review 274 A4).
	mine := func(id string) error {
		t, account, _, ok := ParsePool(id)
		if !ok {
			return invalid("%q is not a pool (tool:account:window)", id)
		}
		if t != tool || account != DefaultAccount {
			return invalid("%s reports on pool %s, not one of its own tool's under the %s account", executor, id, DefaultAccount)
		}
		return nil
	}
	if u != nil {
		if u.Pool != "" {
			if err := mine(u.Pool); err != nil {
				return err
			}
		}
		if u.Tokens != nil && *u.Tokens < 0 || u.USD != nil && (*u.USD < 0 || math.IsNaN(*u.USD) || *u.USD > 1e6) {
			return invalid("usage out of range")
		}
	}
	if len(rs) > maxReadings {
		return invalid("at most %d pool readings", maxReadings)
	}
	for _, r := range rs {
		if err := mine(r.Pool); err != nil {
			return err
		}
		if err := checkReading(r); err != nil {
			return err
		}
	}
	return nil
}

func checkReading(r PoolReading) error {
	if r.UsedFraction != nil && !(*r.UsedFraction >= 0 && *r.UsedFraction <= 1) {
		return invalid("the used fraction of %s is not between 0 and 1", r.Pool)
	}
	if len(r.Text) > maxPoolText || !utf8.ValidString(r.Text) || strings.ContainsRune(r.Text, 0) {
		return invalid("the refusal text of %s is not one short line", r.Pool)
	}
	return nil
}

// cooldownUntil is how long a reading cools its pool: a refusal until the
// reset it named (or unknownResetCooldown), a full window until its reset;
// otherwise not at all (zero).
//
// A reset already past cools nothing: the window has come round (a refusal
// handed over after its reset, review 274 B6). A reset further ahead than
// maxReset is not believed, and the refusal counts as one that named none.
func cooldownUntil(r PoolReading, now time.Time) time.Time {
	full := r.UsedFraction != nil && *r.UsedFraction >= 1
	if !r.Limited && !full {
		return time.Time{}
	}
	switch {
	case r.ResetsAt == nil || r.ResetsAt.Sub(now) > maxReset:
		if r.Limited {
			return now.Add(unknownResetCooldown)
		}
		return time.Time{}
	case !r.ResetsAt.After(now):
		return time.Time{}
	}
	return *r.ResetsAt
}

// chargeAttempt writes what the attempt used to the ledger: its wall time, and
// the tokens and cost its vendor reported, charged to the pool it names (the
// pool row is made on first use).
func chargeAttempt(ctx context.Context, tx pgx.Tx, actor string, a attemptRow, wallS int64, u *Usage) error {
	if u == nil {
		u = &Usage{}
	}
	body := map[string]any{"attempt_id": a.id, "wall_s": wallS}
	if u.Pool != "" {
		body["vendor_pool_id"] = u.Pool
	}
	if u.Tokens != nil {
		body["tokens"] = *u.Tokens
	}
	if u.USD != nil {
		body["usd_micros"] = int64(math.Round(*u.USD * 1e6))
	}
	if err := attemptEvent(ctx, tx, actor, a, "budget_charged", body); err != nil {
		return err
	}
	var pool any
	if u.Pool != "" {
		tool, _, window, _ := ParsePool(u.Pool)
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.vendor_pool (vendor_pool_id, tool, window_kind) VALUES ($1,$2,$3)
		  ON CONFLICT (vendor_pool_id) DO NOTHING`, u.Pool, tool, window); err != nil {
			return err
		}
		pool = u.Pool
	}
	rows := [][3]any{{"wall_s", wallS, true}}
	if u.Tokens != nil {
		rows = append(rows, [3]any{"tokens", *u.Tokens, true})
	}
	if u.USD != nil {
		rows = append(rows, [3]any{"usd", *u.USD, false})
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.budget_ledger (attempt_id, vendor_pool_id, unit, amount, measured)
		  VALUES ($1,$2,$3,$4,$5)`, a.id, pool, r[0], r[1], r[2]); err != nil {
			return err
		}
	}
	return nil
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

// measurePools records readings of pools and cools those they say are spent.
// A cooldown from an executor's output only ever lengthens the one in force;
// the Owner's reading replaces it, and lifts it when it says the pool has
// quota left.
func measurePools(ctx context.Context, tx pgx.Tx, actor, source string, rs []PoolReading) error {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	for _, r := range rs {
		tool, _, window, ok := ParsePool(r.Pool)
		if !ok {
			return invalid("%q is not a pool", r.Pool)
		}
		body := map[string]any{"vendor_pool_id": r.Pool, "source": source}
		var used, resets any
		if r.UsedFraction != nil {
			body["used_permille"] = int64(math.Round(*r.UsedFraction * 1000))
			used = *r.UsedFraction
		}
		if r.ResetsAt != nil {
			t := r.ResetsAt.UTC().Truncate(time.Microsecond)
			body["resets_at"] = t.Format(timeLayout)
			resets = t
		}
		if r.Limited {
			body["limited"] = true
		}
		if r.Text != "" {
			body["text"] = r.Text
		}
		if _, err := Append(ctx, tx, Event{Stream: "pool:" + r.Pool, Type: "pool_measured", Actor: actor, Body: body}); err != nil {
			return err
		}
		var cooling *time.Time
		if err := tx.QueryRow(ctx, `INSERT INTO agentrt.vendor_pool
		  (vendor_pool_id, tool, window_kind, used_fraction, resets_at, measured_at, measure_source)
		  VALUES ($1,$2,$3,$4,$5,$6,$7)
		  ON CONFLICT (vendor_pool_id) DO UPDATE SET
		    used_fraction = COALESCE(EXCLUDED.used_fraction, agentrt.vendor_pool.used_fraction),
		    resets_at = CASE WHEN EXCLUDED.resets_at IS NOT NULL THEN EXCLUDED.resets_at
		      -- a new fraction with no reset is not bound to a reset already past (review 285)
		      WHEN EXCLUDED.used_fraction IS NOT NULL AND agentrt.vendor_pool.resets_at <= EXCLUDED.measured_at THEN NULL
		      ELSE agentrt.vendor_pool.resets_at END,
		    measured_at = EXCLUDED.measured_at, measure_source = EXCLUDED.measure_source,
		    version = agentrt.vendor_pool.version + 1
		  RETURNING cooldown_until`, r.Pool, tool, window, used, resets, now, source).Scan(&cooling); err != nil {
			return err
		}
		until := cooldownUntil(r, now)
		inForce := cooling != nil && cooling.After(now)
		switch {
		case !until.IsZero() && (source == "owner" || !inForce || until.After(*cooling)):
			if err := SetCooldown(ctx, tx, actor, r.Pool, tool, window, until, poolReason(source, r)); err != nil {
				return err
			}
		case until.IsZero() && source == "owner" && inForce && r.UsedFraction != nil && *r.UsedFraction < 1:
			if err := SetCooldown(ctx, tx, actor, r.Pool, tool, window, now, "the Owner's reading says the pool has quota left"); err != nil {
				return err
			}
		}
	}
	return nil
}

func poolReason(source string, r PoolReading) string {
	switch {
	case source == "owner":
		return "the Owner's reading says the pool is spent"
	case r.Text != "":
		return "the vendor refused: " + r.Text
	case r.Limited:
		return "the vendor refused"
	}
	return "the vendor's reading says the window is spent"
}

// RecordOwnerReading records the Owner's reading of a pool from the vendor's
// usage page (Owner 2026-10-18 「额度自动加手填」): how much of the window is
// used and, for a spent one, when it resets. It replaces a cooldown in force,
// and lifts it when the pool has quota left.
func RecordOwnerReading(ctx context.Context, db DB, w Writer, key string, r PoolReading) (map[string]any, error) {
	switch {
	case key == "":
		return nil, invalid("an idempotency key is required")
	case r.UsedFraction == nil:
		return nil, invalid("a reading says how much of the window is used")
	case r.Limited || r.Text != "":
		return nil, invalid("the Owner's reading is a used fraction and a reset, not a refusal")
	case *r.UsedFraction >= 1 && r.ResetsAt == nil:
		return nil, invalid("a spent pool's reading says when it resets")
	case r.ResetsAt != nil && (!r.ResetsAt.After(time.Now()) || time.Until(*r.ResetsAt) > maxReset):
		return nil, invalid("the reset is not ahead and within %d days", int(maxReset.Hours()/24))
	}
	if _, _, _, ok := ParsePool(r.Pool); !ok {
		return nil, invalid("%q is not a pool (tool:account:window)", r.Pool)
	}
	if err := checkReading(r); err != nil {
		return nil, err
	}
	req := map[string]any{"pool": r.Pool, "used_permille": int64(math.Round(*r.UsedFraction * 1000))}
	if r.ResetsAt != nil {
		req["resets_at"] = r.ResetsAt.UTC().Truncate(time.Microsecond).Format(timeLayout)
	}
	return idempotent(ctx, db, w.Actor, "pool_reading", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return nil, err
		}
		if w.Actor != "owner" {
			return nil, invalid("only the Owner reads a pool by hand")
		}
		if err := lockPools(ctx, tx, r.Pool); err != nil {
			return nil, err
		}
		if err := measurePools(ctx, tx, w.Actor, "owner", []PoolReading{r}); err != nil {
			return nil, err
		}
		return map[string]any{"vendor_pool_id": r.Pool, "recorded": true}, nil
	})
}

// coolingAccounts are the tool:account pairs with a pool cooling down now.
func coolingAccounts(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT vendor_pool_id FROM agentrt.vendor_pool WHERE cooldown_until > clock_timestamp()`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range ids {
		if tool, account, _, ok := ParsePool(id); ok {
			out[tool+":"+account] = true
		}
	}
	return out, nil
}

// notCooling are the tools whose account has no pool cooling down.
func notCooling(ctx context.Context, tx pgx.Tx, tools []Tool) ([]Tool, error) {
	cool, err := coolingAccounts(ctx, tx)
	if err != nil || len(cool) == 0 {
		return tools, err
	}
	out := tools[:0:0]
	for _, tl := range tools {
		if !cool[toolOf(tl.Executor)+":"+DefaultAccount] {
			out = append(out, tl)
		}
	}
	return out, nil
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

// ListPools reads every pool with its pace and what the ledger charged it
// since its window began (the last window length when no reset is known).
func ListPools(ctx context.Context, db DB) ([]PoolView, error) {
	var out []PoolView
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = listPools(ctx, tx)
		return err
	})
	return out, err
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
