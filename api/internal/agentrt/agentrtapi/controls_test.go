package agentrtapi_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func mayList(out map[string]any) []string {
	var l []string
	for _, v := range out["may"].([]any) {
		l = append(l, v.(string))
	}
	return l
}

// whoami: who the caller is, who its writes are recorded as, what it may do.
func TestWhoami(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	for _, c := range []struct {
		who, session, actor, kind, problem string
		may                                []string
	}{
		{"reporter", "reporter-s1", "thread:reporter-s1", "thread (registered on its first write)", "", []string{"read", "stop", "external_work"}},
		{"operator", "s1", "thread:s1", "thread (registered on its first write)", "", []string{"read", "propose", "upload", "stop", "drain"}},
		{"operator", "", "", "", "X-Bifrost-Session", []string{"read", "propose", "upload", "stop", "drain"}},
		{"agentd:h1", "", "host:h1", "host", "", []string{"read", "claim", "upload", "stop"}},
		{"admin", "", "owner", "owner", "", []string{"read", "propose", "upload", "stop", "drain", "lift", "measure", "signoff"}},
		{"seat:chief_of_staff", "", "seat:chief_of_staff", "seat", "", []string{"read", "propose", "upload", "stop"}},
		{"seat:ghost", "", "seat:ghost", "", "not registered", []string{"read", "propose", "upload", "stop"}},
		{"viewer", "s9", "thread:s9", "thread (registered on its first write)", "", []string{"read", "stop"}},
	} {
		var header []string
		if c.session != "" {
			header = []string{session, c.session}
		}
		code, out := call(t, s, c.who, "GET", "whoami", nil, header...)
		problem, _ := out["actor_error"].(string)
		if code != http.StatusOK || out["actor"] != nilIfEmpty(c.actor) || out["actor_kind"] != nilIfEmpty(c.kind) ||
			(c.problem == "") != (problem == "") || !strings.Contains(problem, c.problem) || !slices.Equal(mayList(out), c.may) {
			t.Errorf("%s/%s: %d %v", c.who, c.session, code, out)
		}
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Controls: anyone sets, the Owner lifts; a pause stops claims and its lift
// lets them go on; a drain is a control on the host; bad controls write
// nothing.
func TestControlsAndHosts(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	claimBody := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}}

	code, hosts := call(t, s, "viewer", "GET", "hosts", nil)
	if h1 := hostOf(t, hosts, "h1"); code != http.StatusOK || h1["online"] != false || h1["poll_age_s"] != nil {
		t.Fatalf("a host that never polled: %d %v", code, hosts)
	}
	pause := map[string]any{"idempotency_key": "p1", "scope": "global", "kind": "pause", "reason": "test"}
	code, set := call(t, s, "viewer", "POST", "controls", pause, session, "s1")
	if code != http.StatusOK || set["kind"] != "pause" {
		t.Fatalf("a viewer pauses: %d %v", code, set)
	}
	if got, again := call(t, s, "viewer", "POST", "controls", pause, session, "s1"); got != http.StatusOK || again["control_id"] != set["control_id"] {
		t.Fatalf("a retry: %d %v", got, again)
	}
	var by string
	must(t, pool.QueryRow(ctx, `SELECT set_by_actor_id FROM agentrt.control WHERE control_id = $1`, set["control_id"]).Scan(&by))
	if by != "thread:s1" {
		t.Fatalf("set by %s", by)
	}
	if got, _ := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusNoContent {
		t.Fatalf("a claim under a global pause: %d", got)
	}
	id := set["control_id"].(string)
	for _, who := range []string{"operator", "viewer", "agentd:h1"} {
		if got, _ := call(t, s, who, "DELETE", "controls/"+id, nil, session, "s1"); got != http.StatusUnauthorized && got != http.StatusForbidden {
			t.Fatalf("%s lifted a control: %d", who, got)
		}
	}
	code, listed := call(t, s, "viewer", "GET", "controls", nil)
	if code != http.StatusOK || len(listed["controls"].([]any)) != 1 {
		t.Fatalf("controls in force: %d %v", code, listed)
	}
	if got, out := call(t, s, "admin", "DELETE", "controls/"+id, nil); got != http.StatusOK || out["cleared"] != true || out["cleared_by"] != "owner" {
		t.Fatalf("the Owner lifts: %d %v", got, out)
	}
	if got, out := call(t, s, "admin", "DELETE", "controls/"+id, nil); got != http.StatusOK || out["cleared"] != false {
		t.Fatalf("lifted twice: %d %v", got, out)
	}
	if got, _ := call(t, s, "admin", "DELETE", "controls/c_nope", nil); got != http.StatusNotFound {
		t.Fatalf("a control there is not: %d", got)
	}
	if got, out := call(t, s, "viewer", "GET", "controls?all=1", nil); got != http.StatusOK || len(out["controls"].([]any)) != 1 {
		t.Fatalf("every control: %d %v", got, out)
	} else if got, out := call(t, s, "viewer", "GET", "controls", nil); got != http.StatusOK || len(out["controls"].([]any)) != 0 {
		t.Fatalf("controls in force after the lift: %d %v", got, out)
	}

	// A drain: only operators and the Owner; the host takes no new work. The
	// operator writes as a thread of its own (s1 is the viewer's, TD-370).
	drain := map[string]any{"idempotency_key": "d1", "reason": "maintenance"}
	if got, _ := call(t, s, "viewer", "POST", "hosts/h1/drain", drain, session, "s1"); got != http.StatusUnauthorized && got != http.StatusForbidden {
		t.Fatalf("a viewer drained: %d", got)
	}
	if got, out := call(t, s, "operator", "POST", "hosts/nope/drain", drain, session, "s2"); got != http.StatusUnprocessableEntity {
		t.Fatalf("a host there is not: %d %v", got, out)
	}
	code, drained := call(t, s, "operator", "POST", "hosts/h1/drain", drain, session, "s2")
	if code != http.StatusOK || drained["kind"] != "drain" || drained["scope_ref"] != "h1" {
		t.Fatalf("drain: %d %v", code, drained)
	}
	if got, _ := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusNoContent {
		t.Fatalf("a drained host claimed: %d", got)
	}
	_, hosts = call(t, s, "viewer", "GET", "hosts", nil)
	h1 := hostOf(t, hosts, "h1")
	if h1["online"] != true || len(h1["controls"].([]any)) != 1 {
		t.Fatalf("h1 after a poll and a drain: %v", h1)
	}
	if got, _ := call(t, s, "admin", "DELETE", "controls/"+drained["control_id"].(string), nil); got != http.StatusOK {
		t.Fatalf("lift the drain: %d", got)
	}
	if got, claim := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusOK || claim["task_id"] != task {
		t.Fatalf("a claim after the lift: %d %v", got, claim)
	}
	_, hosts = call(t, s, "viewer", "GET", "hosts", nil)
	if h1 = hostOf(t, hosts, "h1"); h1["live_attempts"] != float64(1) {
		t.Fatalf("h1 with its attempt: %v", h1)
	}

	// An agentd stops its own work, recorded as its host.
	code, own := call(t, s, "agentd:h1", "POST", "controls", map[string]any{"idempotency_key": "s1", "scope": "task", "scope_ref": task,
		"kind": "stop", "reason": "the executor misbehaves"})
	if code != http.StatusOK {
		t.Fatalf("agentd stops: %d %v", code, own)
	}
	must(t, pool.QueryRow(ctx, `SELECT set_by_actor_id FROM agentrt.control WHERE control_id = $1`, own["control_id"]).Scan(&by))
	if by != "host:h1" {
		t.Fatalf("set by %s", by)
	}

	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.control`).Scan(&before))
	for name, body := range map[string]map[string]any{
		"no key":         {"scope": "global", "kind": "stop", "reason": "x"},
		"no reason":      {"idempotency_key": "b1", "scope": "global", "kind": "stop"},
		"bad scope":      {"idempotency_key": "b2", "scope": "planet", "kind": "stop", "reason": "x"},
		"drain a task":   {"idempotency_key": "b3", "scope": "task", "scope_ref": task, "kind": "drain", "reason": "x"},
		"global ref":     {"idempotency_key": "b4", "scope": "global", "scope_ref": "x", "kind": "stop", "reason": "x"},
		"no ref":         {"idempotency_key": "b5", "scope": "mission", "kind": "stop", "reason": "x"},
		"missing task":   {"idempotency_key": "b6", "scope": "task", "scope_ref": "t_nope", "kind": "stop", "reason": "x"},
		"missing host":   {"idempotency_key": "b7", "scope": "host", "scope_ref": "h9", "kind": "pause", "reason": "x"},
		"missing schema": {"idempotency_key": "b8", "scope": "mission", "scope_ref": "M-9", "kind": "pause", "reason": "x"},
	} {
		if got, out := call(t, s, "operator", "POST", "controls", body, session, "s2"); got != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %v", name, got, out)
		}
	}
	var after int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.control`).Scan(&after))
	if after != before {
		t.Fatalf("refused controls wrote %d", after-before)
	}
}

