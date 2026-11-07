package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Slot signals (R5 phase 2 step 3b, T5; the Owner 2026-11-06 Q3/Q4): what a
// policy that moves a host's effective slot count reads. agentd's claim
// carries the host's headroom (load1, cpus, mem_avail_bytes,
// mem_total_bytes; optional, with the same unknown-field fallback as the
// disk fields), and SlotSignals puts it with what the platform knows:
// the vendor pools' rate-limit refusals, the leasable queue and the
// host's attempt durations. GET /hosts shows them for every host.
//
// The headroom is recorded on a stream of the host's own (HostHeadroomStream),
// read through its head as the offers are (presence.go): agentrt.host
// takes a write only with a host_registered, host_state_changed or
// host_retired event (0002's event_may_write) and capacity_json is not a
// liveness column, so writing host.capacity_json.headroom needs a migration
// the plan did not reserve a number for (0013 is the rule approval, Q1). A
// claim records its headroom when it moved materially or the last record
// is HeadroomEvery old, not on every poll.

// HostHeadroomStream holds a host's headroom as its claims report it.
func HostHeadroomStream(host string) string { return "headroom:host:" + host }

// HeadroomEvery is how old a host's headroom record may get while it does
// not move materially before a claim records it again.
var HeadroomEvery = 5 * time.Minute

// SlotSignalInterval is the interval signals are counted over when the
// caller names none: refusals and durations since one interval ago, the
// durations' previous interval the one before.
const SlotSignalInterval = time.Hour

// Headroom is what a claim says of the host's load and memory; each field
// is nil when the claim did not carry it. At is when the platform recorded
// it.
type Headroom struct {
	Load1    *float64   `json:"load1,omitempty"`
	CPUs     *int       `json:"cpus,omitempty"`
	MemAvail *int64     `json:"mem_avail_bytes,omitempty"`
	MemTotal *int64     `json:"mem_total_bytes,omitempty"`
	At       *time.Time `json:"at,omitempty"`
}

// Empty says whether the claim carried none of it.
func (h Headroom) Empty() bool {
	return h.Load1 == nil && h.CPUs == nil && h.MemAvail == nil && h.MemTotal == nil
}

// Check refuses what no host can report: a negative or not finite load, no
// cpu, negative memory, more available than in total.
func (h Headroom) Check() error {
	switch {
	case h.Load1 != nil && (math.IsNaN(*h.Load1) || math.IsInf(*h.Load1, 0) || *h.Load1 < 0):
		return invalid("load1 is a load, not negative")
	case h.CPUs != nil && *h.CPUs < 1:
		return invalid("cpus is at least 1")
	case (h.MemAvail == nil) != (h.MemTotal == nil):
		return invalid("mem_avail_bytes and mem_total_bytes go together")
	case h.MemAvail != nil && (*h.MemAvail < 0 || *h.MemTotal <= 0 || *h.MemAvail > *h.MemTotal):
		return invalid("memory available is between 0 and the total, and the total is more than 0")
	}
	return nil
}

// body is the headroom as an event records it: integers only (canon takes
// no float), the load in hundredths.
func (h Headroom) body() map[string]any {
	b := map[string]any{}
	if h.Load1 != nil {
		b["load1_hundredths"] = int64(math.Round(*h.Load1 * 100))
	}
	if h.CPUs != nil {
		b["cpus"] = int64(*h.CPUs)
	}
	if h.MemAvail != nil {
		b["mem_avail_bytes"], b["mem_total_bytes"] = *h.MemAvail, *h.MemTotal
	}
	return b
}

// words says the headroom on one line.
func (h Headroom) words() string {
	var parts []string
	if h.Load1 != nil {
		load := fmt.Sprintf("a one-minute load of %.2f", *h.Load1)
		if h.CPUs != nil {
			load += fmt.Sprintf(" on %d cpus", *h.CPUs)
		}
		parts = append(parts, load)
	} else if h.CPUs != nil {
		parts = append(parts, fmt.Sprintf("%d cpus", *h.CPUs))
	}
	if h.MemAvail != nil {
		parts = append(parts, fmt.Sprintf("%s of %s memory available", gib(*h.MemAvail), gib(*h.MemTotal)))
	}
	return strings.Join(parts, ", ")
}

