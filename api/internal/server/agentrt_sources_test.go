package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

type fakeApprovals map[string]approvals.Approval

func (f fakeApprovals) Lookup(ref string) (approvals.Approval, bool, error) {
	if ref == "13" {
		return approvals.Approval{}, false, errors.New("store unreadable")
	}
	a, ok := f[ref]
	return a, ok, nil
}

// question answers as questions_wire.go's reader does, from plain fields.
func question(_ context.Context, id string) (store.Question, bool) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	switch id {
	case "q_open":
		return questionFact(id, "open", false, "", "", "", time.Time{})
	case "q_done":
		return questionFact(id, "answered", true, "a", "go", "owner", at)
	case "q_textual":
		return questionFact(id, "answered", true, "", "just text", "owner", at)
	case "q_empty":
		return questionFact(id, "answered", true, "", "", "owner", at)
	case "q_nobody":
		return questionFact(id, "answered", true, "a", "", "", at)
	case "q_untimed":
		return questionFact(id, "answered", true, "a", "", "owner", time.Time{})
	}
	return store.Question{}, false
}

type fakeCI struct{ lists atomic.Int32 }

func (f *fakeCI) CIIndex(context.Context) (map[string]delivery.CIOutcome, error) {
	if f.lists.Add(1) > 10 {
		return nil, errors.New("cluster unreachable")
	}
	return map[string]delivery.CIOutcome{"bifrost-platform@ok": {Status: "Succeeded", Run: "ci-platform-1"}}, nil
}

type fakeCurrent map[string]releases.Record

func (f fakeCurrent) Current(_ context.Context, lane, env string) (releases.Record, error) {
	if lane != "platform" {
		return releases.Record{}, fmt.Errorf("lane %s", lane)
	}
	if env == "qa" {
		return releases.Record{}, releases.ErrUnsettled
	}
	r, ok := f[env]
	if !ok {
		return releases.Record{}, releases.ErrNoDeployment
	}
	return r, nil
}

var (
	sha1 = "1111111111111111111111111111111111111111"
	sha2 = "2222222222222222222222222222222222222222"
)

func platformRec(run, sha string) releases.Record {
	r := releases.Record{Run: run, Lane: "platform", Deploys: true, Repos: map[string]releases.RepoBuild{}}
	if sha != "" {
		r.Repos["bifrost-platform"] = releases.RepoBuild{SHA: sha}
	}
	return r
}

