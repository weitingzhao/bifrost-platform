package relay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var ctx = context.Background()

type fixture struct {
	db    *agentrttest.DB
	relay *Relay
	rc    *redis.Client
	pre   string
	spec  string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	rc, pre := agentrttest.Redis(t)
	f := &fixture{db: db, rc: rc, pre: pre, relay: &Relay{PG: db.Pool(t, db.Runtime), Redis: rc, Prefix: pre, LockTTL: 5 * time.Second}}
	f.tx(t, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-1", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		var err error
		f.spec, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "text/plain", []byte("spec"))
		return err
	})
	return f
}

func (f *fixture) tx(t *testing.T, fn func(pgx.Tx) error) {
	t.Helper()
	if err := store.InTx(ctx, f.relay.PG, fn); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) readyTask(t *testing.T) string {
	t.Helper()
	var id string
	f.tx(t, func(tx pgx.Tx) error {
		id = store.NewTaskID()
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", OffPlanReason: "relay test",
			Role: "implement", Class: "go", Title: "t", SpecArtifact: f.spec, Effect: "branch"}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	})
	return id
}

func (f *fixture) post(t *testing.T, to string, n int) []string {
	t.Helper()
	var ids []string
	f.tx(t, func(tx pgx.Tx) error {
		for i := range n {
			id, err := store.PostMessage(ctx, tx, "controller", to, "note", []byte(fmt.Sprintf("message %d", i)))
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	return ids
}

func (f *fixture) ack(t *testing.T, id string) {
	t.Helper()
	f.tx(t, func(tx pgx.Tx) error { return store.AckMessage(ctx, tx, "exec:codex", id) })
}

// unread is the fact a receiver acts on: PostgreSQL's unread list.
func (f *fixture) unread(t *testing.T, to string) []string {
	t.Helper()
	rows, err := f.relay.PG.Query(ctx, `SELECT message_id FROM agentrt.message WHERE to_actor_id = $1 AND acked_at IS NULL ORDER BY created_at, message_id`, to)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func (f *fixture) streamEventIDs(t *testing.T, key string) []string {
	t.Helper()
	msgs, err := f.rc.XRange(ctx, f.pre+key, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range msgs {
		out = append(out, fmt.Sprint(m.Values["event_id"]))
	}
	return out
}

func deliverAll(t *testing.T, l *Lease) {
	t.Helper()
	for {
		n, err := l.DeliverOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// T31: the relay dies after XADD and before marking the row delivered; the
// next relay sends that signal again, so it is in Redis twice and counts once
// after de-duplication by event_id; every outbox row ends delivered.
func TestCrashBetweenXAddAndMarkResendsOnce(t *testing.T) {
	f := setup(t)
	f.readyTask(t)
	f.readyTask(t)
	l, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	crashed := false
	f.relay.afterXAdd = func(int64) error {
		if !crashed {
			crashed = true
			return errors.New("killed")
		}
		return nil
	}
	if _, err := l.DeliverOnce(ctx); err == nil {
		t.Fatal("the simulated crash did not stop delivery")
	}
	deliverAll(t, l)

	ids := f.streamEventIDs(t, "wake")
	if len(ids) != 3 {
		t.Fatalf("wake has %d entries, want 3 (one sent twice)", len(ids))
	}
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	if len(seen) != 2 {
		t.Fatalf("de-duplicated by event_id: %d signals, want 2", len(seen))
	}
	var pending, orphan int
	if err := f.relay.PG.QueryRow(ctx, `SELECT count(*) FILTER (WHERE delivered_at IS NULL), count(*) FILTER (WHERE e.event_id IS NULL)
	  FROM agentrt.outbox o LEFT JOIN agentrt.event e USING (event_id)`).Scan(&pending, &orphan); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || orphan != 0 {
		t.Fatalf("pending %d, outbox rows without an event %d", pending, orphan)
	}
}

// T15a, quiet fixture: after wiping the prefix and rebuilding, Redis holds
// exactly the rebuild sources read from PostgreSQL.
func TestRebuildQuietFixtureMatchesPostgres(t *testing.T) {
	f := setup(t)
	t1, t2 := f.readyTask(t), f.readyTask(t)
	msgs := f.post(t, "exec:codex", 3)
	f.ack(t, msgs[1])
	f.tx(t, func(tx pgx.Tx) error {
		if err := store.SetControl(ctx, tx, "controller", "c-1", "global", "", "pause", "test"); err != nil {
			return err
		}
		if err := store.SetControl(ctx, tx, "controller", "c-2", "task", t1, "stop", "test"); err != nil {
			return err
		}
		if err := store.ClearControl(ctx, tx, "owner", "c-2"); err != nil {
			return err
		}
		return store.SetCooldown(ctx, tx, "controller", "codex:plan", "codex", "weekly", time.Now().Add(10*time.Minute), "rate limited")
	})
	agentrttest.DropPrefix(t, f.rc, f.pre)

	l, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := l.Rebuild(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.rc.Get(ctx, f.pre+"meta:epoch").Result(); got != fmt.Sprint(epoch) {
		t.Fatalf("epoch %q, want %d", got, epoch)
	}
	if n, _ := f.rc.Exists(ctx, f.pre+"meta:rebuilding").Result(); n != 0 {
		t.Fatal("rebuilding flag left behind")
	}

	wake, _ := f.rc.XRange(ctx, f.pre+"wake", "-", "+").Result()
	var woken []string
	for _, m := range wake {
		woken = append(woken, fmt.Sprint(m.Values["p"]))
	}
	sort.Strings(woken)
	for _, want := range []string{t1, t2} {
		found := false
		for _, p := range woken {
			if strings.Contains(p, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("ready task %s has no wake signal: %v", want, woken)
		}
	}
	if len(wake) != 2 {
		t.Fatalf("wake has %d entries, want 2", len(wake))
	}

	mbox, _ := f.rc.XRange(ctx, f.pre+"mbox:exec:codex", "-", "+").Result()
	var inRedis []string
	for _, m := range mbox {
		inRedis = append(inRedis, fmt.Sprint(m.Values["p"]))
	}
	want := f.unread(t, "exec:codex")
	if len(inRedis) != len(want) {
		t.Fatalf("mailbox has %d signals, PostgreSQL %d unread", len(inRedis), len(want))
	}
	for i, id := range want {
		if !strings.Contains(inRedis[i], id) {
			t.Fatalf("mailbox entry %d is %s, want %s", i, inRedis[i], id)
		}
	}
	groups, err := f.rc.XInfoGroups(ctx, f.pre+"mbox:exec:codex").Result()
	if err != nil || len(groups) != 1 || groups[0].Name != "g" {
		t.Fatalf("consumer group: %v %v", groups, err)
	}

	stop, _ := f.rc.XRange(ctx, f.pre+"stop", "-", "+").Result()
	if len(stop) != 1 || !strings.Contains(fmt.Sprint(stop[0].Values["p"]), "c-1") {
		t.Fatalf("stop stream %v, want only c-1", stop)
	}
	if ttl, _ := f.rc.PTTL(ctx, f.pre+"cooldown:codex:plan").Result(); ttl <= 0 || ttl > 10*time.Minute {
		t.Fatalf("cooldown ttl %v", ttl)
	}
}

// T15a: more unread messages than the mailbox keeps. Redis trims signals;
// the receiver still finds every message in PostgreSQL.
func TestOverMaxLenLosesSignalsNotMessages(t *testing.T) {
	f := setup(t)
	f.post(t, "exec:cursor", maxMbox+200)
	agentrttest.DropPrefix(t, f.rc, f.pre)
	l, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	n, _ := f.rc.XLen(ctx, f.pre+"mbox:exec:cursor").Result()
	if n >= maxMbox+200 {
		t.Fatalf("mailbox kept %d entries; MAXLEN did not trim", n)
	}
	if got := len(f.unread(t, "exec:cursor")); got != maxMbox+200 {
		t.Fatalf("PostgreSQL has %d unread, want %d", got, maxMbox+200)
	}
}

// T15a: a message posted, a message acknowledged and a stop set while a
// rebuild is between its snapshot and its writes are all present afterwards
// once the outbox is delivered; the acknowledged one is gone from the facts.
func TestWritesDuringRebuildAreKept(t *testing.T) {
	f := setup(t)
	before := f.post(t, "exec:codex", 2)
	var during []string
	f.relay.afterSnapshot = func() {
		during = f.post(t, "exec:codex", 1)
		f.ack(t, before[0])
		f.tx(t, func(tx pgx.Tx) error {
			return store.SetControl(ctx, tx, "controller", "c-during", "global", "", "stop", "test")
		})
	}
	l, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	deliverAll(t, l)

	got := f.unread(t, "exec:codex")
	want := []string{before[1], during[0]}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unread %v, want %v", got, want)
	}
	stop, _ := f.rc.XRange(ctx, f.pre+"stop", "-", "+").Result()
	found := false
	for _, m := range stop {
		if strings.Contains(fmt.Sprint(m.Values["p"]), "c-during") {
			found = true
		}
	}
	if !found {
		t.Fatal("the stop set during the rebuild never reached Redis")
	}
	var active int
	if err := f.relay.PG.QueryRow(ctx, `SELECT count(*) FROM agentrt.control WHERE cleared_at IS NULL`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active controls %d, %v", active, err)
	}
}

// T15a: a relay whose lock expired cannot write, renew, or release the new
// holder's lock.
func TestExpiredLockIsFenced(t *testing.T) {
	f := setup(t)
	f.relay.LockTTL = 300 * time.Millisecond
	old, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.relay.Acquire(ctx); !errors.Is(err, ErrHeld) {
		t.Fatalf("second acquire while held: %v", err)
	}
	time.Sleep(450 * time.Millisecond)
	f.relay.LockTTL = 5 * time.Second
	cur, err := f.relay.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.readyTask(t)
	if _, err := old.DeliverOnce(ctx); !errors.Is(err, ErrFenced) {
		t.Fatalf("old holder delivered: %v", err)
	}
	if _, err := old.Rebuild(ctx); !errors.Is(err, ErrFenced) {
		t.Fatalf("old holder rebuilt: %v", err)
	}
	if err := old.Renew(ctx); !errors.Is(err, ErrFenced) {
		t.Fatalf("old holder renewed: %v", err)
	}
	if err := old.Release(ctx); !errors.Is(err, ErrFenced) {
		t.Fatalf("old holder released: %v", err)
	}
	if v, _ := f.rc.Get(ctx, f.pre+"relay:lock").Result(); v != cur.Token() {
		t.Fatalf("lock holds %q, want the new token %q", v, cur.Token())
	}
	deliverAll(t, cur)
	if ids := f.streamEventIDs(t, "wake"); len(ids) != 1 {
		t.Fatalf("wake has %d entries after the new holder delivered, want 1", len(ids))
	}
}

// Run rebuilds a wiped Redis by itself and then delivers on notification.
func TestRunRebuildsThenDelivers(t *testing.T) {
	f := setup(t)
	f.readyTask(t)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- f.relay.Run(runCtx) }()
	defer func() { cancel(); <-done }()
	waitFor(t, func() bool { n, _ := f.rc.Exists(ctx, f.pre+"meta:epoch").Result(); return n == 1 })
	f.readyTask(t)
	waitFor(t, func() bool {
		var pending int
		_ = f.relay.PG.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox WHERE delivered_at IS NULL`).Scan(&pending)
		return pending == 0
	})
	if n, _ := f.rc.XLen(ctx, f.pre+"wake").Result(); n < 2 {
		t.Fatalf("wake has %d entries, want both ready tasks signalled", n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within 10 seconds")
}
