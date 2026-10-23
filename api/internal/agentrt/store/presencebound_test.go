package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// fill appends n notes unrelated to presence to stream, chained as the
// guards check (seq and prev_sha256; the digests are not the canonical ones,
// which no test of these streams verifies).
func (w *world) fill(t *testing.T, stream string, n int) {
	t.Helper()
	_, err := w.rt.Exec(ctx, fmt.Sprintf(`DO $$
	DECLARE last bigint; prev text; next text;
	BEGIN
	  INSERT INTO agentrt.event_stream (event_stream_id) VALUES (%[1]s) ON CONFLICT DO NOTHING;
	  FOR i IN 1..%[2]d LOOP
	    SELECT last_seq, last_sha256 INTO last, prev FROM agentrt.event_stream WHERE event_stream_id = %[1]s FOR UPDATE;
	    next := encode(sha256(convert_to('unrelated ' || last || prev, 'UTF8')), 'hex');
	    INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
	      VALUES (%[1]s, last + 1, 'note', 'controller', '{"text": "an unrelated note"}', prev, next, clock_timestamp());
	  END LOOP;
	END $$`, "'"+strings.ReplaceAll(stream, "'", "''")+"'", n))
	ok(t, err)
}

// planNode is a node of EXPLAIN (FORMAT JSON).
type planNode struct {
	Type     string     `json:"Node Type"`
	Relation string     `json:"Relation Name"`
	Index    string     `json:"Index Name"`
	Hit      int        `json:"Shared Hit Blocks"`
	Read     int        `json:"Shared Read Blocks"`
	Plans    []planNode `json:"Plans"`
}

func (n planNode) walk(f func(planNode)) {
	f(n)
	for _, c := range n.Plans {
		c.walk(f)
	}
}

// explain runs q under EXPLAIN (ANALYZE, BUFFERS) and returns its plan and
// the text form, for the log.
func (w *world) explain(t *testing.T, q string, args ...any) (planNode, string) {
	t.Helper()
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+q, args...).Scan(&raw))
	var doc []struct {
		Plan planNode `json:"Plan"`
	}
	ok(t, json.Unmarshal(raw, &doc))
	rows, err := w.rt.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF, SUMMARY OFF) `+q, args...)
	ok(t, err)
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return doc[0].Plan, strings.Join(lines, "\n")
}

// Review 526 B2: the presence pass's reads of history are bounded. With
// 1,000 and then 11,000 unrelated notes on each host's stream, the
// controller's and a mission's (h2, a host that never polled, has no offers
// stream; the executors, which never turned, have no presence streams), the
// hosts' document, the presence streams' heads and a host's latest claim
// read no event outside a stream's head: no sequential scan of the events,
// and as many buffers with eleven times the notes.
func TestPresenceReadsAreBounded(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1", "h2")
	w.poll(t, "h1", claude, codex)
	presenceAt(t, w, time.Time{})
	admin := w.db.Connect(t, w.db.Admin)
	streams := []string{store.HostPresenceStream("h1"), store.HostPresenceStream("h2"),
		store.ExecutorPresenceStream("exec:claude"), store.ExecutorPresenceStream("exec:codex")}
	type read struct {
		name, q string
		args    []any
	}
	now := time.Now()
	reads := []read{
		{"hosts", store.HostsSQL("$4::timestamptz"), append(store.LivenessArgs(time.Time{}), now)},
		{"presence heads", store.OpenAlertsSQL, []any{streams}},
		{"latest claim h1", store.LatestClaimSQL, []any{store.HostOffersStream("h1")}},
		{"latest claim h2 (never recorded)", store.LatestClaimSQL, []any{store.HostOffersStream("h2")}},
	}
	measure := func() map[string]int {
		_, err := admin.Exec(ctx, `ANALYZE agentrt.event; ANALYZE agentrt.event_stream`)
		ok(t, err)
		out := map[string]int{}
		for _, r := range reads {
			plan, text := w.explain(t, r.q, r.args...)
			plan.walk(func(n planNode) {
				if n.Relation == "event" && n.Type == "Seq Scan" {
					t.Fatalf("%s scans the events:\n%s", r.name, text)
				}
			})
			out[r.name] = plan.Hit + plan.Read
			t.Logf("%s (%d buffers):\n%s", r.name, plan.Hit+plan.Read, text)
		}
		return out
	}
	unrelated := []string{"host:h1", "host:h2", store.ControllerStream, store.MissionStream("M-1")}
	for _, s := range unrelated {
		w.fill(t, s, 1000)
	}
	before := measure()
	for _, s := range unrelated {
		w.fill(t, s, 10000)
	}
	after := measure()
	for name, b := range before {
		// A B-tree may gain a level under the notes: one buffer more per
		// probe, two probes per stream read.
		if after[name] > b+2*len(streams) {
			t.Fatalf("%s read %d buffers with 44,000 unrelated notes, %d with 4,000", name, after[name], b)
		}
	}
}