func hostOf(t *testing.T, hosts map[string]any, id string) map[string]any {
	t.Helper()
	for _, h := range hosts["hosts"].([]any) {
		if m := h.(map[string]any); m["host_id"] == id {
			return m
		}
	}
	t.Fatalf("no host %s in %v", id, hosts)
	return nil
}

// The plan: the roots, a subtree depth first with each step's state, the
// event it came from and its tasks, and one node in full.
func TestPlanReads(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	code, roots := call(t, s, "viewer", "GET", "plan", nil)
	if code != http.StatusOK || len(roots["roots"].([]any)) != 1 || roots["roots"].([]any)[0].(map[string]any)["plan_node_id"] != "R2" {
		t.Fatalf("roots: %d %v", code, roots)
	}
	code, tree := call(t, s, "viewer", "GET", "plan?root=R2", nil)
	if code != http.StatusOK {
		t.Fatalf("tree: %d %v", code, tree)
	}
	var order []string
	for _, n := range tree["nodes"].([]any) {
		m := n.(map[string]any)
		order = append(order, fmt.Sprint(m["plan_node_id"], "@", m["depth"]))
		if m["plan_node_id"] == "R2:1-1" {
			ev, _ := m["state_event"].(map[string]any)
			counts, _ := m["task_counts"].(map[string]any)
			if m["state"] != "planned" || ev["event_type"] != "plan_node_created" || counts["ready"] != float64(1) {
				t.Errorf("the step: %v", m)
			}
		}
	}
	if fmt.Sprint(order) != "[R2@0 R2:1@1 R2:1-1@2]" {
		t.Fatalf("order %v", order)
	}
	if got, _ := call(t, s, "viewer", "GET", "plan?root=R9", nil); got != http.StatusNotFound {
		t.Fatalf("a root there is not: %d", got)
	}
	code, node := call(t, s, "viewer", "GET", "plan/R2:1-1", nil)
	if code != http.StatusOK {
		t.Fatalf("node: %d %v", code, node)
	}
	tasks := node["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["task_id"] != task || len(node["events"].([]any)) == 0 {
		t.Fatalf("node: %v", node)
	}
	code, item := call(t, s, "viewer", "GET", "plan/R2:1", nil)
	if children := item["children"].([]any); code != http.StatusOK || len(children) != 1 || children[0].(map[string]any)["plan_node_id"] != "R2:1-1" {
		t.Fatalf("item: %d %v", code, item)
	}
	if got, _ := call(t, s, "viewer", "GET", "plan/R9", nil); got != http.StatusNotFound {
		t.Fatalf("a node there is not: %d", got)
	}
}

