package agentrtapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
)

// The collector principal of R5:5.5-12 §1 (S1-1): its actor, its reserved
// name, its own records, and a hook's and an MCP's writes for one session.

// CollectorDerivation (§1.5): every principal that is not a collector
// derives its writer as today, whatever header it sends (a header
// collector@h1 is refused by ThreadActor: "@" is no session character); a
// collector is thread:collector@<host> with no header or any header; a name
// in the namespace that is not a valid collector derives nothing.
func TestCollectorDerivation(t *testing.T) {
	reporter := actuation.Principal{Name: "reporter", Role: actuation.RoleReporter}
	operator := actuation.Principal{Name: "operator", Role: actuation.RoleOperator}
	c1 := actuation.Principal{Name: "collector:h1", Role: actuation.RoleReporter}
	c2 := actuation.Principal{Name: "collector:h2", Role: actuation.RoleReporter}
	for _, c := range []struct {
		p             actuation.Principal
		header        string
		writer, actor string // "" refused
	}{
		{reporter, "collector@h1", "", ""},
		{reporter, "collector-h1", "thread:collector-h1", "thread:collector-h1"},
		{reporter, "collector:h1", "thread:collector:h1", "thread:collector:h1"},
		{reporter, "thread:collector@h1", "", ""},
		{reporter, "", "", ""},
		{operator, "collector@h1", "", ""},
		{operator, "s-1", "thread:s-1", "thread:s-1"},
		{actuation.Principal{Name: "admin", Role: actuation.RoleAdmin}, "collector@h1", "owner", "owner"},
		{actuation.Principal{Name: "seat:chief_of_staff", Role: actuation.RoleReporter}, "collector@h1", "seat:chief_of_staff", "seat:chief_of_staff"},
		{actuation.Principal{Name: "agentd:h1", Role: actuation.RoleAgentd}, "collector@h1", "", "host:h1"},
		{actuation.Principal{Name: "agentd:h1", Role: actuation.RoleAgentd}, "s-1", "thread:s-1", "host:h1"},
		{c1, "", "thread:collector@h1", "thread:collector@h1"},
		{c1, "s-1", "thread:collector@h1", "thread:collector@h1"},
		{c1, "collector@h2", "thread:collector@h1", "thread:collector@h1"},
		{c1, "local_0b8e2f4a", "thread:collector@h1", "thread:collector@h1"},
		{c2, "collector@h1", "thread:collector@h2", "thread:collector@h2"},
		// No configuration admits these (actuation.collectorsValid); they
		// derive nothing all the same.
		{actuation.Principal{Name: "collector:h1", Role: actuation.RoleOperator}, "s-1", "", ""},
		{actuation.Principal{Name: "collector:H1", Role: actuation.RoleReporter}, "s-1", "", ""},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		if c.header != "" {
			r.Header.Set(agentrtapi.SessionHeader, c.header)
		}
		r = r.WithContext(actuation.WithPrincipal(r.Context(), c.p))
		w, a, _, werr, aerr, _ := agentrtapi.WritersForTest(r)
		if (c.writer == "") != (werr != nil) || w.Actor != c.writer && werr == nil || (c.actor == "") != (aerr != nil) || a.Actor != c.actor && aerr == nil {
			t.Errorf("%+v with %q: writer %q %v, actor %q %v; want %q, %q", c.p, c.header, w.Actor, werr, a.Actor, aerr, c.writer, c.actor)
		}
		if werr == nil && w.Token != c.p.Name {
			t.Errorf("%+v: the write is held to token %q", c.p, w.Token)
		}
	}
}

