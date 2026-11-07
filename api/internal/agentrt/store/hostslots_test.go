package store_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/slots"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// slotsWorld is a world whose host h1 may take work of M-1, under the dev
// budget and the concurrency rule text (version 1, signed).
func slotsWorld(t *testing.T, rule string) *world {
	t.Helper()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h1", "dev"))
	w.jobs(t, "active", "*", codex)
	_, err := w.rt.Exec(ctx, agentrttest.DevBudgetRule)
	ok(t, err)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, fmt.Sprintf(concurrencyHead, 1, rules.Initial)+rule)))
	return w
}

var easyHost = &store.HostSignals{Load1: 1, CPUs: 8, MemAvail: 8 << 30, MemTotal: 16 << 30}

func (w *world) claimSlots(t *testing.T, free int, sig *store.HostSignals) *store.Claim {
	t.Helper()
	c, err := store.ClaimFor(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: free, Tools: []store.Tool{codex}, Signals: sig}, time.Hour)
	ok(t, err)
	return c
}

func granted(t *testing.T, w *world) int {
	t.Helper()
	n, err := store.GrantedSlots(ctx, w.rt, "h1")
	ok(t, err)
	return n
}

// R5 phase 2 step 3b (elastic slots, the Owner 2026-11-06 Q3, Q4): under a
// slots block the claim leases up to the count the slots pass recorded, the
// rule's start while none is, and answers it; the claim keeps the host's
// headroom for the pass; a move is one event and capacity_json together, and
// a pass that read an older count writes nothing; a tighter rule holds a
// recorded count to its range at once.
func TestClaimLeasesUpToTheRecordedSlots(t *testing.T) {
	t.Parallel()
	w := slotsWorld(t, "per_host: 3\nslots: {policy: aimd, min: 1, start: 1}\n")
	for range 3 {
		w.ready(t, store.Task{})
	}
	if n := granted(t, w); n != 1 {
		t.Fatalf("granted before any move: %d, want the start 1", n)
	}
	c := w.claimSlots(t, 3, easyHost)
	if c == nil || c.GrantedSlots != 1 {
		t.Fatalf("first claim: %+v, want a lease granting 1", c)
	}
	if c := w.claimSlots(t, 3, easyHost); c != nil {
		t.Fatalf("a second lease at the start of 1: %s", c.TaskID)
	}
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT signals_json FROM agentrt.host WHERE host_id = 'h1'`).Scan(&raw))
	var sig map[string]any
	ok(t, json.Unmarshal(raw, &sig))
	if sig["load1"] != 1.0 || sig["cpus"] != 8.0 || sig["mem_total_bytes"] != float64(16<<30) || sig["at"] == nil {
		t.Fatalf("the host's signals: %s", raw)
	}

	rule, hosts, _, err := store.SlotHosts(ctx, w.rt)
	ok(t, err)
	if rule == nil || len(hosts) != 1 || hosts[0].Effective != -1 || !hosts[0].LastSet.IsZero() || !hosts[0].Signals.Known || hosts[0].Signals.Live != 1 {
		t.Fatalf("slot hosts: %+v %+v", rule, hosts)
	}
	d, err := slots.Decide(slots.ConfigOf(rule.Rule), hosts[0].Effective, hosts[0].Signals)
	ok(t, err)
	if d.Old != 1 || d.New != 2 {
		t.Fatalf("decision %+v", d)
	}
	wrote, err := store.SetHostSlots(ctx, w.rt, "h1", d, -1)
	ok(t, err)
	if !wrote {
		t.Fatal("the move was not written")
	}
	if wrote, err = store.SetHostSlots(ctx, w.rt, "h1", d, -1); err != nil || wrote {
		t.Fatalf("a pass that read no count wrote over 2: %v %v", wrote, err)
	}
	var body []byte
	var effective int
	ok(t, w.rt.QueryRow(ctx, `SELECT e.body_json, (h.capacity_json->>'effective_slots')::int FROM agentrt.event e, agentrt.host h
	  WHERE e.event_stream_id = 'host:h1' AND e.event_type = 'host_slots_set' AND h.host_id = 'h1'`).Scan(&body, &effective))
	var ev map[string]any
	ok(t, json.Unmarshal(body, &ev))
	if effective != 2 || ev["host_id"] != "h1" || ev["policy"] != "aimd" || ev["old"] != 1.0 || ev["new"] != 2.0 || ev["reason"] == "" ||
		ev["signals"].(map[string]any)["load1_hundredths"] != 100.0 || ev["params"].(map[string]any)["max"] != 3.0 {
		t.Fatalf("effective %d, event %s", effective, body)
	}

	if c := w.claimSlots(t, 2, easyHost); c == nil || c.GrantedSlots != 2 {
		t.Fatalf("after the move to 2: %+v", c)
	}
	if c := w.claimSlots(t, 1, easyHost); c != nil {
		t.Fatalf("a third lease at 2: %s", c.TaskID)
	}
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "concurrency",
		[]byte(fmt.Sprintf(concurrencyHead, 2, rules.Tighten)+"per_host: 1\nslots: {policy: aimd, min: 1, start: 1}\n"))
	ok(t, err)
	if n := granted(t, w); n != 1 {
		t.Fatalf("granted under per_host 1 with 2 recorded: %d", n)
	}
}

// A rule without a slots block reads no recorded count: the host has
// per_host, as before; a claim without signals keeps what the host sent.
func TestClaimWithoutASlotsBlockIgnoresRecordedSlots(t *testing.T) {
	t.Parallel()
	w := slotsWorld(t, "per_host: 2\n")
	w.ready(t, store.Task{})
	if c := w.claimSlots(t, 2, easyHost); c == nil || c.GrantedSlots != 2 {
		t.Fatalf("claim: %+v", c)
	}
	rule, hosts, _, err := store.SlotHosts(ctx, w.rt)
	ok(t, err)
	if rule == nil || len(hosts) != 1 {
		t.Fatalf("slot hosts %+v", hosts)
	}
	d := slots.Decision{Policy: "fixed", Params: slots.Params{Min: 1, Start: 1, Max: 1}, Old: 2, New: 1, Reason: "test"}
	wrote, err := store.SetHostSlots(ctx, w.rt, "h1", d, -1)
	ok(t, err)
	if !wrote || granted(t, w) != 2 {
		t.Fatalf("wrote %v, granted %d; want per_host 2 whatever is recorded", wrote, granted(t, w))
	}
	if c := w.claimSlots(t, 1, nil); c != nil {
		t.Fatalf("leased %s", c.TaskID)
	}
	var known bool
	ok(t, w.rt.QueryRow(ctx, `SELECT signals_json->>'cpus' = '8' FROM agentrt.host WHERE host_id = 'h1'`).Scan(&known))
	if !known {
		t.Fatal("a claim without signals cleared the host's")
	}
	if n, err := store.GrantedSlots(ctx, w.rt, "nobody"); err != nil || n != 0 {
		t.Fatalf("an unregistered host: %d %v", n, err)
	}
}

// The database holds the guards: a host's signals move without an event, its
// capacity only with one of the types that write the host, and
// host_slots_set's body is the closed one.
func TestHostSlotsGuards(t *testing.T) {
	t.Parallel()
	w := slotsWorld(t, "per_host: 1\n")
	if _, err := w.rt.Exec(ctx, `UPDATE agentrt.host SET signals_json = '{"cpus": 2}' WHERE host_id = 'h1'`); err != nil {
		t.Fatalf("signals as liveness: %v", err)
	}
	if _, err := w.rt.Exec(ctx, `UPDATE agentrt.host SET capacity_json = '{"effective_slots": 3}' WHERE host_id = 'h1'`); err == nil {
		t.Fatal("capacity written without an event")
	}
	err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "host:h1", Type: "host_slots_set", Actor: "controller",
			Body: map[string]any{"host_id": "h1", "policy": "pid", "params": map[string]any{}, "signals": map[string]any{}, "old": 1, "new": 2, "reason": "x"}})
		return err
	})
	if err == nil {
		t.Fatal("an unknown policy was written")
	}
}
