package agentrtapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A collector's external work carries an immutable session identity
// (R5:5.5-12 §1.4, §6 row 3, P-b; S1-2): vendor, session and epoch on its
// registration and on every event, an optional sidebar, the host written by
// the server, the run key fixed by the identity; registered_at in the
// answers; the old Coverage count leaves collectors out.

func ident(vendor, session string, epoch int) map[string]any {
	return map[string]any{"vendor": vendor, "session": session, "epoch": epoch}
}

// putSpec uploads the description of task as caller and gives its id.
func putSpec(t *testing.T, s *httptest.Server, caller, task string) string {
	t.Helper()
	spec := collectorSpec(task)
	if code, out := call(t, s, caller, "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other", session, sessionOf(caller)); code != http.StatusOK {
		t.Fatalf("%s uploads %s: %d %v", caller, task, code, out)
	}
	return artifactID(spec)
}

func regBody(key, artifact string, id map[string]any, more ...map[string]any) map[string]any {
	return with(map[string]any{"idempotency_key": key, "root": "R2", "plan_node_id": "R2:1-1", "artifact_id": artifact}, append([]map[string]any{id}, more...)...)
}

// register registers as caller and gives the work's id, failing on anything
// but 200.
func register(t *testing.T, s *httptest.Server, caller string, body map[string]any) (string, map[string]any) {
	t.Helper()
	code, out := call(t, s, caller, "POST", "external-work", body, session, sessionOf(caller))
	if code != http.StatusOK || out["external_work_id"] == nil {
		t.Fatalf("%s registers %v: %d %v", caller, body, code, out)
	}
	return out["external_work_id"].(string), out
}

// notes are a work's notes, oldest first.
func notes(t *testing.T, pool *pgxpool.Pool, id string) []map[string]any {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT body_json->'external_work' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' ORDER BY seq`, "external-work:"+id)
	must(t, err)
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		must(t, rows.Scan(&raw))
		var n map[string]any
		must(t, json.Unmarshal(raw, &n))
		out = append(out, n)
	}
	must(t, rows.Err())
	return out
}

func eventOf(key string, seq int, typ string, id map[string]any, more ...map[string]any) map[string]any {
	return with(map[string]any{"idempotency_key": key, "seq": seq, "type": typ}, append([]map[string]any{id}, more...)...)
}