// moved says whether h differs materially from the record last: a field
// came or went, the cpus or the memory's total changed, the load moved by a
// quarter of a load per cpu (one when the cpus are not known), or the
// available memory by a tenth of the total.
func (h Headroom) moved(last Headroom) bool {
	if (h.Load1 == nil) != (last.Load1 == nil) || (h.CPUs == nil) != (last.CPUs == nil) || (h.MemAvail == nil) != (last.MemAvail == nil) {
		return true
	}
	if h.CPUs != nil && *h.CPUs != *last.CPUs || h.MemTotal != nil && *h.MemTotal != *last.MemTotal {
		return true
	}
	if h.Load1 != nil {
		step := 1.0
		if h.CPUs != nil {
			step = 0.25 * float64(*h.CPUs)
		}
		if math.Abs(*h.Load1-*last.Load1) >= step {
			return true
		}
	}
	return h.MemAvail != nil && math.Abs(float64(*h.MemAvail-*last.MemAvail)) >= float64(*h.MemTotal)/10
}

// headroomOf reads a headroom stream's head (headSQL's document); nil for
// none.
func headroomOf(head []byte) (*Headroom, error) {
	if head == nil {
		return nil, nil
	}
	var d struct {
		At   time.Time `json:"at"`
		Body struct {
			Headroom struct {
				Load  *int64 `json:"load1_hundredths"`
				CPUs  *int   `json:"cpus"`
				Avail *int64 `json:"mem_avail_bytes"`
				Total *int64 `json:"mem_total_bytes"`
			} `json:"headroom"`
		} `json:"body"`
	}
	if err := json.Unmarshal(head, &d); err != nil {
		return nil, err
	}
	r := d.Body.Headroom
	h := &Headroom{CPUs: r.CPUs, MemAvail: r.Avail, MemTotal: r.Total, At: &d.At}
	if r.Load != nil {
		l := float64(*r.Load) / 100
		h.Load1 = &l
	}
	return h, nil
}

var latestHeadroomSQL = `SELECT ` + headSQL("$1::text")

func latestHeadroom(ctx context.Context, tx pgx.Tx, host string) (*Headroom, error) {
	var head []byte
	if err := tx.QueryRow(ctx, latestHeadroomSQL, HostHeadroomStream(host)).Scan(&head); err != nil {
		return nil, err
	}
	return headroomOf(head)
}

// RecordHostHeadroom records the headroom host's claim reports, when it
// moved materially since the last record or that is HeadroomEvery old (or
// there is none): a note with it. It reports whether it recorded one. A
// claim request calls it once, before its polls; the stream is locked before
// its head is read again, so two claims of one host record it once.
func RecordHostHeadroom(ctx context.Context, db DB, host string, h Headroom) (bool, error) {
	if h.Empty() {
		return false, nil
	}
	if err := h.Check(); err != nil {
		return false, err
	}
	same := func(tx pgx.Tx) (bool, error) {
		last, err := latestHeadroom(ctx, tx, host)
		if err != nil || last == nil {
			return false, err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return false, err
		}
		return !h.moved(*last) && now.Sub(*last.At) < HeadroomEvery, nil
	}
	recorded := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		recorded = false
		if s, err := same(tx); err != nil || s {
			return err
		}
		stream := HostHeadroomStream(host)
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		if s, err := same(tx); err != nil || s {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": "Host " + host + " reports " + h.words() + ".", "headroom": h.body()}})
		recorded = err == nil
		return err
	})
	return recorded && err == nil, err
}

// PoolRefusals are the rate-limit refusals of one vendor pool (tool:account,
// every window of it) in the interval: the refusals recorded (pool_measured
// limited: an executor's exit text or the Owner's reading), the attempts
// that ended on one (an exit text recorded with the attempt's computed),
// the cooldowns set (pool_cooldown_set to a time after it was set; a lift
// is not one), the cooldown in force now, and the last refusal or cooldown.
type PoolRefusals struct {
	Pool            string     `json:"vendor_pool"`
	Refusals        int        `json:"refusals"`
	LimitedAttempts int        `json:"limited_attempts"`
	Cooldowns       int        `json:"cooldowns"`
	CoolingUntil    *time.Time `json:"cooling_until"`
	LastAt          *time.Time `json:"last_at"`
}