// collectorSpec is an external work description; task tells one uploader's
// bytes from another's (an artifact is its first uploader's).
func collectorSpec(task string) []byte {
	return []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"` + task + `","executor":"claude","provider":"anthropic","repo":"platform","branch":"test","acceptance_refs":["PV1"],"estimate_minutes":[1,2]}`)
}

// registerAs uploads the description of task and registers its external
// work as caller, sending sess as its session header; it gives the work's id
// and the description's.
func registerAs(t *testing.T, s *httptest.Server, caller, sess, task, run string) (string, string) {
	t.Helper()
	spec := collectorSpec(task)
	if code, out := call(t, s, caller, "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other", session, sess); code != http.StatusOK {
		t.Fatalf("%s uploads %s: %d %v", caller, task, code, out)
	}
	reg := map[string]any{"idempotency_key": "reg-" + run, "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": run, "artifact_id": artifactID(spec)}
	code, out := call(t, s, caller, "POST", "external-work", reg, session, sess)
	if code != http.StatusOK {
		t.Fatalf("%s registers %s: %d %v", caller, run, code, out)
	}
	return out["external_work_id"].(string), artifactID(spec)
}

func registrant(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var who string
	must(t, pool.QueryRow(ctx, `SELECT body_json->'external_work'->>'registrant' FROM agentrt.event
	  WHERE event_stream_id = $1 ORDER BY seq LIMIT 1`, "external-work:"+id).Scan(&who))
	return who
}

// heldTo is the token a thread's registration holds it to (TD-370), ""
// when the thread is not registered.
func heldTo(t *testing.T, pool *pgxpool.Pool, actor string) string {
	t.Helper()
	var tokens []string
	rows, err := pool.Query(ctx, `SELECT COALESCE(body_json->>'token', '') FROM agentrt.event
	  WHERE event_stream_id = 'system' AND event_type = 'actor_registered' AND body_json->'actors' ? $1 ORDER BY event_id`, actor)
	must(t, err)
	for rows.Next() {
		var tok string
		must(t, rows.Scan(&tok))
		tokens = append(tokens, tok)
	}
	must(t, rows.Err())
	switch len(tokens) {
	case 0:
		return ""
	case 1:
		return tokens[0]
	}
	t.Fatalf("%s is registered %d times: %v", actor, len(tokens), tokens)
	return ""
}

// whoami says a collector is its host's collector, bound to the host, and
// may do what the route gate lets it do and nothing more.
func TestCollectorWhoami(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	code, out := call(t, s, "collector:h1", "GET", "whoami", nil, session, "local_0b8e2f4a")
	if code != http.StatusOK || out["name"] != "collector:h1" || out["role"] != "reporter" || out["host_id"] != "h1" ||
		out["actor"] != "thread:collector@h1" || out["actor_error"] != nil ||
		!reflect.DeepEqual(out["may"], []any{"external_work", "read_own", "heartbeat"}) {
		t.Fatalf("a collector's whoami: %d %v", code, out)
	}
	registerAs(t, s, "collector:h1", "", "whoami", "whoami-run")
	if code, out = call(t, s, "collector:h1", "GET", "whoami", nil); code != http.StatusOK || out["actor_kind"] != "thread" {
		t.Fatalf("after its first write: %d %v", code, out)
	}
	if code, out = call(t, s, "reporter", "GET", "whoami", nil, session, "collector@h1"); code != http.StatusOK || out["actor_error"] == nil || out["actor"] != nil {
		t.Fatalf("a reporter naming the collector: %d %v", code, out)
	}
}

// CollectorNameReserved (§1.5): only the token named collector:h1 writes as
// thread:collector@h1. A token naming it in its header is refused before
// anything is written, it registers nothing, and the collector's first
// write holds the actor to its own token; another host's collector is
// another actor.
func TestCollectorNameReserved(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	spec := collectorSpec("reserved")
	reg := map[string]any{"idempotency_key": "reg", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "run", "artifact_id": artifactID(spec)}
	for _, caller := range []string{"reporter", "operator"} {
		for _, h := range []string{"collector@h1", "thread:collector@h1"} {
			if code, out := call(t, s, caller, "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other", session, h); code != http.StatusUnprocessableEntity {
				t.Fatalf("%s uploading as %s: %d %v", caller, h, code, out)
			}
			if code, out := call(t, s, caller, "POST", "external-work", reg, session, h); code != http.StatusUnprocessableEntity {
				t.Fatalf("%s registering as %s: %d %v", caller, h, code, out)
			}
		}
	}
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.actor WHERE actor_id LIKE 'thread:collector@%'`).Scan(&n))
	if n != 0 {
		t.Fatalf("a refused write registered a collector actor (%d)", n)
	}
	w1, _ := registerAs(t, s, "collector:h1", "collector@h2", "reserved h1", "run-h1")
	w2, _ := registerAs(t, s, "collector:h2", "collector@h1", "reserved h2", "run-h2")
	if got := registrant(t, pool, w1); got != "thread:collector@h1" {
		t.Fatalf("h1's work is registered by %s", got)
	}
	if got := registrant(t, pool, w2); got != "thread:collector@h2" {
		t.Fatalf("h2's work is registered by %s", got)
	}
	if got := heldTo(t, pool, "thread:collector@h1"); got != "collector:h1" {
		t.Fatalf("thread:collector@h1 is held to %q", got)
	}
	if got := heldTo(t, pool, "thread:collector@h2"); got != "collector:h2" {
		t.Fatalf("thread:collector@h2 is held to %q", got)
	}
	// Still no one else: the refusals did not change after the collector
	// registered its actor.
	if code, out := call(t, s, "reporter", "POST", "external-work", reg, session, "collector@h1"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a reporter as collector@h1 after the collector wrote: %d %v", code, out)
	}
}