// legacy writes rows with the import event, which may write any table: the
// fixture for actors and times the routes do not make.
func legacy(t *testing.T, pool *pgxpool.Pool, stmts ...string) {
	t.Helper()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

// Review 176 part 2 B1: whoami says what a write would find, and writes
// nothing at all (review 177 B2: no registration either); an upload then
// finds the same.
func TestWhoamiTellsWhyNot(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	legacy(t, pool, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name, retired_at) VALUES ('thread:old', 'thread', 'old', now())`,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:odd', 'seat', 'odd')`)
	footprint := func() string {
		var fp string
		must(t, pool.QueryRow(ctx, `SELECT concat_ws(' ', (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event),
		  (SELECT string_agg(event_stream_id || '=' || last_seq, ',' ORDER BY event_stream_id) FROM agentrt.event_stream),
		  (SELECT count(*) FROM agentrt.outbox), (SELECT count(*) FROM agentrt.idempotency_key))`).Scan(&fp))
		return fp
	}
	cases := map[string]string{"old": "retired", "odd": "is a seat, not a thread", "fresh": ""}
	for sess, want := range cases {
		before := footprint()
		_, out := call(t, s, "operator", "GET", "whoami", nil, session, sess)
		if after := footprint(); after != before {
			t.Fatalf("whoami for %s wrote: %s -> %s", sess, before, after)
		}
		problem, _ := out["actor_error"].(string)
		if (want == "") != (problem == "") || !strings.Contains(problem, want) {
			t.Errorf("%s: %v", sess, out)
		}
		if want == "" && out["actor_kind"] != "thread (registered on its first write)" {
			t.Errorf("%s: %v", sess, out)
		}
	}
	var fresh int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'thread:fresh'`).Scan(&fresh))
	if fresh != 0 {
		t.Fatal("whoami registered a thread")
	}
	for sess, want := range cases {
		body := []byte("# " + sess + "\n")
		code, _ := call(t, s, "operator", "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", "report", session, sess)
		if (want == "") != (code == http.StatusOK) {
			t.Errorf("%s: whoami said %q, an upload answered %d", sess, want, code)
		}
	}
}

// Review 176 B3: an upload of a body already stored still checks its
// writer, and registers nothing.
func TestDuplicateUploadChecksWriter(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	legacy(t, pool, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name, retired_at) VALUES ('thread:old', 'thread', 'old', now())`)
	body := []byte("# stored\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", "report", session, "s1"); code != http.StatusOK {
		t.Fatalf("first upload: %d %v", code, out)
	}
	var before, after int
	count := `SELECT (SELECT count(*) FROM agentrt.event) + (SELECT count(*) FROM agentrt.actor)`
	must(t, pool.QueryRow(ctx, count).Scan(&before))
	for _, c := range []struct{ who, session string }{{"seat:ghost", ""}, {"operator", "old"}} {
		if code, out := call(t, s, c.who, "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", "report", session, c.session); code != http.StatusUnprocessableEntity {
			t.Errorf("%s/%s: %d %v", c.who, c.session, code, out)
		}
	}
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", "report", session, "new"); code != http.StatusOK || out["created"] != false {
		t.Fatalf("a new thread uploading a stored body: %d %v", code, out)
	}
	must(t, pool.QueryRow(ctx, count).Scan(&after))
	if after != before {
		t.Fatalf("uploads of a stored body wrote %d events or actors", after-before)
	}
}

