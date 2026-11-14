package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/slots"
)

// Elastic executor slots (R5 phase 2 step 3b; the Owner 2026-11-06 Q3, Q4;
// package slots). A host's agentd sends its headroom with each claim
// (HostSignals); the claim keeps it in host.signals_json, a liveness column
// written without an event (0014). The controller's slots pass
// (controller.Slots) reads it with the concurrency rule in force, asks the
// rule's policy for each host's count every interval_s, and records a move
// as host_slots_set on the host's stream together with
// host.capacity_json.effective_slots, in one transaction (SetHostSlots).
// The claim leases up to the least of the host's own slots, the count
// recorded (the rule's start while none is), per_host and slots.Ceiling
// (grantedSlots), and answers the count it granted (Claim.GrantedSlots,
// GrantedSlots), so agentd asks for no more. A rule without a slots block
// reads no recorded count: the host has per_host, as before.

// HostSignals is the headroom a claim reports, in two pairs: the one-minute
// load with the CPUs, and the memory available with the memory in all
// (bytes). A pair the host did not report is zero (CPUs 0, MemTotal 0) and
// is not kept: a load of 0 is a reading, not its absence.
type HostSignals struct {
	Load1    float64
	CPUs     int
	MemAvail int64
	MemTotal int64
}

// Valid says what is wrong with s, "" when nothing is.
func (s HostSignals) Valid() string {
	switch {
	case s.Load1 < 0 || s.CPUs < 0 || s.MemAvail < 0 || s.MemTotal < 0:
		return "headroom signals are not negative"
	case s.MemAvail > s.MemTotal:
		return "mem_avail_bytes is at most mem_total_bytes"
	}
	return ""
}

// SignalsFresh is how old a host's signals may be and still move its count:
// a host claims at least every long poll (25 seconds), so older ones say its
// agentd sends none now (an agentd from before them, or a host gone quiet).
const SignalsFresh = 2 * time.Minute

// grantedSlots is how many live attempts a host may hold now: its own slots,
// the count the slots pass recorded for it (the rule's start while none is;
// only under a rule with a slots block), per_host and slots.Ceiling; none
// without a concurrency rule in force.
func grantedSlots(ctx context.Context, tx pgx.Tx, stated int, effective *int) (int, error) {
	r, err := inForce(ctx, tx, rules.KindConcurrency)
	if err != nil || r == nil {
		return 0, err
	}
	return grantOf(slots.ConfigOf(r.Rule), stated, effective), nil
}

func grantOf(c slots.Config, stated int, effective *int) int {
	n := min(stated, c.Params.Max)
	if !c.Elastic {
		return max(n, 0)
	}
	e := c.Params.Start
	if effective != nil {
		e = *effective
	}
	return max(min(n, slots.Clamp(e, c.Params.Min, c.Params.Max)), 0)
}

// GrantedSlots is the count a claim of host would lease up to now, for the
// claim's answer when it leased nothing; 0 for a host not registered.
func GrantedSlots(ctx context.Context, db DB, host string) (int, error) {
	n := 0
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		var stated int
		var effective *int
		err := tx.QueryRow(ctx, `SELECT COALESCE((capacity_json->>'slots')::int, $2), (capacity_json->>'effective_slots')::int
		  FROM agentrt.host WHERE host_id = $1`, host, rules.PerHostCeiling).Scan(&stated, &effective)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err = grantedSlots(ctx, tx, stated, effective)
		return err
	})
	return n, err
}

// SlotMovesShown is how many of a host's latest moves the hosts document
// shows (HostsSQL).
const SlotMovesShown = 10

// SlotHost is a host the slots pass decides for: its recorded count (-1:
// none yet), when the last was recorded (zero: never), and its signals.
type SlotHost struct {
	Host      string
	Effective int
	LastSet   time.Time
	Signals   slots.Signals
}