// CollectorOwnRecordsOnly (§1.5, §1.3 记录级归属): a collector reads its own
// work and the descriptions it uploaded first; another host's collector's,
// an MCP registrant's and records that do not exist are 403 alike, with one
// answer, so a refusal says nothing of whose a record is or whether it is
// there. A description another uploader put first stays unread even after
// the collector uploads the same bytes. Other readers read as before.
func TestCollectorOwnRecordsOnly(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	w1, d1 := registerAs(t, s, "collector:h1", "", "h1 work", "run-h1")
	w2, d2 := registerAs(t, s, "collector:h2", "", "h2 work", "run-h2")
	w3, d3 := registerAs(t, s, "reporter", "local_mcp", "mcp work", "run-mcp")
	missingWork := "ew_" + fmt.Sprintf("%064x", 7)
	missingSpec := artifactID([]byte("no such description"))

	var refusal map[string]any
	for _, path := range []string{"external-work/" + w2, "external-work/" + w3, "external-work/" + missingWork,
		"artifacts/" + d2, "artifacts/" + d3, "artifacts/" + missingSpec} {
		code, out := call(t, s, "collector:h1", "GET", path, nil)
		if code != http.StatusForbidden || out["error"] != "not_yours" {
			t.Fatalf("h1 reads %s: %d %v", path, code, out)
		}
		if refusal == nil {
			refusal = out
		} else if !reflect.DeepEqual(out, refusal) {
			t.Fatalf("h1's refusals differ, telling records apart: %v and %v", refusal, out)
		}
	}
	// Its own (the positive control).
	if code, out := call(t, s, "collector:h1", "GET", "external-work/"+w1, nil); code != http.StatusOK || out["external_work_id"] != w1 {
		t.Fatalf("h1 reads its own work: %d %v", code, out)
	}
	if code, out := call(t, s, "collector:h1", "GET", "artifacts/"+d1, nil); code != http.StatusOK || out["task"] != "h1 work" {
		t.Fatalf("h1 reads its own description: %d %v", code, out)
	}
	// h2 uploads h1's bytes: the upload is harmless, the read is still h1's.
	spec := collectorSpec("h1 work")
	if code, out := call(t, s, "collector:h2", "PUT", "artifacts/"+d1, spec, "X-Artifact-Kind", "other"); code != http.StatusOK || out["created"] != false {
		t.Fatalf("h2 uploads h1's bytes: %d %v", code, out)
	}
	if code, out := call(t, s, "collector:h2", "GET", "artifacts/"+d1, nil); code != http.StatusForbidden || !reflect.DeepEqual(out, refusal) {
		t.Fatalf("h2 reads the description h1 put first: %d %v", code, out)
	}
	// It uploads descriptions only (§1.3: kind other, the closed schema).
	report := []byte("# a report\n")
	for _, c := range []struct {
		kind string
		body []byte
	}{{"report", collectorSpec("as a report")}, {"task_spec", collectorSpec("as a spec")}, {"other", report}} {
		if code, out := call(t, s, "collector:h1", "PUT", "artifacts/"+artifactID(c.body), c.body, "X-Artifact-Kind", c.kind); code != http.StatusForbidden {
			t.Fatalf("h1 uploads a %s: %d %v", c.kind, code, out)
		}
	}
	// The other readers are as before.
	for _, caller := range []string{"viewer", "reporter", "operator"} {
		for _, path := range []string{"external-work/" + w1, "external-work/" + w2, "external-work/" + w3} {
			if code, out := call(t, s, caller, "GET", path, nil); code != http.StatusOK {
				t.Fatalf("%s reads %s: %d %v", caller, path, code, out)
			}
		}
		if code, out := call(t, s, caller, "GET", "artifacts/"+d2, nil); code != http.StatusOK || out["task"] != "h2 work" {
			t.Fatalf("%s reads h2's description: %d %v", caller, code, out)
		}
		if code, _ := call(t, s, caller, "GET", "external-work/"+missingWork, nil); code != http.StatusNotFound {
			t.Fatalf("%s reads no work: %d", caller, code)
		}
	}
}