// The controller's sources read the platform's stores (design v5 §5.2 外部事实).
func TestAgentRuntimeSources(t *testing.T) {
	ci := &fakeCI{}
	src := agentRuntimeSources(
		fakeApprovals{"7": {Number: 7, Status: approvals.StatusExecuted}, "8": {Number: 8}},
		question,
		ci,
		fakeCurrent{"stg": platformRec("deliver-platform-2", sha2), "prod": platformRec("deliver-platform-prod-1", sha1),
			"short": platformRec("deliver-short", "abc"), "norepo": platformRec("deliver-norepo", ""), "norun": platformRec("", sha1)},
	)
	ctx := context.Background()

	if s, ok := src.Approval(ctx, 7); !ok || s != "executed" {
		t.Errorf("approval 7 = %q %v", s, ok)
	}
	for _, n := range []int64{8, 9, 13} { // no status, missing, store unreadable
		if s, ok := src.Approval(ctx, n); ok {
			t.Errorf("approval %d = %q, read", n, s)
		}
	}

	if q, ok := src.Question(ctx, "q_open"); !ok || q.Status != "open" || q.Option != "" {
		t.Errorf("open question = %+v %v", q, ok)
	}
	want := store.Question{ID: "q_done", Status: "answered", Option: "a", Text: "go", AnsweredBy: "owner", AnsweredAt: "2026-10-14T17:00:00Z"}
	if q, ok := src.Question(ctx, "q_done"); !ok || q != want {
		t.Errorf("answered question = %+v %v", q, ok)
	}
	if q, ok := src.Question(ctx, "q_textual"); !ok || q.Option != "none" || q.Text != "just text" {
		t.Errorf("a text-only answer = %+v %v", q, ok)
	}
	for _, id := range []string{"q_empty", "q_nobody", "q_untimed", "q_missing"} {
		if q, ok := src.Question(ctx, id); ok {
			t.Errorf("%s = %+v, read", id, q)
		}
	}

	for i := 0; i < 5; i++ {
		if s, run, ok := src.CI(ctx, "bifrost-platform", "ok"); !ok || s != "Succeeded" || run != "ci-platform-1" {
			t.Errorf("ci = %q %q %v", s, run, ok)
		}
		if _, _, ok := src.CI(ctx, "bifrost-platform", fmt.Sprint("commit-", i)); ok {
			t.Errorf("a commit with no run read")
		}
	}
	if n := ci.lists.Load(); n != 1 {
		t.Errorf("ten CI reads within the snapshot listed %d times", n)
	}

	for env, want := range map[string]string{"stg": sha2 + " map[delivery_run:deliver-platform-2]", "prod": sha1 + " map[delivery_run:deliver-platform-prod-1]"} {
		commit, ref, ok := src.Deployed(ctx, env)
		if got := commit + " " + fmt.Sprint(ref); !ok || got != want {
			t.Errorf("deployed %s = %s %v", env, got, ok)
		}
	}
	for _, env := range []string{"dev", "qa", "short", "norepo", "norun"} {
		if commit, _, ok := src.Deployed(ctx, env); ok {
			t.Errorf("deployed %s = %s, read", env, commit)
		}
	}

	none := agentRuntimeSources(nil, nil, nil, nil)
	if _, ok := none.Approval(ctx, 7); ok {
		t.Error("no approvals store, read")
	}
	if _, ok := none.Question(ctx, "q_done"); ok {
		t.Error("no question store, read")
	}
	if _, _, ok := none.CI(ctx, "bifrost-platform", "ok"); ok {
		t.Error("no CI reader, read")
	}
	if _, _, ok := none.Deployed(ctx, "stg"); ok {
		t.Error("no release records, read")
	}
	empty := (&Server{}).agentRuntimeSources() // no store: a nil reader, never a typed nil
	if _, ok := empty.Approval(ctx, 7); ok {
		t.Error("a server with no stores read an approval")
	}
	if _, _, ok := empty.Deployed(ctx, "stg"); ok {
		t.Error("a server with no stores read a deployment")
	}
}

// The CI snapshot lists again once its time is up, and an error is not kept.
func TestCISnapshot(t *testing.T) {
	var lists int
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	fail := false
	s := &ciSnapshot{ttl: 5 * time.Second, now: func() time.Time { return now }, index: func(context.Context) (map[string]delivery.CIOutcome, error) {
		lists++
		if fail {
			return nil, errors.New("down")
		}
		return map[string]delivery.CIOutcome{"r@c": {Status: "Failed", Run: fmt.Sprint("run-", lists)}}, nil
	}}
	ctx := context.Background()
	if o, _ := s.get(ctx, "r@c"); o.Run != "run-1" {
		t.Fatalf("first = %+v", o)
	}
	now = now.Add(4 * time.Second)
	if o, _ := s.get(ctx, "r@c"); o.Run != "run-1" || lists != 1 {
		t.Fatalf("within the snapshot = %+v after %d lists", o, lists)
	}
	now = now.Add(2 * time.Second)
	if o, _ := s.get(ctx, "r@c"); o.Run != "run-2" {
		t.Fatalf("after it = %+v", o)
	}
	fail, now = true, now.Add(10*time.Second)
	if _, err := s.get(ctx, "r@c"); err == nil {
		t.Fatal("a failed list read as an old snapshot")
	}
	fail = false
	if o, err := s.get(ctx, "r@c"); err != nil || o.Run != "run-4" {
		t.Fatalf("after a failed list = %+v %v; it lists again", o, err)
	}
}