// Review 176 part 2 B2, narrowed by review 177 B1: what each control does
// to a claim (a host under it, or any host for the wider scopes, takes no
// new work; another host still does) and to the heartbeat of a started
// attempt (only a stop tells it to stop; agentd ending the work on that
// signal is agentd's test, TestStopAndHardLimit).
func TestControlMatrix(t *testing.T) {
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claimOf := func(host string) map[string]any {
		return map[string]any{"host_id": host, "free_slots": 1, "tools": []any{cursor}}
	}
	for _, c := range []struct {
		scope, kind string
		stopsWork   bool
	}{
		{"global", "stop", true}, {"global", "pause", false},
		{"mission", "stop", true}, {"mission", "pause", false},
		{"task", "stop", true}, {"task", "pause", false},
		{"host", "stop", true}, {"host", "pause", false}, {"host", "drain", false},
	} {
		t.Run(c.scope+"-"+c.kind, func(t *testing.T) {
			pool, task := world(t)
			s := server(t, pool)
			for _, h := range []string{"h2", "h3"} {
				must(t, store.RegisterHost(ctx, pool, "owner", h, "dev"))
			}
			code, claim := call(t, s, "agentd:h1", "POST", "claim", claimOf("h1"))
			if code != http.StatusOK {
				t.Fatalf("claim: %d %v", code, claim)
			}
			attempt := claim["attempt_id"].(string)
			fence := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"], "model_provider": "cursor-mix"}
			if got, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/start", fence); got != http.StatusOK {
				t.Fatalf("start: %d %v", got, out)
			}
			second := readyTask(t, pool)
			set := func(key, ref string) {
				t.Helper()
				if got, out := call(t, s, "operator", "POST", "controls", map[string]any{"idempotency_key": key, "scope": c.scope,
					"scope_ref": ref, "kind": c.kind, "reason": "test"}, session, "s1"); got != http.StatusOK {
					t.Fatalf("control on %s: %d %v", ref, got, out)
				}
			}
			// Under the control: the running attempt's own scope, and the
			// one that would hand out the second task.
			switch c.scope {
			case "global":
				set("k1", "")
			case "mission":
				set("k1", "M-1")
			case "task":
				set("k1", task)
				set("k2", second)
			case "host":
				set("k1", "h1")
				set("k2", "h2")
			}
			if got, _ := call(t, s, "agentd:h2", "POST", "claim", claimOf("h2")); got != http.StatusNoContent {
				t.Errorf("a claim under a %s %s: %d", c.scope, c.kind, got)
			}
			if c.scope == "host" {
				if got, out := call(t, s, "agentd:h3", "POST", "claim", claimOf("h3")); got != http.StatusOK || out["task_id"] != second {
					t.Errorf("a host the %s does not name: %d %v", c.kind, got, out)
				}
			}
			got, beat := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/heartbeat",
				map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]})
			if got != http.StatusOK || beat["stop"] != c.stopsWork {
				t.Errorf("the started attempt's heartbeat under a %s %s: %d %v", c.scope, c.kind, got, beat)
			}
		})
	}
}