// ForeignWork_OtherToken (§1.5): an event on a collector's work from any
// other token is 403 — another host's collector, the MCP's token naming any
// session, an operator — and a collector's event on work not its own
// (another registrant's, or none) is 403 with one answer. The registrants
// go on with their own.
func TestForeignWork_OtherToken(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	w1, _ := registerAs(t, s, "collector:h1", "", "h1 work", "run-h1")
	w3, _ := registerAs(t, s, "reporter", "local_mcp", "mcp work", "run-mcp")
	started := map[string]any{"idempotency_key": "start", "seq": 1, "type": "started"}
	for _, c := range []struct{ caller, sess string }{
		{"collector:h2", ""}, {"collector:h2", "collector@h1"}, {"reporter", "local_mcp"}, {"reporter", "collector-h1"},
		{"operator", "local_mcp"}, {"operator", "x"},
	} {
		if code, out := call(t, s, c.caller, "POST", "external-work/"+w1+"/events", started, session, c.sess); code != http.StatusForbidden || out["error"] != "not_yours" {
			t.Fatalf("%s as %q on h1's work: %d %v", c.caller, c.sess, code, out)
		}
	}
	var refusal map[string]any
	for _, id := range []string{w3, "ew_" + fmt.Sprintf("%064x", 7)} {
		code, out := call(t, s, "collector:h1", "POST", "external-work/"+id+"/events", started, session, "local_mcp")
		if code != http.StatusForbidden || out["error"] != "not_yours" {
			t.Fatalf("h1 on %s: %d %v", id, code, out)
		}
		if refusal == nil {
			refusal = out
		} else if !reflect.DeepEqual(out, refusal) {
			t.Fatalf("h1's refusals differ: %v and %v", refusal, out)
		}
	}
	for _, c := range []struct{ caller, sess, id string }{{"collector:h1", "", w1}, {"reporter", "local_mcp", w3}} {
		if code, out := call(t, s, c.caller, "POST", "external-work/"+c.id+"/events", started, session, c.sess); code != http.StatusOK || out["state"] != "Running" {
			t.Fatalf("%s on its own work: %d %v", c.caller, code, out)
		}
	}
	if code, out := call(t, s, "viewer", "GET", "external-work/"+w1, nil); code != http.StatusOK || out["seq"] != float64(1) {
		t.Fatalf("h1's work after the refusals: %d %v", code, out)
	}
}