// Durations are the median wall time from start to computed of the host's
// attempts computed in the interval and in the one before it, and how many
// each counts; a median is nil when there is none.
type Durations struct {
	MedianS         *float64 `json:"median_s"`
	Count           int      `json:"count"`
	PreviousMedianS *float64 `json:"previous_median_s"`
	PreviousCount   int      `json:"previous_count"`
}

// HostSlotSignals are a host's slot signals over the interval from Since to At:
// its latest headroom (nil until a claim reported one), the refusals of each
// vendor pool its offered tools route to (a tool's executor under the
// account every host uses, DefaultAccount), the leasable queue, and its
// attempts' durations.
type HostSlotSignals struct {
	HostID     string         `json:"host_id"`
	Since      time.Time      `json:"since"`
	At         time.Time      `json:"at"`
	IntervalS  int64          `json:"interval_s"`
	Headroom   *Headroom      `json:"headroom"`
	Pools      []PoolRefusals `json:"pools"`
	QueueDepth int            `json:"queue_depth"`
	Durations  Durations      `json:"durations"`
}

// SlotSignals reads host's slot signals since since (zero, or not before
// now: SlotSignalInterval ago).
func SlotSignals(ctx context.Context, db DB, host string, since time.Time) (HostSlotSignals, error) {
	var out HostSlotSignals
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		all, err := slotSignalsIn(ctx, tx, []string{host}, since)
		out = all[host]
		return err
	})
	return out, err
}

// HostsSlotSignals is SlotSignals of every registered host, by host, read
// in tx at one moment (GET /hosts reads it with the hosts' document).
func HostsSlotSignals(ctx context.Context, tx pgx.Tx, since time.Time) (map[string]HostSlotSignals, error) {
	rows, err := tx.Query(ctx, `SELECT host_id FROM agentrt.host ORDER BY host_id`)
	if err != nil {
		return nil, err
	}
	hosts, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	return slotSignalsIn(ctx, tx, hosts, since)
}

// vendorPoolOf is the vendor pool (tool:account) of a pool id, "" for none.
func vendorPoolOf(id string) string {
	tool, account, _, ok := ParsePool(id)
	if !ok {
		return ""
	}
	return tool + ":" + account
}

