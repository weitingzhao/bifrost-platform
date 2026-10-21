package agentrtapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of the waits past their limit
// (console/src/api/__fixtures__/agentrt-events-waits.testdb.json): GET
// /api/v1/agentrt/events?types=wait_limit_passed as History reads it, one of
// each wait. Off unless asked for:
//
//	CONSOLE_WAITS_FIXTURE_WRITE=1 go test ./internal/agentrt/agentrtapi/ -run TestWaitsThroughTheEvents
const consoleWaitsFixture = "../../../../console/src/api/__fixtures__/agentrt-events-waits.testdb.json"

// W-31 2.3 (RULE-waits 总则 4): the controller's records of an attempt
// running, a task ready and a message unread past their limits are read
// through GET /events like any event, each with the body the store wrote.
func TestWaitsThroughTheEvents(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	status, claim := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
	if status != http.StatusOK || claim["task_id"] != task {
		t.Fatalf("claim: %d %v", status, claim)
	}
	attempt := claim["attempt_id"].(string)
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/start",
		map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"], "model_provider": "cursor-mix"}); code != http.StatusOK {
		t.Fatalf("start: %d %v", code, out)
	}
	ready := store.NewTaskID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# another task\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: ready, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "another", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", ready)
	}))
	thread := store.Writer{Actor: "thread:s1", Token: "operator"}
	_, err := store.SendMessage(ctx, pool, thread, "owner", "note", []byte("hello"))
	must(t, err)
	mail, err := store.SendMessage(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "thread:s1", "answer", []byte("read me"))
	must(t, err)

	n, err := store.WaitLimits(ctx, pool, time.Now().Add(31*time.Minute))
	must(t, err)
	if n != 3 {
		t.Fatalf("%d waits recorded, want 3", n)
	}
	raw := get(t, s, "events?types=wait_limit_passed&limit=10")
	var p struct {
		Events []struct {
			Type      string         `json:"event_type"`
			Stream    string         `json:"event_stream_id"`
			Task      string         `json:"task_id"`
			Attempt   string         `json:"attempt_id"`
			Actor     string         `json:"actor_id"`
			Body      map[string]any `json:"body_json"`
			MissionID string         `json:"mission_id"`
		} `json:"events"`
	}
	must(t, json.Unmarshal(raw, &p))
	got := map[string]map[string]any{}
	for _, e := range p.Events {
		if e.Type != "wait_limit_passed" || e.Actor != "controller" {
			t.Fatalf("an event %+v", e)
		}
		got[e.Body["wait"].(string)] = e.Body
		switch e.Body["wait"] {
		case store.WaitAttemptRunning:
			if e.Attempt != attempt || e.Task != task || e.Stream != store.WaitsStream || e.Body["ref"] != attempt {
				t.Fatalf("the attempt's wait %+v", e)
			}
		case store.WaitTaskReady:
			if e.Task != ready || e.Attempt != "" || e.Body["ref"] != ready {
				t.Fatalf("the ready task's wait %+v", e)
			}
		case store.WaitMessageUnread:
			if e.Stream != store.WaitsStream || e.Body["ref"] != mail || e.Body["calls"] != "thread:s1" {
				t.Fatalf("the message's wait %+v", e)
			}
		}
		for _, f := range []string{"since", "limit_s", "basis", "waited_s", "calls"} {
			if _, ok := e.Body[f]; !ok {
				t.Fatalf("a wait without %s: %+v", f, e.Body)
			}
		}
	}
	if len(got) != 3 {
		t.Fatalf("waits %v", got)
	}
	if os.Getenv("CONSOLE_WAITS_FIXTURE_WRITE") == "1" {
		var doc any
		must(t, json.Unmarshal(raw, &doc))
		out, err := json.MarshalIndent(doc, "", "  ")
		must(t, err)
		must(t, os.WriteFile(consoleWaitsFixture, append(out, '\n'), 0o644))
	}
	if strings.Contains(string(raw), "/Users/") {
		t.Fatal("a local path in the events")
	}
}
