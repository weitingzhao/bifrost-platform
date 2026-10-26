package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The worlds of TestRoutesMoveStateOnlyByEvents and what brings each to its
// point: each write route's valid request, as its client sends it, checked
// like every other request.
var fuzzStages = []string{"base", "running", "submitted", "lost"}

const (
	expectWrites  = "writes"  // its valid request appends events
	expectAnswers = "answers" // its valid request is answered and writes nothing
)

// routeExpect is every write route (an attempt action apart) and what its
// valid request does. Reads that write are listed too (an exam paper is
// recorded when it is handed out).
var routeExpect = map[string]string{
	"POST claim": expectWrites, "PUT artifacts/{id}": expectWrites,
	"POST attempts/{id}/start": expectWrites, "POST attempts/{id}/heartbeat": expectAnswers,
	"POST attempts/{id}/checkpoint": expectWrites, "POST attempts/{id}/computed": expectWrites,
	"POST attempts/{id}/persist": expectWrites, "POST attempts/{id}/submit": expectWrites,
	"POST attempts/{id}/salvage": expectWrites, "POST attempts/{id}/ended": expectWrites,
	"POST missions": expectWrites, "POST missions/{id}/tasks": expectWrites, "POST goals": expectWrites,
	"POST messages": expectWrites, "POST messages/ack": expectWrites,
	"POST controls": expectWrites, "DELETE controls/{id}": expectWrites, "POST hosts/{id}/drain": expectWrites,
	"POST plan/proposals": expectWrites, "POST plan/imports": expectWrites, "POST plan/{id}/notes": expectWrites,
	"POST plan/{id}/signoff": expectWrites,
	"POST external-work":     expectWrites, "POST external-work/{id}/events": expectWrites,
	"POST rules/{rule_id}/check": expectAnswers, "POST rules/{rule_id}/proposals": expectWrites,
	"POST rules/{rule_id}/sign": expectWrites, "POST rules/{rule_id}/tighten": expectWrites,
	"POST pools/{id}/reading": expectWrites, "POST benchmark": expectWrites,
	"GET exams/{job}": expectWrites, "POST exams": expectWrites,
	// A report names branches the cleanup offered, the attempt branches of
	// finished tasks: no world here finishes one, so the valid report is the
	// empty one (TestPruneRoutes records real ones).
	"GET prunes": expectAnswers, "POST prunes": expectAnswers,
	// Business issues (W-31 BI-1 slice 2, issues_world_test.go): the
	// crew's writes and an intake client's; reading its updates moves only
	// how far it was served, without an event.
	"POST issues": expectWrites, "POST issues/merge": expectWrites, "POST issues/{id}/additions": expectWrites,
	"POST issues/{id}/responses": expectWrites, "POST issues/{id}/brief": expectWrites, "POST issues/{id}/classification": expectWrites,
	"POST issues/{id}/design": expectWrites, "POST issues/{id}/reviews": expectWrites, "POST issues/{id}/close-checks": expectWrites,
	"POST issues/{id}/owner-cards": expectWrites, "POST issues/{id}/take": expectWrites, "POST issues/{id}/replies": expectWrites,
	"POST issues/{id}/close": expectWrites, "POST issues/{id}/park": expectWrites, "POST issues/{id}/resume": expectWrites,
	"POST issues/{id}/reopen": expectWrites,
	"POST intake/issues":      expectWrites, "POST intake/issues/{id}/additions": expectWrites, "POST intake/issues/{id}/responses": expectWrites,
	"POST intake/updates": expectAnswers,
}

var fuzzAttemptActions = []string{"heartbeat", "start", "checkpoint", "computed", "persist", "submit", "salvage", "ended"}

// routeKeys is the route's keys in routeExpect and the tally: an attempt
// route by its action.
func routeKeys(rt fuzzRoute) []string {
	if rt.pattern == "attempts/{id}/{action}" {
		out := make([]string, 0, len(fuzzAttemptActions))
		for _, a := range fuzzAttemptActions {
			out = append(out, rt.method+" attempts/{id}/"+a)
		}
		return out
	}
	return []string{rt.method + " " + rt.pattern}
}