// Review 177 A1: a stop the Owner lifted, asked for again with a new key,
// is a new stop in force; a retry with the old key is answered with the old
// (lifted) one.
func TestStopAgainAfterLift(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	stop := map[string]any{"idempotency_key": "stop-1", "scope": "global", "kind": "stop", "reason": "look"}
	_, first := call(t, s, "operator", "POST", "controls", stop, session, "s1")
	if code, _ := call(t, s, "admin", "DELETE", "controls/"+first["control_id"].(string), nil); code != http.StatusOK {
		t.Fatalf("lift: %d", code)
	}
	if _, again := call(t, s, "operator", "POST", "controls", stop, session, "s1"); again["control_id"] != first["control_id"] {
		t.Fatalf("a retry of the first stop: %v", again)
	}
	stop["idempotency_key"] = "stop-2"
	_, second := call(t, s, "operator", "POST", "controls", stop, session, "s1")
	if second["control_id"] == first["control_id"] {
		t.Fatalf("a new stop answered with the old: %v", second)
	}
	_, inForce := call(t, s, "viewer", "GET", "controls", nil)
	if list := inForce["controls"].([]any); len(list) != 1 || list[0].(map[string]any)["control_id"] != second["control_id"] {
		t.Fatalf("in force: %v", inForce)
	}
}

// readyTask proposes another ready task of M-1 at step R2:1-1.
func readyTask(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := store.NewTaskID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# another\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "another", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	}))
	return id
}

