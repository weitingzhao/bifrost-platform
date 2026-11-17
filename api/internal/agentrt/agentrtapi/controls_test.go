package agentrtapi_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
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
	t.Parallel()
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
		{"admin", "", "owner", "owner", "", []string{"read", "propose", "upload", "stop", "drain", "lift", "measure", "signoff", "assess"}},
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

// The plan: the roots, a subtree depth first with each step's state, the
// event it came from and its tasks, and one node in full.
func TestPlanReads(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
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
			// The tasks' table is not read any more: the counts are there, empty.
			counts, _ := m["task_counts"].(map[string]any)
			if m["state"] != "planned" || ev["event_type"] != "plan_node_created" || counts == nil || len(counts) != 0 {
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
	if len(tasks) != 0 || len(node["events"].([]any)) == 0 {
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
	t.Parallel()
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
	t.Parallel()
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

// Review 177 A1: a stop the Owner lifted, asked for again with a new key,
// is a new stop in force; a retry with the old key is answered with the old
// (lifted) one.
func TestStopAgainAfterLift(t *testing.T) {
	t.Parallel()
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

// Review 176 part 2 B2: the tree is depth first across siblings, and a step
// whose state changed shows that change as its source.
func TestPlanTreeOrder(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	for _, n := range []planstore.PlanNode{
		{ID: "R2:2", Parent: "R2", Kind: "item", Label: "2", Title: "second item", Ordinal: 1, Root: "R2"},
		{ID: "R2:1-2", Parent: "R2:1", Kind: "step", Label: "1-2", Title: "second step", MissionID: "M-1", StepKind: "agent", Ordinal: 1, Root: "R2"},
		{ID: "R2:2-1", Parent: "R2:2", Kind: "step", Label: "2-1", Title: "a step", MissionID: "M-1", StepKind: "agent", Root: "R2"},
	} {
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { _, err := planstore.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", "R2", "R2:1-2", "active", "test")
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
	t.Parallel()
	pool, _ := world(t)
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
	if len(tasks) != 0 { // a mission's tasks are not read any more
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
