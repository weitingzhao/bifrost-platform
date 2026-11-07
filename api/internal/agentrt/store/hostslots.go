package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// doc is s as host.signals_json keeps it: the pairs reported.
func (s HostSignals) doc() map[string]any {
	doc := map[string]any{}
	if s.CPUs > 0 {
		doc["load1"], doc["cpus"] = s.Load1, s.CPUs
	}
	if s.MemTotal > 0 {
		doc["mem_avail_bytes"], doc["mem_total_bytes"] = s.MemAvail, s.MemTotal
	}
	return doc
}

// SignalsFresh is how old a host's signals may be and still move its count:
// a host claims at least every long poll (25 seconds), so older ones say its
// agentd sends none now (an agentd from before them, or a host gone quiet).
const SignalsFresh = 2 * time.Minute

// signalsArg is s as the claim's UPDATE takes it: JSON, or NULL
// to keep what the column has.
func signalsArg(s *HostSignals) (any, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(s.doc())
	return string(b), err
}

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

// SlotHosts reads the concurrency rule in force (nil: none, and no hosts)
// and the active hosts that take ordinary work, with their counts and
// signals as of now (the database's clock, also returned). Signals older
// than SignalsFresh are not known.
func SlotHosts(ctx context.Context, db DB) (*StoredRule, []SlotHost, time.Time, error) {
	var rule *StoredRule
	var hosts []SlotHost
	var now time.Time
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		var err error
		if rule, err = CurrentRule(ctx, tx, rules.KindConcurrency, now); errors.Is(err, errBadRule) {
			rule, err = nil, nil
		}
		if err != nil || rule == nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT h.host_id, COALESCE((h.capacity_json->>'effective_slots')::int, -1),
		    (SELECT e.at FROM agentrt.event e WHERE e.event_stream_id = 'host:' || h.host_id AND e.event_type = 'host_slots_set'
		      ORDER BY e.seq DESC LIMIT 1),
		    h.signals_json IS NOT NULL AND (h.signals_json->>'at')::timestamptz >= $1::timestamptz - make_interval(secs => $2),
		    COALESCE((h.signals_json->>'load1')::float8, 0), COALESCE((h.signals_json->>'cpus')::int, 0),
		    COALESCE((h.signals_json->>'mem_avail_bytes')::bigint, 0), COALESCE((h.signals_json->>'mem_total_bytes')::bigint, 0),
		    (SELECT count(*) FROM agentrt.attempt a WHERE a.host_id = h.host_id AND a.state IN ('leased','running','stale'))
		  FROM agentrt.host h WHERE h.state = 'active' AND h.host_kind <> $3 ORDER BY h.host_id`,
			now, SignalsFresh.Seconds(), HostExternal)
		if err != nil {
			return err
		}
		hosts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SlotHost, error) {
			var h SlotHost
			var last *time.Time
			err := r.Scan(&h.Host, &h.Effective, &last, &h.Signals.Known, &h.Signals.Load1, &h.Signals.CPUs,
				&h.Signals.MemAvail, &h.Signals.MemTotal, &h.Signals.Live)
			if last != nil {
				h.LastSet = *last
			}
			return h, err
		})
		return err
	})
	return rule, hosts, now, err
}

// SetHostSlots records d for host: host_slots_set on its stream and its
// capacity_json.effective_slots, in one transaction. The host's row is taken
// first, as a claim takes it, then its stream. It writes nothing, and says
// so, when the host's recorded count is no longer recorded, the one its
// pass read (-1: none; another pass moved it since), or the host is no
// longer active.
func SetHostSlots(ctx context.Context, db DB, host string, d slots.Decision, recorded int) (bool, error) {
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		var effective int
		var state string
		err := tx.QueryRow(ctx, `SELECT state, COALESCE((capacity_json->>'effective_slots')::int, -1) FROM agentrt.host
		  WHERE host_id = $1 FOR NO KEY UPDATE`, host).Scan(&state, &effective)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (state != "active" || effective != recorded)) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = Append(ctx, tx, Event{Stream: "host:" + host, Type: "host_slots_set", Actor: "controller", Body: map[string]any{
			"host_id": host, "policy": d.Policy, "params": d.Params.Doc(), "signals": slots.SignalsDoc(d.Signals),
			"old": d.Old, "new": d.New, "reason": d.Reason}}); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE agentrt.host SET capacity_json = capacity_json || jsonb_build_object('effective_slots', $2::int)
		  WHERE host_id = $1`, host, d.New)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("store: host %s's slots were not written", host)
		}
		wrote = true
		return nil
	})
	return wrote, err
}

// SlotsInForce is the slots configuration of the concurrency rule in force,
// as GET /hosts shows it: the rule's version, its policy and parameters, and
// whether it is elastic (has a slots block); nil when no rule is in force.
func SlotsInForce(ctx context.Context, db DB) (map[string]any, error) {
	r, err := RuleInForce(ctx, db, rules.KindConcurrency)
	if errors.Is(err, errBadRule) {
		return nil, nil
	}
	if err != nil || r == nil {
		return nil, err
	}
	c := slots.ConfigOf(r.Rule)
	return map[string]any{"rule_version": r.Version, "policy": c.Policy, "elastic": c.Elastic, "params": c.Params.Doc(),
		"ceiling": slots.Ceiling}, nil
}
