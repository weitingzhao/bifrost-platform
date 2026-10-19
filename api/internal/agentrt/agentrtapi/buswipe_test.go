package agentrtapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 10 seconds", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// T15 总线重建（接口层） (v5 §5.3, review 142 R6): redis-agents is wiped
// while a task runs and a message waits. While it is empty (the relay is
// held off, so nothing writes it back; review 340 A4) the heartbeat, a new
// claim, the unread message and a stop set then are all answered by what
// PostgreSQL holds, and Redis is still empty after them. Let back, the relay
// rebuilds Redis by itself, and again when Redis is wiped under it while it
// runs; the message it may now signal twice is handled once, by its
// message_id.
func TestBusWipedMidTask(t *testing.T) {
	rdb, prefix := agentrttest.Redis(t)
	actionStore = &actions.Store{Redis: rdb, Prefix: prefix}
	t.Cleanup(func() { actionStore = nil })
	pool, task := world(t)
	s := server(t, pool)
	c := func(caller, method, path string, body any) (int, map[string]any) {
		return call(t, s, caller, method, path, body)
	}
	rl := &relay.Relay{PG: pool, Redis: rdb, Prefix: prefix, LockTTL: 5 * time.Second}
	start := func() (stop func()) {
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- rl.Run(runCtx) }()
		var once sync.Once
		stop = func() { once.Do(func() { cancel(); <-done }) }
		t.Cleanup(stop)
		return stop
	}
	stopRelay := start()
	delivered := func() bool {
		var pending int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox WHERE delivered_at IS NULL`).Scan(&pending))
		return pending == 0
	}
	mbox := prefix + store.MailboxTopic(store.Seat)

	attempt, fence := claimOne(t, s, c)
	code, out := c("admin", "POST", "messages", map[string]any{"to": store.Seat, "kind": "ask", "body": "status?"})
	if code != http.StatusOK {
		t.Fatalf("send: %d %v", code, out)
	}
	message := out["message_id"].(string)
	eventually(t, "the message in its mailbox stream", func() bool {
		n, _ := rdb.XLen(ctx, mbox).Result()
		return delivered() && n >= 1
	})

	stopRelay()
	agentrttest.DropPrefix(t, rdb, prefix)
	empty := func(when string) {
		t.Helper()
		for _, key := range []string{"meta:epoch", store.MailboxTopic(store.Seat), "stop", "wake"} {
			if n, err := rdb.Exists(ctx, prefix+key).Result(); err != nil || n != 0 {
				t.Fatalf("%s: %s is in Redis (%d, %v)", when, key, n, err)
			}
		}
	}
	empty("after the wipe")

	if got, beat := c("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(nil)); got != http.StatusOK || beat["stop"] != false {
		t.Fatalf("a heartbeat after the wipe: %d %v", got, beat)
	}
	var spec string
	must(t, pool.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&spec))
	second := store.NewTaskID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: second, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "second", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", second)
	}))
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	if got, claim := c("agentd:h3", "POST", "claim", map[string]any{"host_id": "h3", "free_slots": 1, "tools": []any{cursor}}); got != http.StatusOK || claim["task_id"] != second {
		t.Fatalf("a claim after the wipe: %d %v", got, claim)
	}
	code, out = c("seat:chief_of_staff", "GET", "messages", nil)
	if ms, _ := out["messages"].([]any); code != http.StatusOK || len(ms) != 1 || ms[0].(map[string]any)["message_id"] != message {
		t.Fatalf("unread after the wipe: %d %v", code, out)
	}
	if code, out := c("admin", "POST", "controls", map[string]any{"idempotency_key": "w1", "scope": "task", "scope_ref": task,
		"kind": "stop", "reason": "T15"}); code != http.StatusOK {
		t.Fatalf("stop: %d %v", code, out)
	}
	if code, out := c("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(nil)); code != http.StatusOK || out["stop"] != true {
		t.Fatalf("the heartbeat after a stop set on an empty Redis: %d %v", code, out)
	}

	empty("after the calls")

	start()
	eventually(t, "the rebuild and the delivery after it", func() bool {
		n, _ := rdb.Exists(ctx, prefix+"meta:epoch").Result()
		l, _ := rdb.XLen(ctx, mbox).Result()
		return n == 1 && l >= 1 && delivered()
	})
	entries, err := rdb.XRange(ctx, mbox, "-", "+").Result()
	must(t, err)
	if len(entries) == 0 {
		t.Fatal("the rebuilt mailbox is empty")
	}
	// Wiped again under the same running relay, it finds its lock gone and
	// rebuilds by itself, a new epoch with the unread message (review 341 B3).
	epoch, err := rdb.Get(ctx, prefix+"meta:epoch").Result()
	must(t, err)
	agentrttest.DropPrefix(t, rdb, prefix)
	eventually(t, "the running relay's own rebuild", func() bool {
		now, _ := rdb.Get(ctx, prefix+"meta:epoch").Result()
		l, _ := rdb.XLen(ctx, mbox).Result()
		return now != "" && now != epoch && l >= 1 && delivered()
	})
	for i := 0; i < 2; i++ { // Redis may signal it twice; it is acknowledged once
		code, out := c("seat:chief_of_staff", "POST", "messages/ack", map[string]any{"message_ids": []string{message}})
		if acked, _ := out["acknowledged"].([]any); code != http.StatusOK || len(acked) != 1-i {
			t.Fatalf("ack %d: %d %v", i+1, code, out)
		}
	}
	if code, out := c("seat:chief_of_staff", "GET", "messages", nil); code != http.StatusOK || len(out["messages"].([]any)) != 0 {
		t.Fatalf("unread after the ack: %d %v", code, out)
	}
	var acks int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'message_acked' AND body_json->>'message_id' = $1`, message).Scan(&acks))
	if acks != 1 {
		t.Fatalf("%d message_acked events", acks)
	}
}
