package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type eventRow struct {
	ID     int64  `json:"event_id"`
	Stream string `json:"event_stream_id"`
	Type   string `json:"event_type"`
}

type eventPage struct {
	Events []eventRow `json:"events"`
	Next   string     `json:"next"`
}

func page(t *testing.T, s *httptest.Server, path string) eventPage {
	t.Helper()
	var out eventPage
	must(t, json.Unmarshal(get(t, s, path), &out))
	if out.Next == "" {
		t.Fatalf("%s gave no next cursor", path)
	}
	return out
}

func ids(events []eventRow) []int64 {
	out := make([]int64, 0, len(events))
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

// readOn reads on from a cursor, limit at a time, until a page is short; it
// fails on an event listed twice.
func readOn(t *testing.T, s *httptest.Server, from, query string, limit int) ([]int64, string) {
	t.Helper()
	var got []int64
	seen := map[int64]bool{}
	for i := 0; ; i++ {
		p := page(t, s, fmt.Sprintf("events?after=%s&limit=%d%s", from, limit, query))
		for _, id := range ids(p.Events) {
			if seen[id] {
				t.Fatalf("event %d listed twice reading on", id)
			}
			seen[id] = true
			got = append(got, id)
		}
		from = p.Next
		if len(p.Events) < limit || i > 100 {
			return got, from
		}
	}
}

// readAllOn reads on from a cursor until it has listed want: other tests'
// transactions, anywhere in the cluster, may hold the horizon a moment. It
// fails on an event listed twice.
func readAllOn(t *testing.T, s *httptest.Server, from, query string, limit int, want []int64) []int64 {
	t.Helper()
	var got []int64
	seen := map[int64]bool{}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		more, next := readOn(t, s, from, query, limit)
		for _, id := range more {
			if seen[id] {
				t.Fatalf("event %d listed twice reading on", id)
			}
			seen[id] = true
		}
		got, from = append(got, more...), next
		sorted := slices.Clone(got)
		slices.Sort(sorted)
		if slices.Equal(sorted, want) || time.Now().After(deadline) {
			return sorted
		}
	}
}

func sortedIDs(t *testing.T, pool *pgxpool.Pool, where string) []int64 {
	t.Helper()
	var out []int64
	rows, err := pool.Query(ctx, `SELECT event_id FROM agentrt.event WHERE `+where+` ORDER BY event_id`)
	must(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int64
		must(t, rows.Scan(&id))
		out = append(out, id)
	}
	return out
}