// HookAndMcpWriteSameSession, its first half (S1-1; the second, joining
// them by sidebar, is S1-2's): a hook's write through the collector token
// and an MCP's through its own token, both naming one session, in either
// order. Each is recorded as its own actor, the collector's never the
// session's; neither takes the other's: the session's thread stays held to
// the MCP's token, the collector's to the collector's, and both go on.
func TestHookAndMcpWriteSameSession(t *testing.T) {
	const sess = "local_0b8e2f4a"
	for _, order := range [][]string{{"hook", "mcp"}, {"mcp", "hook"}} {
		t.Run(order[0]+" first", func(t *testing.T) {
			pool, _ := world(t)
			s := server(t, pool)
			callers := map[string]string{"hook": "collector:h1", "mcp": "reporter"}
			actors := map[string]string{"hook": "thread:collector@h1", "mcp": "thread:" + sess}
			for round := 1; round <= 2; round++ {
				for i, who := range order {
					id, _ := registerAs(t, s, callers[who], sess, fmt.Sprintf("%s %d", who, round), fmt.Sprintf("%s-run-%d", who, round))
					if got := registrant(t, pool, id); got != actors[who] {
						t.Fatalf("round %d: the %s's work is registered by %s", round, who, got)
					}
					if round == 1 && i == 0 {
						other := order[1]
						if got := heldTo(t, pool, actors[other]); got != "" {
							t.Fatalf("the %s's write registered the %s's actor (held to %q)", who, other, got)
						}
					}
				}
			}
			if got := heldTo(t, pool, actors["hook"]); got != "collector:h1" {
				t.Fatalf("the collector's actor is held to %q", got)
			}
			if got := heldTo(t, pool, actors["mcp"]); got != "reporter" {
				t.Fatalf("the session's actor is held to %q", got)
			}
			for who, caller := range callers {
				code, out := call(t, s, caller, "GET", "whoami", nil, session, sess)
				if code != http.StatusOK || out["actor"] != actors[who] || out["actor_error"] != nil {
					t.Fatalf("the %s's whoami: %d %v", who, code, out)
				}
			}
			var threads []string
			rows, err := pool.Query(ctx, `SELECT actor_id FROM agentrt.actor WHERE actor_kind = 'thread' ORDER BY actor_id`)
			must(t, err)
			for rows.Next() {
				var a string
				must(t, rows.Scan(&a))
				threads = append(threads, a)
			}
			must(t, rows.Err())
			if !slices.Contains(threads, actors["hook"]) || !slices.Contains(threads, actors["mcp"]) || len(threads) != 2 {
				t.Fatalf("thread actors: %v", threads)
			}
		})
	}
}

// A collector's registration names no content it may not read (P-a review
// M2): an `other` artifact someone else put, whatever its bytes, and an
// artifact that does not exist are refused with one answer that carries no
// field name, value or byte of the body. A valid description another
// collector put first, the same bytes the collector holds itself, registers
// (§1.3: it keeps its own copy).
func TestCollectorRegistrationNamesNoForeignContent(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	bodies := [][]byte{[]byte(`{"private-h2-field":123}`), []byte(`{"v":987654321012345678901234567890}`),
		[]byte(`{"kind":"another-kind","secret-key-9d41":"secret-value-9d41"}`),
		[]byte("plain-text-secret-9d41")}
	markers := []string{"private-h2-field", "987654321012345678901234567890", "secret-key-9d41", "secret-value-9d41", "another-kind",
		"plain-text-secret-9d41", "ExternalDescription", "cannot unmarshal"}
	var refusal map[string]any
	ids := []string{artifactID([]byte("no such artifact"))}
	for _, b := range bodies {
		if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(b), b, "X-Artifact-Kind", "other", session, "local_op"); code != http.StatusOK {
			t.Fatalf("the operator's artifact: %d %v", code, out)
		}
		ids = append(ids, artifactID(b))
	}
	for i, id := range ids {
		reg := map[string]any{"idempotency_key": fmt.Sprintf("reg-%d", i), "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": fmt.Sprintf("run-%d", i), "artifact_id": id}
		code, out := call(t, s, "collector:h1", "POST", "external-work", reg)
		doc := fmt.Sprint(out)
		for _, m := range markers {
			if strings.Contains(doc, m) {
				t.Fatalf("the refusal of %s carries %q: %d %v", id, m, code, out)
			}
		}
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("a registration naming %s: %d %v", id, code, out)
		}
		if refusal == nil {
			refusal = out
		} else if !reflect.DeepEqual(out, refusal) {
			t.Fatalf("the refusals differ, telling artifacts apart: %v and %v", refusal, out)
		}
	}
	// The shared description: h2 puts it first, h1 holds the same bytes.
	spec := collectorSpec("shared work")
	for _, caller := range []string{"collector:h2", "collector:h1"} {
		if code, out := call(t, s, caller, "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other"); code != http.StatusOK {
			t.Fatalf("%s uploads the shared description: %d %v", caller, code, out)
		}
	}
	reg := map[string]any{"idempotency_key": "shared", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "shared-run", "artifact_id": artifactID(spec)}
	code, out := call(t, s, "collector:h1", "POST", "external-work", reg)
	if code != http.StatusOK || out["external_work_id"] == nil {
		t.Fatalf("h1 registers with the description h2 put first: %d %v", code, out)
	}
	if got := registrant(t, pool, out["external_work_id"].(string)); got != "thread:collector@h1" {
		t.Fatalf("the shared registration is %s's", got)
	}
}