// Review 176 part 2 B2: online is a poll within store.HostLostAfter (30
// seconds since W-31 3.5); a same key with another control is a conflict; a
// lift at the same time is recorded once.
func TestHostsAndControlEdges(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	must(t, store.RegisterHost(ctx, pool, "owner", "h2", "dev"))
	legacy(t, pool, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '29 seconds' WHERE host_id = 'h1'`,
		`UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '31 seconds' WHERE host_id = 'h2'`)
	_, hosts := call(t, s, "viewer", "GET", "hosts", nil)
	if h1, h2 := hostOf(t, hosts, "h1"), hostOf(t, hosts, "h2"); h1["online"] != true || h2["online"] != false ||
		h1["reported_offline"] != false || h2["reported_offline"] != true {
		t.Fatalf("29 and 31 seconds since the last poll: %v %v", h1, h2)
	}
	// An API whose runtime routes became ready just now (review 528 A1b): h2,
	// 31 s silent, is not lost until it stays so 30 s more, on GET /hosts and
	// on GET /progress alike.
	back := time.Now()
	fresh := server(t, pool, func(a *agentrtapi.API) { a.Back = func() time.Time { return back } })
	_, hosts = call(t, fresh, "viewer", "GET", "hosts", nil)
	if h2 := hostOf(t, hosts, "h2"); h2["online"] != false || h2["reported_offline"] != false || h2["claiming"] != nil {
		t.Fatalf("h2 31 s silent, the API just ready: %v", h2)
	}
	_, prog := call(t, fresh, "viewer", "GET", "progress", nil)
	for _, h := range prog["hosts"].([]any) {
		if h := h.(map[string]any); h["host_id"] == "h2" && h["reported_offline"] != false {
			t.Fatalf("progress, the API just ready: %v", h)
		}
	}
	body := map[string]any{"idempotency_key": "k", "scope": "global", "kind": "pause", "reason": "one"}
	_, set := call(t, s, "viewer", "POST", "controls", body, session, "s1")
	body["reason"] = "two"
	if code, out := call(t, s, "viewer", "POST", "controls", body, session, "s1"); code != http.StatusConflict || out["error"] != "key_conflict" {
		t.Fatalf("a key with another control: %d %v", code, out)
	}
	id := set["control_id"].(string)
	lifts := make(chan int, 4)
	for range 4 {
		go func() {
			code, _ := call(t, s, "admin", "DELETE", "controls/"+id, nil)
			lifts <- code
		}()
	}
	for range 4 {
		if code := <-lifts; code != http.StatusOK {
			t.Errorf("a lift: %d", code)
		}
	}
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'control_cleared' AND body_json->>'control_id' = $1`, id).Scan(&n))
	if n != 1 {
		t.Fatalf("%d control_cleared for four lifts at once", n)
	}
}

// Review 176 part 2 B2: the tree is depth first across siblings, and a step
// whose state changed shows that change as its source.
func TestPlanTreeOrder(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	for _, n := range []store.PlanNode{
		{ID: "R2:2", Parent: "R2", Kind: "item", Label: "2", Title: "second item", Ordinal: 1, Root: "R2"},
		{ID: "R2:1-2", Parent: "R2:1", Kind: "step", Label: "1-2", Title: "second step", MissionID: "M-1", StepKind: "agent", Ordinal: 1, Root: "R2"},
		{ID: "R2:2-1", Parent: "R2:2", Kind: "step", Label: "2-1", Title: "a step", MissionID: "M-1", StepKind: "agent", Root: "R2"},
	} {
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", "R2", "R2:1-2", "active", "test")
		return err
	}))
	_, tree := call(t, s, "viewer", "GET", "plan?root=R2", nil)
	var order []string
	for _, n := range tree["nodes"].([]any) {
		m := n.(map[string]any)
		order = append(order, m["plan_node_id"].(string))
		if m["plan_node_id"] == "R2:1-2" {
			if ev, _ := m["state_event"].(map[string]any); ev["event_type"] != "plan_node_state_changed" || m["state"] != "active" {
				t.Errorf("a step that changed: %v", m)
			}
		}
	}
	if strings.Join(order, " ") != "R2 R2:1 R2:1-1 R2:1-2 R2:2 R2:2-1" {
		t.Fatalf("order %v", order)
	}
}

// GET /progress: any role reads the projection, computed now; no token
// reads nothing; a read that fails is 503 and nothing else (review 181 B1).
func TestProgressRoute(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	if code, _ := call(t, s, "", "GET", "progress", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	code, out := call(t, s, "viewer", "GET", "progress", nil)
	if code != http.StatusOK || out["generated_at"] == nil || out["as_of_event_id"] == nil {
		t.Fatalf("progress: %d %v", code, out)
	}
	work, _ := out["work"].(map[string]any)
	missions, _ := work["missions"].([]any)
	if len(missions) != 1 || missions[0].(map[string]any)["mission_id"] != "M-1" {
		t.Fatalf("work: %v", out["work"])
	}
	tasks := missions[0].(map[string]any)["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["task_id"] != task || tasks[0].(map[string]any)["status"] != "queued" {
		t.Fatalf("tasks: %v", tasks)
	}
	db := agentrttest.Migrated(t)
	closed := db.Pool(t, db.Runtime)
	closed.Close()
	down := server(t, closed)
	code, out = call(t, down, "viewer", "GET", "progress", nil)
	if code != http.StatusServiceUnavailable || out["error"] != "unavailable" || out["work"] != nil {
		t.Fatalf("a read that fails: %d %v", code, out)
	}
}