// The note of a collector's work carries the host from its token and the
// identity it registered with; its run key is the identity's, whether the
// client names it or not; a host in the body is refused. The two actor
// prefixes (actuation's, the store's) are one.
func TestCollectorIdentityRecorded(t *testing.T) {
	t.Parallel()
	if actuation.CollectorActorPrefix != store.CollectorActorPrefix {
		t.Fatalf("actuation %q, store %q", actuation.CollectorActorPrefix, store.CollectorActorPrefix)
	}
	pool, _ := world(t)
	s := server(t, pool)
	spec := putSpec(t, s, "collector:h1", "recorded")
	id, _ := register(t, s, "collector:h1", regBody("r1", spec, ident("claude", "0b8e2f4a-aaaa", 1), map[string]any{"sidebar": "local_0b8e2f4a"}))
	n := notes(t, pool, id)
	if len(n) != 1 || n[0]["host"] != "h1" || n[0]["vendor"] != "claude" || n[0]["session"] != "0b8e2f4a-aaaa" || n[0]["epoch"] != float64(1) ||
		n[0]["sidebar"] != "local_0b8e2f4a" || n[0]["source_run_key"] != "claude:0b8e2f4a-aaaa:1" || n[0]["registrant"] != "thread:collector@h1" {
		t.Fatalf("the note: %v", n)
	}
	// The same work named with its run key spelled out is the same work.
	if again, _ := register(t, s, "collector:h1", regBody("r2", spec, ident("claude", "0b8e2f4a-aaaa", 1),
		map[string]any{"sidebar": "local_0b8e2f4a", "source_run_key": "claude:0b8e2f4a-aaaa:1"})); again != id {
		t.Fatalf("the spelled-out run key is another work: %s, %s", again, id)
	}
	for name, body := range map[string]map[string]any{
		"another run key": regBody("r3", spec, ident("claude", "x", 1), map[string]any{"source_run_key": "other"}),
		"a host":          regBody("r4", spec, ident("claude", "y", 1), map[string]any{"host": "h2"}),
	} {
		if code, out := call(t, s, "collector:h1", "POST", "external-work", body); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
}

// CollectorRegisteredAt (§1.5): the registration's answer and GET
// /external-work/{id} carry registered_at; a replay of its key, another key
// for the same work, an event's answer and a recovery read give the same
// time. Another registrant's answers carry it too.
func TestCollectorRegisteredAt(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	spec := putSpec(t, s, "collector:h1", "registered at")
	body := regBody("reg", spec, ident("claude", "s-at", 1))
	id, first := register(t, s, "collector:h1", body)
	at, _ := first["registered_at"].(string)
	if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		t.Fatalf("registered_at %q: %v", at, err)
	}
	same := func(what string, out map[string]any) {
		t.Helper()
		if out["registered_at"] != at || out["external_work_id"] != id {
			t.Fatalf("%s: %v, want registered_at %s", what, out, at)
		}
	}
	_, replay := register(t, s, "collector:h1", body)
	same("a replay of its key", replay)
	_, other := register(t, s, "collector:h1", with(body, map[string]any{"idempotency_key": "reg-2"}))
	same("another key", other)
	time.Sleep(5 * time.Millisecond)
	code, ev := call(t, s, "collector:h1", "POST", "external-work/"+id+"/events", eventOf("start", 1, "started", ident("claude", "s-at", 1)))
	if code != http.StatusOK {
		t.Fatalf("started: %d %v", code, ev)
	}
	same("an event's answer", ev)
	for _, caller := range []string{"collector:h1", "viewer"} {
		code, out := call(t, s, caller, "GET", "external-work/"+id, nil)
		if code != http.StatusOK {
			t.Fatalf("%s reads: %d %v", caller, code, out)
		}
		same(caller+"'s recovery read", out)
	}
	_, mcp := register(t, s, "reporter", map[string]any{"idempotency_key": "mcp", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "mcp-run",
		"artifact_id": putSpec(t, s, "operator", "mcp at")})
	if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(mcp["registered_at"])); err != nil {
		t.Fatalf("the reporter's answer: %v", mcp)
	}
}