func slotSignalsIn(ctx context.Context, tx pgx.Tx, hosts []string, since time.Time) (map[string]HostSlotSignals, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	if since.IsZero() || !since.Before(now) {
		since = now.Add(-SlotSignalInterval)
	}
	interval := now.Sub(since)
	pools, err := poolRefusals(ctx, tx, since, now)
	if err != nil {
		return nil, err
	}
	var depth int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  WHERE t.state = 'ready' AND m.state = 'active' AND t.effect_class = ANY($1) AND t.executor_pin IS NULL
	    AND NOT EXISTS (SELECT 1 FROM agentrt.control c WHERE c.cleared_at IS NULL AND c.control_kind IN ('stop','pause','drain')
	      AND ((c.scope = 'mission' AND c.scope_ref = t.mission_id) OR (c.scope = 'task' AND c.scope_ref = t.task_id)))`,
		dispatched).Scan(&depth); err != nil {
		return nil, err
	}
	out := make(map[string]HostSlotSignals, len(hosts))
	for _, host := range hosts {
		s := HostSlotSignals{HostID: host, Since: since, At: now, IntervalS: int64(interval / time.Second), QueueDepth: depth, Pools: []PoolRefusals{}}
		if s.Headroom, err = latestHeadroom(ctx, tx, host); err != nil {
			return nil, err
		}
		claim, err := latestClaim(ctx, tx, host)
		if err != nil {
			return nil, err
		}
		var routed []string
		for _, o := range claim.offers {
			if vp := toolOf(o.Executor) + ":" + DefaultAccount; !slices.Contains(routed, vp) {
				routed = append(routed, vp)
			}
		}
		sort.Strings(routed)
		for _, vp := range routed {
			p := pools[vp]
			p.Pool = vp
			s.Pools = append(s.Pools, p)
		}
		var median, previous *float64
		if err := tx.QueryRow(ctx, `SELECT
		    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM computed_at - started_at)::float8) FILTER (WHERE computed_at > $2),
		    count(*) FILTER (WHERE computed_at > $2),
		    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM computed_at - started_at)::float8) FILTER (WHERE computed_at <= $2),
		    count(*) FILTER (WHERE computed_at <= $2)
		  FROM agentrt.attempt WHERE host_id = $1 AND started_at IS NOT NULL AND computed_at > $3 AND computed_at <= $4`,
			host, since, since.Add(-interval), now).Scan(&median, &s.Durations.Count, &previous, &s.Durations.PreviousCount); err != nil {
			return nil, err
		}
		s.Durations.MedianS, s.Durations.PreviousMedianS = median, previous
		out[host] = s
	}
	return out, nil
}

// poolRefusals aggregates the refusals and cooldowns of (since, now] and the
// cooldowns in force at now by vendor pool.
func poolRefusals(ctx context.Context, tx pgx.Tx, since, now time.Time) (map[string]PoolRefusals, error) {
	out := map[string]PoolRefusals{}
	last := func(p *PoolRefusals, at time.Time) {
		if p.LastAt == nil || at.After(*p.LastAt) {
			p.LastAt = &at
		}
	}
	// An attempt's computed records its exit text's refusal in the same
	// transaction (attempt.go ComputedWith): the computed of the refusal's
	// transaction is the attempt that ended on it.
	rows, err := tx.Query(ctx, `SELECT e.body_json->>'vendor_pool_id', e.event_type, e.at, COALESCE(e.body_json->>'until', ''),
	    COALESCE((SELECT c.attempt_id FROM agentrt.event c WHERE c.txid = e.txid AND c.event_type = 'attempt_computed'
	      AND e.body_json->>'source' = 'exit_text' LIMIT 1), '')
	  FROM agentrt.event e
	  WHERE e.event_type IN ('pool_cooldown_set', 'pool_measured') AND e.at > $1 AND e.at <= $2
	    AND (e.event_type = 'pool_cooldown_set' OR COALESCE((e.body_json->>'limited')::boolean, false))
	  ORDER BY e.event_id`, since, now)
	if err != nil {
		return nil, err
	}
	attempts := map[string]map[string]bool{}
	var pool, kind, until, attempt string
	var at time.Time
	if _, err := pgx.ForEachRow(rows, []any{&pool, &kind, &at, &until, &attempt}, func() error {
		vp := vendorPoolOf(pool)
		if vp == "" {
			return nil
		}
		p := out[vp]
		switch kind {
		case "pool_measured":
			p.Refusals++
			last(&p, at)
			if attempt != "" {
				if attempts[vp] == nil {
					attempts[vp] = map[string]bool{}
				}
				attempts[vp][attempt] = true
			}
		case "pool_cooldown_set":
			if u, perr := time.Parse(timeLayout, until); perr == nil && u.After(at) {
				p.Cooldowns++
				last(&p, at)
			}
		}
		out[vp] = p
		return nil
	}); err != nil {
		return nil, err
	}
	for vp, as := range attempts {
		p := out[vp]
		p.LimitedAttempts = len(as)
		out[vp] = p
	}
	rows, err = tx.Query(ctx, `SELECT vendor_pool_id, cooldown_until FROM agentrt.vendor_pool WHERE cooldown_until > $1`, now)
	if err != nil {
		return nil, err
	}
	var cooling time.Time
	if _, err := pgx.ForEachRow(rows, []any{&pool, &cooling}, func() error {
		vp := vendorPoolOf(pool)
		if vp == "" {
			return nil
		}
		p := out[vp]
		if p.CoolingUntil == nil || cooling.After(*p.CoolingUntil) {
			c := cooling
			p.CoolingUntil = &c
		}
		out[vp] = p
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}
