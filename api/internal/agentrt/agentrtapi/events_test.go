package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type eventRow struct {
	ID     int64  `json:"event_id"`
	Stream string `json:"event_stream_id"`
	Type   string `json:"event_type"`
}

func events(t *testing.T, doc []byte) []eventRow {
	t.Helper()
	var out []eventRow
	must(t, json.Unmarshal(doc, &out))
	return out
}

// GET /events without a stream reads across streams (2.5 G6): the latest
// first, a type filter, paging back by before_event_id and on by
// after_event_id; the stream form is as it was.
func TestEventsAcrossStreams(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	var total int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&total))

	all := events(t, get(t, s, "events?limit=500"))
	if len(all) != total {
		t.Fatalf("%d events read, %d stored", len(all), total)
	}
	streams := map[string]bool{}
	for i, e := range all {
		streams[e.Stream] = true
		if i > 0 && all[i-1].ID <= e.ID {
			t.Fatalf("not newest first at %d: %d then %d", i, all[i-1].ID, e.ID)
		}
	}
	if !streams["mission:M-1"] || !streams["plan:R2"] {
		t.Fatalf("streams read: %v", streams)
	}

	// The latest two, then the two before them.
	page := events(t, get(t, s, "events?limit=2"))
	if len(page) != 2 || page[0].ID != all[0].ID || page[1].ID != all[1].ID {
		t.Fatalf("latest page = %+v", page)
	}
	older := events(t, get(t, s, fmt.Sprintf("events?limit=2&before_event_id=%d", page[1].ID)))
	if len(older) != 2 || older[0].ID != all[2].ID || older[1].ID != all[3].ID {
		t.Fatalf("older page = %+v", older)
	}
	// On from an id, oldest first.
	on := events(t, get(t, s, fmt.Sprintf("events?after_event_id=%d&limit=3", all[3].ID)))
	if len(on) != 3 || on[0].ID != all[2].ID || on[2].ID != all[0].ID {
		t.Fatalf("after page = %+v", on)
	}
	// By type.
	proposed := events(t, get(t, s, "events?types=task_proposed,mission_proposed"))
	if len(proposed) == 0 {
		t.Fatal("no proposals read")
	}
	for _, e := range proposed {
		if e.Type != "task_proposed" && e.Type != "mission_proposed" {
			t.Fatalf("a %s read under the type filter", e.Type)
		}
	}
	// The stream form is unchanged.
	if one := events(t, get(t, s, "events?stream=mission:M-1")); len(one) == 0 || one[0].Stream != "mission:M-1" {
		t.Fatalf("the stream form = %+v", one)
	}

	for _, bad := range []string{"events?limit=0", "events?limit=501", "events?limit=x", "events?types=not_a_type",
		"events?after_event_id=1&before_event_id=9", "events?after_event_id=0", "events?after_seq=1"} {
		if code, _ := call(t, s, "viewer", "GET", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	if code, _ := call(t, s, "", "GET", "events", nil); code != http.StatusUnauthorized {
		t.Errorf("an anonymous read = %d", code)
	}
}