// ForeignWork_IdentityRequired (§1.5): a collector's registration or event
// missing a field of its identity, empty, null or of the wrong shape, a
// linked event without one included, is 400 identity_required and writes
// nothing.
func TestForeignWork_IdentityRequired(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	spec := putSpec(t, s, "collector:h1", "required")
	good := ident("claude", "s-req", 1)
	without := func(m map[string]any, k string) map[string]any {
		out := with(m)
		delete(out, k)
		return out
	}
	bad := map[string]map[string]any{}
	for _, k := range []string{"vendor", "session", "epoch"} {
		bad["no "+k] = without(good, k)
		bad["null "+k] = with(good, map[string]any{k: nil})
	}
	for name, v := range map[string]map[string]any{
		"empty vendor": {"vendor": ""}, "empty session": {"session": ""}, "epoch 0": {"epoch": 0}, "epoch -1": {"epoch": -1},
		"another vendor": {"vendor": "gemini"}, "a vendor capitalised": {"vendor": "Claude"}, "a session with a space": {"session": "s req"},
		"a session too long": {"session": strings.Repeat("s", 129)}, "a sidebar with a space": {"sidebar": "local x"},
	} {
		bad[name] = with(good, v)
	}
	bad["no identity"] = map[string]any{}
	for name, id := range bad {
		code, out := call(t, s, "collector:h1", "POST", "external-work", regBody("k-"+name, spec, id))
		if code != http.StatusBadRequest || out["error"] != "identity_required" {
			t.Errorf("registration, %s: %d %v", name, code, out)
		}
	}
	// A string epoch is not a number at all: refused by the decoder.
	if code, out := call(t, s, "collector:h1", "POST", "external-work", regBody("k-str", spec, with(good, map[string]any{"epoch": "1"}))); code != http.StatusBadRequest {
		t.Errorf("a string epoch: %d %v", code, out)
	}
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id LIKE 'external-work:%'`).Scan(&n))
	if n != 0 {
		t.Fatalf("a refused registration wrote %d notes", n)
	}
	id, _ := register(t, s, "collector:h1", regBody("reg", spec, good))
	delete(bad, "a sidebar with a space") // an event names no sidebar
	for name, idn := range bad {
		for _, e := range []map[string]any{eventOf("e-"+name, 1, "started", idn),
			eventOf("l-"+name, 1, "linked", idn, map[string]any{"root": "R2", "plan_node_id": "R2:1-1"})} {
			code, out := call(t, s, "collector:h1", "POST", "external-work/"+id+"/events", e)
			if code != http.StatusBadRequest || out["error"] != "identity_required" {
				t.Errorf("%s event, %s: %d %v", e["type"], name, code, out)
			}
		}
	}
	if got := notes(t, pool, id); len(got) != 1 {
		t.Fatalf("a refused event wrote: %v", got)
	}
}

// ForeignWork_IdentityMismatch (§1.5): an event naming its session with
// another epoch, or another valid vendor, or another session's whole
// identity (session B's on W_A), linked included, is 409 identity_mismatch
// and changes nothing; the work's own identity goes on.
func TestForeignWork_IdentityMismatch(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	a, b := ident("claude", "s-a", 1), ident("claude", "s-b", 1)
	wa, _ := register(t, s, "collector:h1", regBody("ra", putSpec(t, s, "collector:h1", "work a"), a))
	register(t, s, "collector:h1", regBody("rb", putSpec(t, s, "collector:h1", "work b"), b))
	for name, e := range map[string]map[string]any{
		"another epoch":        eventOf("m1", 1, "started", ident("claude", "s-a", 2)),
		"another vendor":       eventOf("m2", 1, "started", ident("cursor", "s-a", 1)),
		"session B's":          eventOf("m3", 1, "started", b),
		"session B's, linked":  eventOf("m4", 1, "linked", b, map[string]any{"root": "R2", "plan_node_id": "R2:1-1"}),
		"another epoch, later": eventOf("m5", 2, "heartbeat", ident("claude", "s-a", 2)),
	} {
		code, out := call(t, s, "collector:h1", "POST", "external-work/"+wa+"/events", e)
		if code != http.StatusConflict || out["error"] != "identity_mismatch" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if got := notes(t, pool, wa); len(got) != 1 {
		t.Fatalf("a refused event wrote: %v", got)
	}
	if code, out := call(t, s, "collector:h1", "POST", "external-work/"+wa+"/events", eventOf("ok", 1, "started", a)); code != http.StatusOK || out["state"] != "Running" {
		t.Fatalf("its own identity: %d %v", code, out)
	}
}

// ForeignWork_ReplayOtherIdentity (§1.5): an event replayed with its key
// and its seq under another identity is 409 identity_mismatch, not the key's
// key_conflict (TD-516), as is one under another key at the same seq; the
// replay as sent the first time is the first answer.
func TestForeignWork_ReplayOtherIdentity(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	a := ident("claude", "s-a", 1)
	wa, _ := register(t, s, "collector:h1", regBody("ra", putSpec(t, s, "collector:h1", "replay a"), a))
	register(t, s, "collector:h1", regBody("rb", putSpec(t, s, "collector:h1", "replay b"), ident("claude", "s-b", 1)))
	path := "external-work/" + wa + "/events"
	code, first := call(t, s, "collector:h1", "POST", path, eventOf("s1", 1, "started", a))
	if code != http.StatusOK {
		t.Fatalf("started: %d %v", code, first)
	}
	for name, e := range map[string]map[string]any{
		"its key, session B's":   eventOf("s1", 1, "started", ident("claude", "s-b", 1)),
		"its key, another epoch": eventOf("s1", 1, "started", ident("claude", "s-a", 2)),
		"another key, its seq":   eventOf("s1-other", 1, "started", ident("claude", "s-b", 1)),
	} {
		if code, out := call(t, s, "collector:h1", "POST", path, e); code != http.StatusConflict || out["error"] != "identity_mismatch" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	// Its own identity under its key with another body is still the key's
	// conflict: only the identity is compared ahead of the key.
	if code, out := call(t, s, "collector:h1", "POST", path, eventOf("s1", 1, "heartbeat", a)); code != http.StatusConflict || out["error"] != "key_conflict" {
		t.Errorf("its key, its identity, another body: %d %v", code, out)
	}
	if code, again := call(t, s, "collector:h1", "POST", path, eventOf("s1", 1, "started", a)); code != http.StatusOK || !reflect.DeepEqual(again, first) {
		t.Fatalf("the replay as sent: %d %v, first %v", code, again, first)
	}
	if got := notes(t, pool, wa); len(got) != 2 {
		t.Fatalf("notes after the replays: %v", got)
	}
}

// ForeignWork_Residual (§1.5, N15 (i) accepted): on one host, with its one
// token, the collector naming session B's whole identity on W_B is
// accepted, whichever of the host's sessions sent it: the positive control
// that marks what the identity does not stop.
func TestForeignWork_Residual(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	b := ident("claude", "s-b", 1)
	register(t, s, "collector:h1", regBody("ra", putSpec(t, s, "collector:h1", "residual a"), ident("claude", "s-a", 1)))
	wb, _ := register(t, s, "collector:h1", regBody("rb", putSpec(t, s, "collector:h1", "residual b"), b))
	if code, out := call(t, s, "collector:h1", "POST", "external-work/"+wb+"/events", eventOf("from-a", 1, "started", b), session, "s-a"); code != http.StatusOK {
		t.Fatalf("B's identity on W_B: %d %v", code, out)
	}
}

// BindingAtomicity (S1-2): one identity is one work. Registered again it is
// the same work, one note; registered at once from many requests one note;
// an interrupted request replayed gets its first answer; the same identity
// with another description is 409; linked to another step, or with a task,
// it is still the same work, with its registrant, host and identity.
func TestBindingAtomicity(t *testing.T) {
	t.Parallel()
	t.Run("registered again", func(t *testing.T) {
		pool, _ := world(t)
		s := server(t, pool)
		spec := putSpec(t, s, "collector:h1", "again")
		var ids []string
		for i := 0; i < 3; i++ {
			id, _ := register(t, s, "collector:h1", regBody(fmt.Sprintf("k%d", i), spec, ident("claude", "s-again", 1)))
			ids = append(ids, id)
		}
		if ids[0] != ids[1] || ids[1] != ids[2] || len(notes(t, pool, ids[0])) != 1 {
			t.Fatalf("ids %v, notes %v", ids, notes(t, pool, ids[0]))
		}
	})
	t.Run("at once", func(t *testing.T) {
		pool, _ := world(t)
		s := server(t, pool)
		spec := putSpec(t, s, "collector:h1", "at once")
		const n = 8
		var wg sync.WaitGroup
		codes, outs := make([]int, 2*n), make([]map[string]any, 2*n)
		for i := 0; i < 2*n; i++ {
			key := fmt.Sprintf("k%d", i)
			if i >= n {
				key = "one-key" // half under one key, half under keys of their own
			}
			wg.Add(1)
			go func(i int, key string) {
				defer wg.Done()
				codes[i], outs[i] = call(t, s, "collector:h1", "POST", "external-work", regBody(key, spec, ident("claude", "s-once", 1)))
			}(i, key)
		}
		wg.Wait()
		for i := range codes {
			if codes[i] != http.StatusOK || outs[i]["external_work_id"] != outs[0]["external_work_id"] || outs[i]["registered_at"] != outs[0]["registered_at"] {
				t.Fatalf("request %d: %d %v; first %v", i, codes[i], outs[i], outs[0])
			}
		}
		if got := notes(t, pool, outs[0]["external_work_id"].(string)); len(got) != 1 {
			t.Fatalf("%d notes", len(got))
		}
	})
	t.Run("interrupted and replayed", func(t *testing.T) {
		pool, _ := world(t)
		s := server(t, pool)
		id := ident("claude", "s-int", 1)
		body := regBody("reg", putSpec(t, s, "collector:h1", "interrupted"), id)
		w, first := register(t, s, "collector:h1", body) // its answer lost
		if _, again := register(t, s, "collector:h1", body); !reflect.DeepEqual(again, first) {
			t.Fatalf("the replayed registration: %v, first %v", again, first)
		}
		e := eventOf("start", 1, "started", id)
		_, ev := call(t, s, "collector:h1", "POST", "external-work/"+w+"/events", e) // its answer lost
		if code, again := call(t, s, "collector:h1", "POST", "external-work/"+w+"/events", e); code != http.StatusOK || !reflect.DeepEqual(again, ev) {
			t.Fatalf("the replayed event: %d %v, first %v", code, again, ev)
		}
		if got := notes(t, pool, w); len(got) != 2 {
			t.Fatalf("notes: %v", got)
		}
	})
	t.Run("the same identity, other content", func(t *testing.T) {
		pool, _ := world(t)
		s := server(t, pool)
		id := ident("claude", "s-content", 1)
		register(t, s, "collector:h1", regBody("reg", putSpec(t, s, "collector:h1", "content one"), id))
		for name, body := range map[string]map[string]any{
			"another description": regBody("reg-2", putSpec(t, s, "collector:h1", "content two"), id),
			"a sidebar":           regBody("reg-3", putSpec(t, s, "collector:h1", "content one"), id, map[string]any{"sidebar": "local_x"}),
		} {
			if code, out := call(t, s, "collector:h1", "POST", "external-work", body); code != http.StatusConflict {
				t.Fatalf("%s: %d %v", name, code, out)
			}
		}
	})
	t.Run("linked, still the same work", func(t *testing.T) {
		pool, task := world(t)
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:1-2", Parent: "R2:1", Kind: "step", Label: "1-2", Title: "step 2", StepKind: "agent", Ordinal: 2, Root: "R2"})
			return err
		}))
		s := server(t, pool)
		id := ident("claude", "s-linked", 1)
		w, _ := register(t, s, "collector:h1", regBody("reg", putSpec(t, s, "collector:h1", "linked"), id))
		for i, e := range []map[string]any{
			eventOf("l1", 1, "linked", id, map[string]any{"root": "R2", "plan_node_id": "R2:1-2"}),
			eventOf("l2", 2, "linked", id, map[string]any{"root": "R2", "plan_node_id": "R2:1-1", "task_id": task}),
			eventOf("st", 3, "started", id),
		} {
			code, out := call(t, s, "collector:h1", "POST", "external-work/"+w+"/events", e)
			if code != http.StatusOK || out["external_work_id"] != w {
				t.Fatalf("event %d: %d %v", i, code, out)
			}
		}
		got := notes(t, pool, w)
		last := got[len(got)-1]
		if len(got) != 4 || last["plan_node_id"] != "R2:1-1" || last["task_id"] != task || last["registrant"] != "thread:collector@h1" ||
			last["host"] != "h1" || last["vendor"] != "claude" || last["session"] != "s-linked" || last["epoch"] != float64(1) || last["state"] != "Running" {
			t.Fatalf("notes: %v", got)
		}
		if got[1]["plan_node_id"] != "R2:1-2" {
			t.Fatalf("the first link: %v", got[1])
		}
	})
}

// The old Coverage count (§1.1, §3.1) leaves a host's collector out: a
// thread with plan-node events in the root and no registration there is
// counted, a collector with the same is not.
func TestCoverageCountsNoCollector(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	putSpec(t, s, "collector:h1", "coverage")                                                                    // registers thread:collector@h1
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID([]byte("# cov\n")), []byte("# cov\n"), // registers thread:cov-s
		"X-Artifact-Kind", "report", session, "cov-s"); code != http.StatusOK {
		t.Fatalf("the thread's upload: %d %v", code, out)
	}
	for _, actor := range []string{"thread:collector@h1", "thread:cov-s"} {
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "coverage-test", Type: "note", Actor: actor, PlanNode: "R2:1-1", Body: map[string]any{"text": "plan-node work of " + actor}})
			return err
		}))
	}
	code, out := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	cov, _ := out["coverage"].(map[string]any)
	if code != http.StatusOK || cov["unregistered_work"] != float64(1) {
		t.Fatalf("coverage: %d %v", code, out["coverage"])
	}
}

// Every other registrant is as it was: a reporter's, an operator's and a
// seat's registrations and events name no identity and work as before; one
// naming a field of it, in any letter case or null, is 400; its notes, its
// answers and its plan-view rows carry none of the new fields but
// registered_at. A collector's note from before the identity reads as it
// was, and its events are refused, the identity unknown.
func TestIdentityOnlyFromCollector(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	for _, caller := range []string{"reporter", "operator", "seat:chief_of_staff"} {
		spec := putSpec(t, s, caller, "old "+caller)
		body := map[string]any{"idempotency_key": "reg", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "run-" + caller, "artifact_id": spec}
		for name, extra := range map[string]map[string]any{"vendor": {"vendor": "claude"}, "Session": {"Session": "s"}, "null epoch": {"epoch": nil},
			"sidebar": {"sidebar": "local_x"}, "host": {"host": "h1"}} {
			if code, out := call(t, s, caller, "POST", "external-work", with(body, extra), session, "old-"+caller); code != http.StatusBadRequest {
				t.Fatalf("%s naming %s: %d %v", caller, name, code, out)
			}
		}
		code, out := call(t, s, caller, "POST", "external-work", body, session, "old-"+caller)
		if code != http.StatusOK {
			t.Fatalf("%s registers: %d %v", caller, code, out)
		}
		w := out["external_work_id"].(string)
		e := map[string]any{"idempotency_key": "start", "seq": 1, "type": "started"}
		if code, out := call(t, s, caller, "POST", "external-work/"+w+"/events", with(e, ident("claude", "s", 1)), session, "old-"+caller); code != http.StatusBadRequest {
			t.Fatalf("%s's event naming an identity: %d %v", caller, code, out)
		}
		if code, out := call(t, s, caller, "POST", "external-work/"+w+"/events", e, session, "old-"+caller); code != http.StatusOK {
			t.Fatalf("%s's event: %d %v", caller, code, out)
		}
		for _, n := range notes(t, pool, w) {
			for _, k := range []string{"host", "vendor", "session", "epoch", "sidebar"} {
				if _, ok := n[k]; ok {
					t.Fatalf("%s's note carries %s: %v", caller, k, n)
				}
			}
		}
		_, got := call(t, s, "viewer", "GET", "external-work/"+w, nil)
		var keys []string
		for k := range got {
			keys = append(keys, k)
		}
		if len(got) != 6 || got["registered_at"] == nil {
			t.Fatalf("%s's work reads %v", caller, keys)
		}
	}
	_, view := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	rows, _ := view["external_work"].([]any)
	if len(rows) != 3 {
		t.Fatalf("plan-view external_work: %v", view["external_work"])
	}
	for _, r := range rows {
		for _, k := range []string{"host", "vendor", "session", "epoch", "sidebar"} {
			if _, ok := r.(map[string]any)[k]; ok {
				t.Fatalf("a plan-view row carries %s: %v", k, r)
			}
		}
	}

	// A collector's note from before (none can be made now: the store
	// refuses a collector's write without its identity).
	spec := putSpec(t, s, "collector:h1", "from before")
	legacy := "ew_" + fmt.Sprintf("%064x", 11)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		digest := "sha256:" + strings.Repeat("0", 64)
		_, err := store.Append(ctx, tx, store.Event{Stream: "external-work:" + legacy, Type: "note", Actor: "thread:collector@h1", PlanNode: "R2:1-1",
			Body: map[string]any{"text": "External work registered", "external_work": map[string]any{"v": 1, "kind": "agentrt.external_work/v1",
				"external_work_id": legacy, "registrant": "thread:collector@h1", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "before",
				"artifact_id": spec, "seq": 0, "type": "registered", "state": "Registered", "request_digest": digest, "registration_digest": digest,
				"registered_at": now, "heartbeat_at": now}}})
		return err
	}))
	if code, out := call(t, s, "collector:h1", "GET", "external-work/"+legacy, nil); code != http.StatusOK || out["external_work_id"] != legacy || out["registered_at"] == nil {
		t.Fatalf("the note from before reads: %d %v", code, out)
	}
	if code, out := call(t, s, "collector:h1", "POST", "external-work/"+legacy+"/events", eventOf("e", 1, "started", ident("claude", "before", 1))); code != http.StatusConflict || out["error"] != "identity_mismatch" {
		t.Fatalf("an event on the note from before: %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "GET", "plan-view?root=R2", nil); code != http.StatusOK {
		t.Fatalf("plan-view with the note from before: %d", code)
	}
	// The store holds a collector's actor to its host, both ways, and an
	// identity to a collector (the route refuses one first).
	for _, w := range []store.Writer{{Actor: "thread:collector@h1", Token: "collector:h1"}, {Actor: "thread:x", Token: "reporter", Host: "h1"}} {
		if _, err := store.RegisterExternalWork(ctx, pool, w, store.ExternalRegistration{Key: "k", Root: "R2", Node: "R2:1-1", RunKey: "r", Artifact: spec}); err == nil {
			t.Fatalf("%+v registered", w)
		}
	}
	plain := store.Writer{Actor: "thread:s-store", Token: "reporter"}
	if _, err := store.RegisterExternalWork(ctx, pool, plain, store.ExternalRegistration{Key: "k", Root: "R2", Node: "R2:1-1", RunKey: "r", Artifact: spec,
		Vendor: "claude", Session: "s", Epoch: 1}); err == nil {
		t.Fatal("a plain writer registered an identity")
	}
	if _, err := store.PostExternalEvent(ctx, pool, plain, legacy, store.ExternalEvent{Key: "e", Seq: 1, Type: "started", Vendor: "claude", Session: "s", Epoch: 1}); err == nil {
		t.Fatal("a plain writer posted an identity")
	}
}

// The plan view carries a collector's host and identity on its row, as
// registered (P-c1: the Console reads them from here), and nothing new on
// any other registrant's row.
func TestPlanViewCarriesCollectorIdentity(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	cw, _ := register(t, s, "collector:h1", regBody("rc", putSpec(t, s, "collector:h1", "plan view collector"), ident("cursor", "s-view", 2),
		map[string]any{"sidebar": "local_view"}))
	mw, _ := register(t, s, "reporter", map[string]any{"idempotency_key": "rm", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "view-mcp",
		"artifact_id": putSpec(t, s, "reporter", "plan view mcp")})
	code, view := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	if code != http.StatusOK {
		t.Fatalf("plan-view: %d %v", code, view)
	}
	byID := map[string]map[string]any{}
	for _, list := range []string{"external_work", "working"} {
		for _, r := range view[list].([]any) {
			row := r.(map[string]any)
			byID[list+"/"+row["external_work_id"].(string)] = row
		}
	}
	for _, list := range []string{"external_work", "working"} {
		c, m := byID[list+"/"+cw], byID[list+"/"+mw]
		if c == nil || m == nil || c["host"] != "h1" || c["vendor"] != "cursor" || c["session"] != "s-view" || c["epoch"] != float64(2) || c["sidebar"] != "local_view" {
			t.Fatalf("%s: the collector's row %v, the reporter's %v", list, c, m)
		}
		for _, k := range []string{"host", "vendor", "session", "epoch", "sidebar"} {
			if _, ok := m[k]; ok {
				t.Fatalf("%s: the reporter's row carries %s: %v", list, k, m)
			}
		}
	}
}

// The plan view's coverage.sessions (R5:5.5-12 §3.2, P-c2): the platform's
// session counts as its provider gives them, the four old keys as they were;
// a provider that fails leaves it out and says so in the warnings; none
// wired, the view is as before.
func TestPlanViewSessionCoverage(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	counts := map[string]any{"linked": 2, "unlinked": 1, "refused": 0, "unsupported": 1, "unknown": 0, "scope": "Sessions heard from in the last 72 hours"}
	for _, c := range []struct {
		name     string
		sessions func(context.Context) (any, error)
	}{
		{"counted", func(context.Context) (any, error) { return counts, nil }},
		{"unread", func(context.Context) (any, error) { return nil, errors.New("statefile unreadable") }},
		{"not wired", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := server(t, pool, func(a *agentrtapi.API) { a.Sessions = c.sessions })
			code, view := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
			cov, _ := view["coverage"].(map[string]any)
			if code != http.StatusOK || cov["unregistered_work"] == nil || cov["label"] == nil || cov["scope"] == nil || cov["reconciled"] == nil {
				t.Fatalf("plan-view: %d %v", code, view["coverage"])
			}
			warned := strings.Contains(fmt.Sprint(view["warnings"]), "Session coverage is Unknown")
			switch c.name {
			case "counted":
				if got, _ := json.Marshal(cov["sessions"]); string(got) != `{"linked":2,"refused":0,"scope":"Sessions heard from in the last 72 hours","unknown":0,"unlinked":1,"unsupported":1}` || warned {
					t.Fatalf("sessions %s, warned %v", got, warned)
				}
			case "unread":
				if cov["sessions"] != nil || !warned || strings.Contains(fmt.Sprint(view), "statefile unreadable") {
					t.Fatalf("an unread provider: %v, warnings %v", cov, view["warnings"])
				}
			default:
				if cov["sessions"] != nil || warned {
					t.Fatalf("no provider: %v, warnings %v", cov, view["warnings"])
				}
			}
		})
	}
}