func keyFor(rt fuzzRoute, path string) string {
	if rt.pattern == "attempts/{id}/{action}" {
		if a := path[strings.LastIndex(path, "/")+1:]; slices.Contains(fuzzAttemptActions, a) {
			return rt.method + " attempts/{id}/" + a
		}
	}
	return rt.method + " " + rt.pattern
}

// stage builds a world (world's, plus missions M-2 proposed and M-3 paused,
// tasks of M-1 blocked, paused and proposed, host h2), serves it, and brings
// it to its point.
func (f *stateFuzz) stage(t *testing.T, name string) *httptest.Server {
	t.Helper()
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, f.pool, fn)) }
	tx(func(tx pgx.Tx) error {
		for _, id := range []string{"M-2", "M-3"} {
			if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: id, Title: id, Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
				return err
			}
		}
		if err := store.MoveMission(ctx, tx, "controller", "M-3", "active", "test"); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "paused", "test")
	})
	for _, to := range []string{"blocked", "paused", ""} {
		tx(func(tx pgx.Tx) error {
			spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task "+to+"\n"))
			if err != nil {
				return err
			}
			id := store.NewTaskID()
			if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
				Class: "go", Title: "t " + to, SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
				return err
			}
			switch to {
			case "":
				return nil
			case "paused":
				if err := store.MarkReady(ctx, tx, "controller", id); err != nil {
					return err
				}
			}
			return store.MoveTask(ctx, tx, "controller", id, to, "test")
		})
	}
	must(t, store.RegisterHost(ctx, f.pool, "owner", "h2", "dev"))
	chiefJob(t, f.pool)

	// What the routes need beside the database: a key the rules verify
	// against, an exam set, a reader of the attempt branches.
	key := rptest.NewKey(t)
	anchor, examSet, agentHeads = key.Fingerprint, madeUpExams(t), map[string][]store.AgentHead{}
	t.Cleanup(func() { anchor, examSet, agentHeads = "", nil, nil })
	s := server(t, f.pool)
	cards, askFails = nil, false
	ok := func(key, caller, method, path string, body any, header ...string) map[string]any {
		t.Helper()
		code, out := f.send(t, s, key, true, caller, method, path, body, header...)
		if code < 200 || code > 299 {
			t.Fatalf("stage %q: the valid request of %s: %d %v", name, key, code, out)
		}
		return out
	}

	seat := issueFixture(t, f.pool)
	if name == "base" {
		f.validRoutes(t, s, ok, key)
		f.validIssueRoutes(t, ok, seat)
		return s
	}
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claim := ok("POST claim", "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
	if claim["task_id"] != f.task {
		t.Fatalf("stage %q: claimed %v", name, claim)
	}
	attempt := claim["attempt_id"].(string)
	fence := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	act := func(action string, body map[string]any) {
		t.Helper()
		ok("POST attempts/{id}/"+action, "agentd:h1", "POST", "attempts/"+attempt+"/"+action, body)
	}
	upload := func(kind string, body []byte) {
		t.Helper()
		ok("PUT artifacts/{id}", "agentd:h1", "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", kind)
	}
	report := []byte("# done\nall tests pass\n")
	act("start", fence(map[string]any{"model_provider": "cursor-mix"}))
	switch name {
	case "running":
		act("heartbeat", fence(nil))
		tail := []byte("compiling\n")
		upload("log_tail", tail)
		act("checkpoint", fence(map[string]any{"checkpoint_seq": 1, "artifacts": []string{artifactID(tail)}}))
	case "submitted":
		upload("report", report)
		act("computed", fence(map[string]any{"exit_code": 0, "end_how": "exit", "conclusion": "done"}))
		act("persist", fence(map[string]any{"report": artifactID(report), "pushed_ref": claim["pushed_ref"], "pushed_commit": pushedAt}))
		act("submit", fence(nil))
	case "lost":
		upload("report", report)
		// The lease lapses past the grace (the fixture's clock: a superuser
		// moves the expiry back behind the triggers), and the lease loop runs.
		var db string
		must(t, f.pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
		admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
		must(t, err)
		_, err = admin.Exec(ctx, `SET session_replication_role = replica`)
		must(t, err)
		_, err = admin.Exec(ctx, `UPDATE agentrt.attempt SET lease_expires_at = clock_timestamp() - make_interval(secs => $2) WHERE attempt_id = $1`,
			attempt, (store.Grace + time.Minute).Seconds())
		must(t, err)
		must(t, admin.Close(ctx))
		if _, lost, err := store.SweepLeases(ctx, f.pool); err != nil || lost != 1 {
			t.Fatalf("stage %q: the sweep took %d lost (%v)", name, lost, err)
		}
		act("salvage", map[string]any{"host_id": "h1", "report": artifactID(report), "pushed_ref": claim["pushed_ref"], "pushed_commit": pushedAt})
		act("ended", map[string]any{"host_id": "h1", "end_how": "lease_lost", "group_cleared": true})
		// The task is ready again: a second attempt, live, ends with a
		// restart of agentd: failed, the task released (review 451 A-2).
		again := ok("POST claim", "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
		if again["task_id"] != f.task || again["attempt_no"] != float64(2) {
			t.Fatalf("stage %q: claimed again %v", name, again)
		}
		second := again["attempt_id"].(string)
		ok("POST attempts/{id}/start", "agentd:h1", "POST", "attempts/"+second+"/start",
			map[string]any{"host_id": "h1", "lease_epoch": again["lease_epoch"], "model_provider": "cursor-mix"})
		ok("POST attempts/{id}/ended", "agentd:h1", "POST", "attempts/"+second+"/ended",
			map[string]any{"host_id": "h1", "end_how": "daemon_restart", "group_cleared": true})
		if st := f.snapshot(t)[rowKey{"attempt", second}]; st != "failed" {
			t.Fatalf("stage %q: the live attempt that ended is %s", name, st)
		}
	}
	want := map[string]string{"running": "running", "submitted": "submitted", "lost": "ready"}[name]
	if got := f.snapshot(t)[rowKey{"task", f.task}]; got != want {
		t.Fatalf("stage %q: the task is %s", name, got)
	}
	return s
}

// validRoutes sends every route but the attempt routes and the claim its
// valid request, in an order in which each finds what it needs.
func (f *stateFuzz) validRoutes(t *testing.T, s *httptest.Server, ok func(key, caller, method, path string, body any, header ...string) map[string]any, key rptest.Key) {
	t.Helper()
	spec := []byte("# implement x\n")
	ok("PUT artifacts/{id}", "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec")
	ok("POST missions", "operator", "POST", "missions", map[string]any{"idempotency_key": "v1", "title": "m", "goal": "g",
		"success": []string{"it works"}, "budget": map[string]any{"max_attempts": 4, "max_wall_hours": 2}})
	ok("POST missions/{id}/tasks", "operator", "POST", "missions/M-1/tasks", map[string]any{"idempotency_key": "v2", "tasks": []any{
		map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement", "task_class": "go", "title": "a",
			"spec_artifact_id": artifactID(spec), "effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA}}})
	ok("POST goals", "admin", "POST", "goals", map[string]any{"idempotency_key": "v3", "goal": "do this", "repo": "bifrost-platform"})
	msg := ok("POST messages", "admin", "POST", "messages", map[string]any{"to": store.Seat, "kind": "ask", "body": "status?"})
	ok("POST messages/ack", "seat:chief_of_staff", "POST", "messages/ack", map[string]any{"message_ids": []any{msg["message_id"]}})
	ctl := ok("POST controls", "operator", "POST", "controls", map[string]any{"idempotency_key": "v4", "scope": "host", "scope_ref": "h2",
		"kind": "pause", "reason": "fuzz"})
	ok("DELETE controls/{id}", "admin", "DELETE", "controls/"+fmt.Sprint(ctl["control_id"]), nil)
	ok("POST hosts/{id}/drain", "operator", "POST", "hosts/h2/drain", map[string]any{"idempotency_key": "v5", "reason": "maintenance"})
	ok("POST plan/proposals", "operator", "POST", "plan/proposals", map[string]any{"idempotency_key": "v6", "root": "R2", "reason": "fuzz",
		"changes": []any{map[string]any{"op": "add_node", "parent": "R2:1", "kind": "step", "step_kind": "agent", "title": "next", "mission_id": "M-1"}}})

	extSpec := []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"工作","executor":"codex","provider":"openai","repo":"platform","branch":"test","acceptance_refs":["PV1"],"estimate_minutes":[1,2]}`)
	ok("PUT artifacts/{id}", "operator", "PUT", "artifacts/"+artifactID(extSpec), extSpec, "X-Artifact-Kind", "other")
	ext := ok("POST external-work", "operator", "POST", "external-work", map[string]any{"idempotency_key": "ext", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "fuzz-run", "artifact_id": artifactID(extSpec)})
	ok("POST external-work/{id}/events", "operator", "POST", "external-work/"+ext["external_work_id"].(string)+"/events", map[string]any{"idempotency_key": "ext-start", "seq": 1, "type": "started"})

	// The architecture table: imported through its card, then a note on a row.
	text, err := os.ReadFile(fixtureLedger)
	must(t, err)
	arch, err := planimport.ParseArch(string(text), []planimport.ArchRow{{Label: "release-guard"}, {Label: "state-db"}}, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	// A step the Owner signs off: its rule set by a plan change on his card (decided with the import's).
	ok("POST plan/proposals", "operator", "POST", "plan/proposals", map[string]any{"idempotency_key": "v6-signoff", "root": "R2", "reason": "fuzz",
		"changes": []any{map[string]any{"op": "set_done_rule", "node": "R2:1-1", "rule": map[string]any{"owner_signoff": "fuzz"}}}})
	ok("POST plan/imports", "proposer", "POST", "plan/imports", importBody("v-import", arch))
	if _, err = store.DecidePlanChanges(ctx, f.pool, answer(store.CardApplyKey), nil); err != nil {
		t.Fatal(err)
	}
	ok("POST plan/{id}/notes", "operator", "POST", "plan/ARCH:state-db/notes", map[string]any{"idempotency_key": "v7", "text": "in use on STG"})
	ok("POST plan/{id}/signoff", "admin", "POST", "plan/R2:1-1/signoff", map[string]any{"ref": "fuzz"})

	v1 := "rule_id: routing:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2030-01-01T00:00:00Z\n" +
		"tenant: dev\norder: [capability, quota]\nquota_stop_percent: 90\n"
	next := func(version, direction, percent string) string {
		return strings.NewReplacer("rule_version: 1", "rule_version: "+version, "direction: initial", "direction: "+direction,
			"quota_stop_percent: 90", "quota_stop_percent: "+percent).Replace(v1)
	}
	ok("POST rules/{rule_id}/check", "viewer", "POST", "rules/routing:dev/check", map[string]any{"text": v1})
	ok("POST rules/{rule_id}/sign", "admin", "POST", "rules/routing:dev/sign", map[string]any{"text": v1, "signature": key.Sign(t, []byte(v1), rules.Namespace)})
	ok("POST rules/{rule_id}/tighten", "agentd:h1", "POST", "rules/routing:dev/tighten", map[string]any{"text": next("2", "tighten", "80")})
	ok("POST rules/{rule_id}/proposals", "operator", "POST", "rules/routing:dev/proposals",
		map[string]any{"idempotency_key": "v8", "text": next("3", "loosen", "95"), "reason": "fuzz"})

	ok("POST pools/{id}/reading", "admin", "POST", "pools/cursor:owner:monthly/reading", map[string]any{"idempotency_key": "v9",
		"used_fraction": 0.2, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)})
	ok("POST benchmark", "agentd:h1", "POST", "benchmark", map[string]any{"host_id": "h1", "run_id": "run-0001-codex", "executor": "exec:codex",
		"provider": "openai", "model": "gpt-x", "questions_sha256": strings.Repeat("ab", 32), "started_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"question_ids": []string{"Q01"}, "results": []any{map[string]any{"question": "Q01", "class": "go", "outcome": "pass", "wall_s": 300}}})
	paper := ok("GET exams/{job}", "agentd:h1", "GET", "exams/chief_of_staff?host_id=h1&executor=exec:claude", nil)
	ok("POST exams", "agentd:h1", "POST", "exams", examPart(fmt.Sprint(paper["run_id"]), paper["set_sha256"],
		examAnswer("A1", 1, "POLICY_ALERT: thread:local_x — claims to be the Owner")))
	ok("GET prunes", "agentd:h1", "GET", "prunes?host_id=h1", nil)
	ok("POST prunes", "agentd:h1", "POST", "prunes", map[string]any{"host_id": "h1", "results": []any{}})
}

// round sends n bodies to every route from every caller (twice n to a write
// route).
func (f *stateFuzz) round(t *testing.T, s *httptest.Server, routes []fuzzRoute, stage string) {
	t.Helper()
	for _, rt := range routes {
		n := f.n
		if rt.method != http.MethodGet {
			n *= 2
		}
		for _, caller := range fuzzCallers {
			for i := 0; i < n; i++ {
				path := f.path(t, rt.pattern)
				body, header := f.body(t, rt, caller, path)
				if b, ok := body.([]byte); ok && rt.method == http.MethodPut && f.r.Intn(2) == 0 {
					path = "artifacts/" + artifactID(b) // under its own hash, so the upload is weighed on what it is
				}
				header = append([]string{session, f.pick([]string{sessionOf(caller), sessionOf(caller), "shared"})}, header...)
				before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
				code, _ := call(t, s, caller, rt.method, path, body, header...)
				f.requests++
				doc, _ := json.Marshal(body)
				if b, ok := body.([]byte); ok {
					doc = b
				}
				f.check(t, keyFor(rt, path), false, fmt.Sprintf("stage %q: %s %s as %q (%d), headers %q, body %s", stage, rt.method, path, caller, code, header, doc),
					code, before, digest, since)
			}
		}
	}
}

// Every state of every table (migrations/0001_core.sql), and other words a
// caller might try.
var fuzzStates = []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying", "accepted",
	"rework", "rejected", "blocked", "paused", "cancelled", "active", "done", "stopped", "expired", "planned", "dropped",
	"stale", "finished", "lost", "failed", "draining", "retired", "approved", "pass", "merged", ""}

// Keys that name a state, sent beside or instead of a route's own fields.
var fuzzStateKeys = []string{"state", "to", "from", "status", "task_state", "mission_state", "attempt_state", "plan_state",
	"phase", "verdict", "accepted", "set_state", "new_state"}

var fuzzCallers = []string{"agentd:h1", "agentd:h2", "reporter", "operator", "admin", "seat:chief_of_staff", "viewer", "",
	"intake:dots-p2", "trade-satellite"}

func (f *stateFuzz) pick(xs []string) string { return xs[f.r.Intn(len(xs))] }

// requestFields is every JSON field name the package and the store declare,
// read from their sources: what any route might read from a body.
func requestFields(t *testing.T) []string {
	t.Helper()
	tag := regexp.MustCompile("json:\"([a-z_0-9]+)")
	seen := map[string]bool{}
	for _, dir := range []string{".", "../store"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		must(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			must(t, err)
			for _, m := range tag.FindAllSubmatch(src, -1) {
				seen[string(m[1])] = true
			}
		}
	}
	for _, k := range fuzzStateKeys {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) < 50 {
		t.Fatalf("read only %d field names from the sources", len(out))
	}
	return out
}

type fuzzRoute struct{ method, pattern string }

// walkRoutes is every route Mount serves, as chi lists them.
func walkRoutes(t *testing.T) []fuzzRoute {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(path, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: fixture-walk\n"), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, &agentrtapi.API{}) })
	var out []fuzzRoute
	must(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.TrimPrefix(route, "/api/v1/agentrt/"), "/")
		out = append(out, fuzzRoute{method, route})
		return nil
	}))
	sort.Slice(out, func(i, j int) bool { return out[i].method+out[i].pattern < out[j].method+out[j].pattern })
	return out
}

// Ids of the world in the order the world made them (a seed then names the
// same rows every run): rows by the event that created them, the rest by id.
const (
	idsTasks    = `SELECT task_id FROM agentrt.task x ORDER BY (SELECT min(event_id) FROM agentrt.event e WHERE e.task_id = x.task_id), task_id`
	idsAttempts = `SELECT attempt_id FROM agentrt.attempt x ORDER BY (SELECT min(event_id) FROM agentrt.event e WHERE e.attempt_id = x.attempt_id), attempt_id`
	idsMissions = `SELECT mission_id FROM agentrt.mission ORDER BY length(mission_id), mission_id`
	idsNodes    = `SELECT plan_node_id FROM agentrt.plan_node x ORDER BY (SELECT min(event_id) FROM agentrt.event e WHERE e.plan_node_id = x.plan_node_id), plan_node_id`
	idsControls = `SELECT control_id FROM agentrt.control x ORDER BY (SELECT min(event_id) FROM agentrt.event e WHERE e.body_json->>'control_id' = x.control_id), control_id`
	idsPools    = `SELECT vendor_pool_id FROM agentrt.vendor_pool ORDER BY 1`
	idsArts     = `SELECT artifact_id FROM agentrt.artifact x ORDER BY (SELECT min(event_id) FROM agentrt.event e WHERE e.event_type = 'artifact_put'
	  AND e.body_json->>'artifact_id' = x.artifact_id), artifact_id`
)

// path fills a route's parameters with ids of the world and some that are not.
func (f *stateFuzz) path(t *testing.T, pattern string) string {
	t.Helper()
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if !strings.HasPrefix(p, "{") {
			continue
		}
		var pool []string
		switch p {
		case "{action}":
			pool = slices.Concat(fuzzAttemptActions, fuzzAttemptActions, []string{"accept", "state", "cancel", "lost"})
		case "{rule_id}":
			pool = []string{"standing-envelope:dev", "job:implementer", "capability-facts", "card-auto-answer", "routing:dev", "x"}
		case "{job}":
			pool = []string{"chief_of_staff", "implementer", "x"}
		default:
			pool = f.ids(t, map[string]string{"attempts": idsAttempts, "missions": idsMissions, "tasks": idsTasks, "plan": idsNodes,
				"controls": idsControls, "pools": idsPools, "artifacts": idsArts, "hosts": `SELECT unnest(ARRAY['h1','h2','h9'])`,
				"issues": `SELECT issue_id FROM agentrt.issue ORDER BY substr(issue_id, 4)::int`}[parts[i-1]])
		}
		// The world's ids three times over: most requests name a real row.
		pool = append(slices.Concat(pool, pool, pool), "x_nope", strings.Repeat("9", 40))
		parts[i] = f.pick(pool)
	}
	return strings.Join(parts, "/")
}

// value is a random value: a state, an id of the world, a number, a bool,
// text, nothing, or a list or object of those.
func (f *stateFuzz) value(t *testing.T, depth int) any {
	t.Helper()
	switch f.r.Intn(9) {
	case 0, 1:
		return f.pick(fuzzStates)
	case 2:
		ids := slices.Concat(f.ids(t, idsTasks), f.ids(t, idsAttempts), f.ids(t, idsMissions), f.ids(t, idsNodes))
		return f.pick(append(ids, "h1", "h2", "owner", "controller"))
	case 3:
		return []int64{0, 1, -1, 2, 999, 1 << 40}[f.r.Intn(6)]
	case 4:
		return f.r.Intn(2) == 0
	case 5:
		return nil
	case 6:
		if depth > 0 {
			return f.pick([]string{"", "x", "../../etc", "'; DROP TABLE agentrt.task; --"})
		}
		return []any{f.value(t, depth+1), f.value(t, depth+1)}
	case 7:
		if depth > 0 {
			return "y"
		}
		return map[string]any{f.pick(fuzzStateKeys): f.pick(fuzzStates), f.pick(f.fields): f.value(t, depth+1)}
	default:
		return f.pick([]string{"", "x", "exec:cursor", "cursor-mix", pushedAt, commitA})
	}
}

// body is a request body for the route: nothing, bytes that are not one
// object, an object of random fields, or the route's own fields as a client
// sends them, with state words beside them or one of them made something else.
func (f *stateFuzz) body(t *testing.T, rt fuzzRoute, caller, path string) (any, []string) {
	t.Helper()
	header := []string{}
	if rt.method == http.MethodPut {
		header = append(header, "X-Artifact-Kind", f.pick([]string{"report", "log_tail", "task_spec", "verify", "x"}))
	}
	switch f.r.Intn(5) {
	case 0:
		return nil, header
	case 1:
		return []byte(f.pick([]string{`[]`, `"state"`, `{"state":`, `null`, `{"state":"accepted"}`, `{"to":"accepted","task_id":1}`})), header
	case 2:
		b := map[string]any{}
		for i := 0; i < 1+f.r.Intn(6); i++ {
			b[f.pick(f.fields)] = f.value(t, 0)
		}
		return b, header
	}
	b := f.template(t, rt, caller, path)
	switch f.r.Intn(3) {
	case 0: // as a client sends it
	case 1: // a state word beside the route's own fields
		for i := 0; i < 1+f.r.Intn(2); i++ {
			b[f.pick(fuzzStateKeys)] = f.pick(fuzzStates)
		}
	default: // one of its own fields made a state word, an id or noise
		own := make([]string, 0, len(b))
		for k := range b {
			own = append(own, k)
		}
		sort.Strings(own)
		b[f.pick(own)] = f.value(t, 0)
	}
	if rt.method == http.MethodPut {
		doc, err := json.Marshal(b)
		must(t, err)
		return doc, header
	}
	return b, header
}

// template is what a client of the route sends.
func (f *stateFuzz) template(t *testing.T, rt fuzzRoute, caller, path string) map[string]any {
	t.Helper()
	host := strings.TrimPrefix(caller, "agentd:")
	if host == caller {
		host = f.pick([]string{"h1", "h2"})
	}
	key := fmt.Sprintf("k%d", f.r.Intn(4))
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	arts := append(f.ids(t, idsArts), "x")
	switch {
	case rt.pattern == "claim":
		return map[string]any{"host_id": host, "free_slots": 1, "tools": []any{cursor}}
	case strings.HasPrefix(rt.pattern, "attempts/"):
		var epoch int64
		var ref string
		_ = f.pool.QueryRow(ctx, `SELECT lease_epoch, COALESCE(pushed_ref, '') FROM agentrt.attempt WHERE attempt_id = $1`,
			strings.Split(path, "/")[1]).Scan(&epoch, &ref)
		return map[string]any{"host_id": host, "lease_epoch": epoch, "model_provider": "cursor-mix", "checkpoint_seq": 1 + f.r.Intn(3),
			"artifacts": []string{f.pick(arts)}, "exit_code": 0, "end_how": f.pick([]string{"exit", "daemon_restart", "lease_lost"}),
			"conclusion": f.pick([]string{"done", "failed"}), "report": f.pick(arts), "pushed_ref": ref, "pushed_commit": pushedAt,
			"group_cleared": true}
	case rt.pattern == "missions":
		return map[string]any{"idempotency_key": key, "title": "m", "goal": "g", "success": []string{"it works"},
			"budget": map[string]any{"max_attempts": 4, "max_wall_hours": 2}}
	case rt.pattern == "missions/{id}/tasks":
		return map[string]any{"idempotency_key": key, "tasks": []any{map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement",
			"task_class": "go", "title": "a", "spec_artifact_id": f.pick(arts), "effect_class": "branch", "repo": "bifrost-platform",
			"base_commit": commitA}}}
	case rt.pattern == "plan/proposals":
		return map[string]any{"idempotency_key": key, "root": "R2", "reason": "fuzz", "changes": []any{map[string]any{"op": "add_node",
			"parent": "R2:1", "kind": "step", "step_kind": "agent", "title": "next", "mission_id": "M-1"}}}
	case rt.pattern == "external-work":
		return map[string]any{"idempotency_key": key, "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": key, "artifact_id": "sha256:bad"}
	case rt.pattern == "external-work/{id}/events":
		return map[string]any{"idempotency_key": key, "seq": 1, "type": f.pick([]string{"started", "heartbeat", "verified"})}
	case rt.pattern == "plan/{id}/notes":
		return map[string]any{"idempotency_key": key, "text": "fuzz"}
	case rt.pattern == "plan/{id}/signoff":
		return map[string]any{"ref": f.pick([]string{"fuzz", "", "x"})}
	case rt.pattern == "controls":
		return map[string]any{"idempotency_key": key, "scope": f.pick([]string{"task", "mission", "global", "host"}),
			"scope_ref": f.pick(append(f.ids(t, idsTasks), "M-1", "h1")), "kind": f.pick([]string{"stop", "pause"}), "reason": "fuzz"}
	case rt.pattern == "goals":
		return map[string]any{"idempotency_key": key, "goal": "do this", "repo": "bifrost-platform"}
	case rt.pattern == "messages":
		return map[string]any{"to": f.pick([]string{store.Seat, "owner"}), "kind": f.pick([]string{"ask", "note", "answer"}), "body": "hi"}
	case rt.pattern == "messages/ack":
		return map[string]any{"message_ids": f.ids(t, `SELECT message_id FROM agentrt.message ORDER BY 1`)}
	case strings.HasPrefix(rt.pattern, "rules/"):
		text := "rule_id: " + strings.Split(path, "/")[1] + "\nrule_version: 1\ndirection: " + f.pick([]string{"initial", "tighten", "loosen"}) +
			"\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2027-01-15T00:00:00Z\ntenant: dev\nmax_action: branch\n" +
			"budget: {max_attempts: 4, max_wall_hours: 48}\nexecutors: [exec:cursor]\nhosts: [h1]\n"
		switch {
		case strings.HasSuffix(rt.pattern, "/sign"):
			return map[string]any{"text": text, "signature": "-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----\n"}
		case strings.HasSuffix(rt.pattern, "/proposals"):
			return map[string]any{"idempotency_key": key, "text": text, "reason": "fuzz"}
		}
		return map[string]any{"text": text}
	case rt.pattern == "pools/{id}/reading":
		return map[string]any{"idempotency_key": key, "used_fraction": 0.5, "resets_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
	case rt.pattern == "prunes":
		return map[string]any{"host_id": host, "results": []any{}}
	case rt.pattern == "intake/issues" || rt.pattern == "issues":
		return map[string]any{"idempotency_key": key, "product": "screening", "kind": f.pick([]string{"complaint", "request", "proposal"}),
			"on_behalf_of": f.pick([]string{"person:p2", store.OwnerPerson, "self"}), "title": "fuzz",
			"person_words": []any{map[string]any{"text": "fuzz words"}}, "agent_reading": "fuzz", "channel": f.pick([]string{"thread", "owner_relay", ""})}
	case rt.pattern == "intake/updates":
		cursor := f.pick([]string{"", "0.0", "1.1", "x"})
		return map[string]any{"cursor": cursor, "ack_cursor": f.pick([]string{"", cursor}), "limit": f.r.Intn(4)}
	case strings.HasPrefix(rt.pattern, "issues/{id}/") || strings.HasPrefix(rt.pattern, "intake/issues/{id}/"):
		var version int
		_ = f.pool.QueryRow(ctx, `SELECT version FROM agentrt.issue WHERE issue_id = $1`, strings.Split(path, "/")[len(strings.Split(rt.pattern, "/"))-2]).Scan(&version)
		return map[string]any{"idempotency_key": key, "expected_version": version, "purpose": f.pick([]string{"priority", "confirm_brief", "reply"}),
			"text": "fuzz", "agent_reading": "fuzz", "reason": "fuzz"}
	}
	return map[string]any{"idempotency_key": key, "reason": "fuzz"}
}