// A caller waiting for another's list gives up with its own context, and
// the list, once done, serves the rest (review 162 B1).
func TestCISnapshotWaitEndsWithTheCaller(t *testing.T) {
	release := make(chan struct{})
	var lists atomic.Int32
	s := &ciSnapshot{ttl: time.Minute, index: func(context.Context) (map[string]delivery.CIOutcome, error) {
		lists.Add(1)
		<-release
		return map[string]delivery.CIOutcome{"r@c": {Status: "Succeeded", Run: "ci-1"}}, nil
	}}
	first := make(chan error, 1)
	go func() { _, err := s.get(context.Background(), "r@c"); first <- err }()
	for lists.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	waited := make(chan error, 1)
	go func() { _, err := s.get(ctx, "r@c"); waited <- err }()
	select {
	case err := <-waited:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a waiter = %v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a waiter outlived its context")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if o, err := s.get(context.Background(), "r@c"); err != nil || o.Run != "ci-1" || lists.Load() != 1 {
		t.Fatalf("after the list = %+v %v, %d lists", o, err, lists.Load())
	}
	done, cancelDone := context.WithCancel(context.Background())
	cancelDone()
	if _, err := s.get(done, "r@c"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled caller read the snapshot: %v", err)
	}
}

// A store read that does not take a context still ends with it, and reads
// that never return hold at most the store's slots (review 161 B1).
func TestSourceReadsAreBounded(t *testing.T) {
	reads := newBoundedReads(2)
	release := make(chan struct{})
	var started atomic.Int32
	stuck := func() (int, bool, error) { started.Add(1); <-release; return 1, true, nil }
	watchdog := time.AfterFunc(10*time.Second, func() { panic("a source read did not return") })
	defer watchdog.Stop()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		start := time.Now()
		_, ok, err := within(ctx, reads, stuck)
		cancel()
		if ok || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
			t.Fatalf("read %d = %v %v after %s", i, ok, err, time.Since(start))
		}
	}
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, ok, err := within(ctx, reads, stuck)
		cancel()
		if ok || !errors.Is(err, errBusy) {
			t.Fatalf("a read past the slots = %v %v", ok, err)
		}
	}
	if n := started.Load(); n != 2 {
		t.Fatalf("%d reads started; two slots", n)
	}
	close(release)
	// both slots come back once the stuck reads return
	for deadline := time.Now().Add(5 * time.Second); len(reads.slots) != 0; {
		if time.Now().After(deadline) {
			t.Fatalf("%d slots still held after the reads returned", len(reads.slots))
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if v, ok, err := within(context.Background(), reads, func() (int, bool, error) { return 7, true, nil }); err == nil {
			if !ok || v != 7 {
				t.Fatalf("after the stuck reads returned = %v %v", v, ok)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the slots were not freed when the reads returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// questions_wire.go's reader on a real question store: open, answered by
// option, answered by text only, missing.
func TestQuestionReaderOnTheStore(t *testing.T) {
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask := func(key string, allowText bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"idempotency_key": key, "title": "Which way?", "urgency": "Before tonight",
			"meanwhile": "The line waits", "options": []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait"}}, "allow_text": allowText})
		if res := rt.svc.Create(ctx, "thread-a", "reporter", "reporter", body); res.Code >= 300 {
			t.Fatalf("create = %d %v", res.Code, res.Body)
		}
	}
	ask("0123456789abcdef", false)
	ask("fedcba9876543210", true)
	read := rt.agentRuntimeQuestion()
	q1, ok := read(ctx, "1")
	if !ok || q1.Status != "open" || q1.Option != "" {
		t.Fatalf("open = %+v %v", q1, ok)
	}
	answer := func(ref string, body map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", ref, raw); res.Code >= 300 {
			t.Fatalf("answer %s = %d %v", ref, res.Code, res.Body)
		}
	}
	answer("1", map[string]any{"option": "b", "channel": "console"})
	answer("2", map[string]any{"text": "neither, wait for the data", "channel": "console"})
	q, ok := read(ctx, "1")
	at, err := time.Parse(time.RFC3339, q.AnsweredAt)
	if !ok || q.Status != "answered" || q.Option != "b" || q.AnsweredBy != "admin" || err != nil || time.Since(at) > time.Minute || at.Location() != time.UTC {
		t.Fatalf("answered = %+v %v %v", q, ok, err)
	}
	if q, ok := read(ctx, "2"); !ok || q.Option != "none" || q.Text != "neither, wait for the data" {
		t.Fatalf("answered by text = %+v %v", q, ok)
	}
	if _, ok := read(ctx, "9"); ok {
		t.Fatal("a missing question read")
	}
	var nilRuntime *questionRuntime
	if nilRuntime.agentRuntimeQuestion() != nil {
		t.Fatal("no question store gave a reader")
	}
}
