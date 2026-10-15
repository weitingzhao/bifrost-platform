package agentrtapi_test

import (
	"net/http"
	"testing"
)

// Owner 2026-10-14 「主机登记 A」: the first claim of an agentd token
// registers the host it is bound to, naming the token; a second claim does
// not register it again.
func TestFirstClaimRegistersTheHost(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.host WHERE host_id = 'h3'`).Scan(&before))
	if before != 0 {
		t.Fatalf("h3 is registered before its first claim")
	}
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	body := map[string]any{"host_id": "h3", "free_slots": 1, "tools": []any{cursor}}
	for range 2 {
		if code, out := call(t, s, "agentd:h3", "POST", "claim", body); code != http.StatusOK && code != http.StatusNoContent {
			t.Fatalf("claim on h3: %d %v", code, out)
		}
	}
	var hosts, events int
	var token string
	must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.host WHERE host_id = 'h3' AND host_kind = 'dev' AND state = 'active'),
	    count(*), COALESCE(max(body_json->>'token'), '') FROM agentrt.event WHERE event_type = 'host_registered' AND body_json->>'host_id' = 'h3'`).
		Scan(&hosts, &events, &token))
	if hosts != 1 || events != 1 || token != "agentd:h3" {
		t.Fatalf("h3: %d hosts, %d registrations, token %q; want one, once, by agentd:h3", hosts, events, token)
	}
	// A token bound to another host still may not claim for this one.
	if code, out := call(t, s, "agentd:h2", "POST", "claim", body); code != http.StatusForbidden || out["error"] != "wrong_host" {
		t.Fatalf("agentd:h2 claiming for h3: %d %v", code, out)
	}
}

// Review 190 B5: a claim refused as malformed registers nothing; two first
// claims at once both succeed, and the host is registered once.
func TestFirstClaimRegistrationEdges(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	bad := map[string]any{"host_id": "h3", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor"}}}
	if code, _ := call(t, s, "agentd:h3", "POST", "claim", bad); code != http.StatusBadRequest {
		t.Fatalf("a tool without its provider: %d", code)
	}
	var hosts int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.host WHERE host_id = 'h3'`).Scan(&hosts))
	if hosts != 0 {
		t.Fatal("a malformed claim registered h3")
	}
	body := map[string]any{"host_id": "h3", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}}
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			code, _ := call(t, s, "agentd:h3", "POST", "claim", body)
			codes <- code
		}()
	}
	for range 2 {
		if code := <-codes; code != http.StatusOK && code != http.StatusNoContent {
			t.Fatalf("a first claim at the same time: %d", code)
		}
	}
	var events int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'host_registered' AND body_json->>'host_id' = 'h3'`).Scan(&events))
	if events != 1 {
		t.Fatalf("%d registrations of h3; want one", events)
	}
}