// GET /events without a stream reads across streams (2.5 G6): the latest
// first, a type filter, paging back by before_event_id and on by the cursor
// it gives; the stream form is as it was.
func TestEventsAcrossStreams(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	all := sortedIDs(t, pool, `true`)
	proposals := sortedIDs(t, pool, `event_type IN ('task_proposed', 'mission_proposed', 'actor_registered')`)
	if len(proposals) < 3 || len(proposals) == len(all) {
		t.Fatalf("the fixture needs proposals among other events: %d of %d", len(proposals), len(all))
	}

	latest := page(t, s, "events?limit=500")
	newest := slices.Clone(all)
	slices.Reverse(newest)
	if !slices.Equal(ids(latest.Events), newest) {
		t.Fatalf("latest = %v, want %v", ids(latest.Events), newest)
	}
	streams := map[string]bool{}
	for _, e := range latest.Events {
		streams[e.Stream] = true
	}
	if !streams["mission:M-1"] || !streams["plan:R2"] {
		t.Fatalf("streams read: %v", streams)
	}

	// Back, two at a time, with and without a type filter: every event once, newest first.
	for _, c := range []struct {
		types string
		want  []int64
	}{{"", all}, {"&types=task_proposed,mission_proposed,actor_registered", proposals}} {
		var got []int64
		p := page(t, s, "events?limit=2"+c.types)
		for len(p.Events) > 0 {
			got = append(got, ids(p.Events)...)
			p = page(t, s, fmt.Sprintf("events?limit=2&before_event_id=%d%s", p.Events[len(p.Events)-1].ID, c.types))
		}
		want := slices.Clone(c.want)
		slices.Reverse(want)
		if !slices.Equal(got, want) {
			t.Fatalf("back%s = %v, want %v", c.types, got, want)
		}
	}

	// On from the start, three at a time, with and without the filter: every event once.
	for _, c := range []struct {
		types string
		want  []int64
	}{{"", all}, {"&types=task_proposed,mission_proposed,actor_registered", proposals}} {
		if got := readAllOn(t, s, "0.0", c.types, 3, c.want); !slices.Equal(got, c.want) {
			t.Fatalf("on%s = %v, want %v", c.types, got, c.want)
		}
	}
	// On from the latest read: no event it did not show (it showed them all).
	got, _ := readOn(t, s, latest.Next, "", 500)
	for _, id := range got {
		if !slices.Contains(all, id) {
			t.Fatalf("on from the latest read listed %d, not stored then", id)
		}
	}
	// A type of the vocabulary no event has yet is read, not refused (review 350 A1).
	if p := page(t, s, "events?types=task_cancelled"); len(p.Events) != 0 {
		t.Fatalf("task_cancelled = %v", p.Events)
	}

	// The stream form is unchanged.
	var one []eventRow
	must(t, json.Unmarshal(get(t, s, "events?stream=mission:M-1"), &one))
	if len(one) == 0 || one[0].Stream != "mission:M-1" {
		t.Fatalf("the stream form = %+v", one)
	}

	for _, bad := range []string{"events?limit=0", "events?limit=501", "events?limit=x", "events?types=not_a_type",
		"events?after=0.0&before_event_id=9", "events?before_event_id=0", "events?after=5", "events?after=-1.0",
		"events?after=1.x", "events?after_seq=1"} {
		if code, _ := call(t, s, "viewer", "GET", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	if code, _ := call(t, s, "", "GET", "events", nil); code != http.StatusUnauthorized {
		t.Errorf("an anonymous read = %d", code)
	}
}

// An event that takes its id first and commits after another stream's later
// one is read on, not skipped (review 350 A2): reading on is by transaction,
// and an event shows once every older transaction has ended.
func TestEventsReadOnSeesALateCommit(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	all := sortedIDs(t, pool, `true`)
	readAllOn(t, s, "0.0", "", 500, all)
	_, from := readOn(t, s, "0.0", "", 500)

	appendIn := func(tx pgx.Tx, stream, typ string, body map[string]any) int64 {
		t.Helper()
		ev, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: typ, Actor: "controller", Body: body})
		must(t, err)
		return ev.EventID
	}
	early, err := pool.Begin(ctx)
	must(t, err)
	defer func() { _ = early.Rollback(ctx) }()
	first := appendIn(early, "system", "legacy_imported", map[string]any{"source": "test: the early one"})
	var second int64
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		second = appendIn(tx, store.PlanStream("R2"), "plan_change_proposed", map[string]any{"changes": []any{map[string]any{"op": "retitle"}},
			"reason": "test: the later one", "standing": false, "question_id": "q_late"})
		return nil
	}))
	if first >= second {
		t.Fatalf("ids %d then %d: the case needs the early transaction's id first", first, second)
	}
	// While the early one runs, the later one is held back, not listed past it.
	got, held := readOn(t, s, from, "", 500)
	if slices.Contains(got, second) || slices.Contains(got, first) {
		t.Fatalf("listed %v while %d's transaction runs", got, first)
	}
	must(t, early.Commit(ctx))
	// Both, once the early one ends (other tests' transactions may hold the horizon a moment).
	var after []int64
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		more, next := readOn(t, s, held, "", 500)
		after, held = append(after, more...), next
		if slices.Contains(after, first) && slices.Contains(after, second) || time.Now().After(deadline) {
			break
		}
	}
	if !slices.Contains(after, first) || !slices.Contains(after, second) {
		t.Fatalf("read on after the commit: %v, want %d and %d", after, first, second)
	}
}
