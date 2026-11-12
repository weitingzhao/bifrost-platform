package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// externalSeqs are the seqs of a work's notes, in the order recorded.
func externalSeqs(t *testing.T, w *world, id string) []int64 {
	t.Helper()
	var seqs []int64
	ok(t, w.rt.QueryRow(ctx, `SELECT array_agg((body_json->'external_work'->>'seq')::bigint ORDER BY seq) FROM agentrt.event WHERE event_stream_id=$1`, "external-work:"+id).Scan(&seqs))
	return seqs
}

// externalCommits are the commits a work's latest note keeps.
func externalCommits(t *testing.T, w *world, id string) []string {
	t.Helper()
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->'external_work'->'commit_shas','[]') FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "external-work:"+id).Scan(&raw))
	var out []string
	ok(t, json.Unmarshal(raw, &out))
	return out
}

// An event that names no seq gets the work's next one, and its answer says
// which. Its replay is by its key; a later event naming that seq is answered
// as any replay is. An event that names its seq keeps its rules around them.
func TestExternalEventSeqAssigned(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	who := wr("thread:seqless")
	out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "seqless", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	post := func(e store.ExternalEvent) (map[string]any, error) {
		return store.PostExternalEvent(ctx, w.rt, who, id, e)
	}
	assigned := func(e store.ExternalEvent, want int) map[string]any {
		t.Helper()
		out, err := post(e)
		ok(t, err)
		if fmt.Sprint(out["seq"]) != fmt.Sprint(want) {
			t.Fatalf("%s %s: seq %v; want %d", e.Type, e.Key, out["seq"], want)
		}
		return out
	}

	_, err = post(store.ExternalEvent{Key: "negative", Seq: -1, Type: "started"})
	is(t, err, store.ErrInvalid)
	started := assigned(store.ExternalEvent{Key: "start", Type: "started"}, 1)
	if started["state"] != "Running" {
		t.Fatalf("started %v", started)
	}
	// The same key again is the first answer, and no second event.
	again, err := post(store.ExternalEvent{Key: "start", Type: "started"})
	ok(t, err)
	same(t, again, started)

	// Explicit seqs interleave: the next one, its replay under another key, a gap.
	explicit := store.ExternalEvent{Key: "hb-2", Seq: 2, Type: "heartbeat"}
	second := assigned(explicit, 2)
	explicit.Key = "hb-2-again"
	again, err = post(explicit)
	ok(t, err)
	same(t, again, second)
	third := assigned(store.ExternalEvent{Key: "hb-3", Type: "heartbeat"}, 3)
	_, err = post(store.ExternalEvent{Key: "gap", Seq: 5, Type: "heartbeat"})
	is(t, err, store.ErrConflict)
	// The assigned seq named later: the same event is its replay, another is a conflict.
	again, err = post(store.ExternalEvent{Key: "hb-3-named", Seq: 3, Type: "heartbeat"})
	ok(t, err)
	same(t, again, third)
	_, err = post(store.ExternalEvent{Key: "hb-3-other", Seq: 3, Type: "heartbeat", ObservedAt: "2026-11-12T00:00:00Z"})
	is(t, err, store.ErrConflict)
	assigned(store.ExternalEvent{Key: "hb-4", Seq: 4, Type: "heartbeat"}, 4)

	// Two posted at once, each with its own key, get the next two seqs; two
	// with one key are one event.
	var wg sync.WaitGroup
	got := make(chan string, 4)
	errs := make(chan error, 4)
	for _, key := range []string{"race-a", "race-b", "race-same", "race-same"} {
		conn := w.db.Connect(t, w.db.Runtime)
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := store.PostExternalEvent(ctx, conn, who, id, store.ExternalEvent{Key: key, Type: "heartbeat"})
			errs <- e
			if e == nil {
				got <- fmt.Sprint(out["seq"])
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(got)
	for e := range errs {
		ok(t, e)
	}
	var raced []string
	for s := range got {
		raced = append(raced, s)
	}
	slices.Sort(raced)
	raced = slices.Compact(raced)
	if !slices.Equal(raced, []string{"5", "6", "7"}) {
		t.Fatalf("concurrent seqs %v; want 5, 6 and 7, the shared key once", raced)
	}

	assigned(store.ExternalEvent{Key: "end", Type: "submitted"}, 8)
	_, err = post(store.ExternalEvent{Key: "late", Type: "heartbeat"})
	is(t, err, store.ErrState)
	if seqs := externalSeqs(t, w, id); !slices.Equal(seqs, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("recorded seqs %v", seqs)
	}
	w.chainHolds(t, "external-work:"+id)
}

// A heartbeat may name commits, checked as the submitted event's are. The
// work's note keeps every commit named so far in the order first named, so
// the plan view shows them while the work runs.
func TestExternalHeartbeatCommits(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	who := wr("thread:early-commit")
	out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "early-commit", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	post := func(e store.ExternalEvent) error {
		_, err := store.PostExternalEvent(ctx, w.rt, who, id, e)
		return err
	}
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64)

	is(t, post(store.ExternalEvent{Key: "start-commit", Type: "started", Commits: []string{a}}), store.ErrInvalid)
	ok(t, post(store.ExternalEvent{Key: "start", Type: "started"}))
	for name, bad := range map[string]store.ExternalEvent{
		"upper case":      {Type: "heartbeat", Commits: []string{strings.Repeat("A", 40)}},
		"short":           {Type: "heartbeat", Commits: []string{a[:39]}},
		"not hex":         {Type: "heartbeat", Commits: []string{strings.Repeat("g", 40)}},
		"one bad of two":  {Type: "heartbeat", Commits: []string{a, "main"}},
		"too many":        {Type: "heartbeat", Commits: slices.Repeat([]string{a}, 101)},
		"a report":        {Type: "heartbeat", ReportDigest: "sha256:" + strings.Repeat("0", 64)},
		"an exit code":    {Type: "heartbeat", ExitCode: new(int)},
		"on linked":       {Type: "linked", Root: root, Node: s1, Commits: []string{a}},
		"on cancelled":    {Type: "cancelled", Commits: []string{a}},
		"seq and bad sha": {Seq: 2, Type: "heartbeat", Commits: []string{"deadbeef"}},
	} {
		bad.Key = name
		if err := post(bad); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%s: %v; want refused as invalid", name, err)
		}
	}
	if got := externalCommits(t, w, id); len(got) != 0 {
		t.Fatalf("a refused event left commits %v", got)
	}

	ok(t, post(store.ExternalEvent{Key: "hb-a", Type: "heartbeat", Commits: []string{a}}))
	ok(t, post(store.ExternalEvent{Key: "hb-none", Type: "heartbeat"}))
	if got := externalCommits(t, w, id); !slices.Equal(got, []string{a}) {
		t.Fatalf("after a heartbeat naming none: %v", got)
	}
	// Running, and the plan view's record of the work carries the commit.
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	raw, _ := json.Marshal(view["external_work"])
	var works []struct {
		ID      string   `json:"external_work_id"`
		State   string   `json:"state"`
		Commits []string `json:"commit_shas"`
	}
	ok(t, json.Unmarshal(raw, &works))
	if len(works) != 1 || works[0].ID != id || works[0].State != "Running" || !slices.Equal(works[0].Commits, []string{a}) {
		t.Fatalf("plan view %s", raw)
	}

	// A later list is added to what the work named, first named first.
	ok(t, post(store.ExternalEvent{Key: "hb-ba", Type: "heartbeat", Commits: []string{b, a, b}}))
	if got := externalCommits(t, w, id); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("after a second list: %v", got)
	}
	// The note keeps at most 100.
	many := make([]string, 99)
	for i := range many {
		many[i] = fmt.Sprintf("%040x", i+1)
	}
	is(t, post(store.ExternalEvent{Key: "hb-many", Type: "heartbeat", Commits: many}), store.ErrInvalid)
	ok(t, post(store.ExternalEvent{Key: "end", Type: "submitted", Commits: []string{c, b}}))
	if got := externalCommits(t, w, id); !slices.Equal(got, []string{a, b, c}) {
		t.Fatalf("after submitted: %v", got)
	}
	w.chainHolds(t, "external-work:"+id)
}
