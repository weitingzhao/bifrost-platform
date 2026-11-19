package coord_test

import (
	"encoding/json"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store" // registers the vocabulary and the artifact checks
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

func statusBody(t *testing.T, summary string, text any) []byte {
	t.Helper()
	raw, ok := text.(string)
	if !ok {
		b, err := json.Marshal(text)
		ok2(t, err)
		raw = string(b)
	}
	b, err := json.Marshal(map[string]any{"v": 1, "type": "status", "summary": summary, "text": raw})
	ok2(t, err)
	return b
}

func ok2(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// 2026-11-17 (the Owner, via the coordinator: "I ask for progress because Ops
// does not show it"; after card #587, no new write route): a seat sends its
// status as a "status" message to the board with the existing message route.
// The board reads each sender's latest status; a status that does not parse
// is listed with its problem and its sender; other messages to the board are
// not statuses and are left out.
func TestSeatStatusesFromMessagesPG(t *testing.T) {
	t.Parallel()
	_, pool, a := conclusionDB(t)
	b := actor.Writer{Actor: "thread:local_other", Token: "other"}
	s := coord.SeatStatus{Goal: "bus", Doing: "mainOf", Percent: 40, PercentBasis: "2 of 5 steps", Next: "release"}
	send := func(w actor.Writer, body []byte) {
		t.Helper()
		_, err := coord.SendMessage(ctx, pool, w, coord.StatusBoard, "note", body)
		ok2(t, err)
	}
	send(a, statusBody(t, "first", s))
	s.Doing, s.Percent, s.WaitingOn = "release", 60, "the release window"
	send(a, statusBody(t, "second", s))
	send(a, []byte("a plain note, not a status"))
	send(b, statusBody(t, "bad", map[string]any{"goal": "g", "doing": "d", "percent": 101, "percent_basis": "b", "next": "n"}))

	got, err := coord.SeatStatuses(ctx, pool, time.Now().Add(-time.Hour))
	ok2(t, err)
	if len(got) != 2 {
		t.Fatalf("%d seats, want 2: %+v", len(got), got)
	}
	by := map[string]coord.SeatStatusRecord{}
	for _, r := range got {
		by[r.Seat] = r
	}
	mine := by[a.Actor]
	if mine.Summary != "second" || mine.Doing != "release" || mine.Percent != 60 || mine.WaitingOn != "the release window" || mine.Problem != "" {
		t.Fatalf("not the latest status: %+v", mine)
	}
	if bad := by[b.Actor]; !strings.Contains(bad.Problem, "percent") || bad.Summary != "bad" {
		t.Fatalf("a malformed status is not named: %+v", bad)
	}
}

func TestParseSeatStatus(t *testing.T) {
	long := strings.Repeat("g", coord.MaxSeatStatusField+1)
	for name, c := range map[string]struct {
		body    string
		ok      bool
		problem string
	}{
		"not json":      {"hello", false, ""},
		"not a status":  {`{"v":1,"type":"note","summary":"s"}`, false, ""},
		"text not json": {`{"v":1,"type":"status","summary":"s","text":"doing things"}`, true, "format: the text"},
		"unknown field": {`{"v":1,"type":"status","summary":"s","text":"{\"goal\":\"g\",\"doing\":\"d\",\"percent\":1,\"percent_basis\":\"b\",\"next\":\"n\",\"seat\":\"thread:x\"}"}`, true, "format: the text"},
		"too long":      {`{"v":1,"type":"status","summary":"s","text":"{\"goal\":\"` + long + `\",\"doing\":\"d\",\"percent\":1,\"percent_basis\":\"b\",\"next\":\"n\"}"}`, true, "format: goal"},
		"no next":       {`{"v":1,"type":"status","summary":"s","text":"{\"goal\":\"g\",\"doing\":\"d\",\"percent\":1,\"percent_basis\":\"b\"}"}`, true, "format: no next"},
		"good":          {`{"v":1,"type":"status","summary":"s","text":"{\"goal\":\"g\",\"doing\":\"d\",\"percent\":1,\"percent_basis\":\"b\",\"next\":\"n\"}"}`, true, ""},
	} {
		_, _, problem, ok := coord.ParseSeatStatus([]byte(c.body))
		if ok != c.ok || !strings.HasPrefix(problem, c.problem) || (c.problem == "" && problem != "") {
			t.Errorf("%s: ok=%v problem=%q", name, ok, problem)
		}
	}
}

// 2026-11-17 (the Owner: he cannot see what the bus's work does): the Bus
// page counts the dispatcher's wakes from the messages themselves — those
// that reached a seat, those that went to the duty seat because the
// recipient was unknown, and how long a seat took to acknowledge one. A
// message that is not a wake is not counted.
func TestWakeReadingsPG(t *testing.T) {
	t.Parallel()
	_, pool, a := conclusionDB(t)
	since := time.Now().Add(-time.Minute)
	ok2(t, coord.PostConclusion(ctx, pool, "sub:land-refused:ev-1", a.Actor, "bus: ci.run.finished land-x@abc (change failed). Read /x."))
	ok2(t, coord.PostConclusion(ctx, pool, "sub:land-refused:ev-2:undeliverable", a.Actor, "bus: a wake could not be delivered: thread:local_gone is not a registered thread here."))
	ok2(t, coord.PostConclusion(ctx, pool, "approval:appr_1:executed", a.Actor, "card #1 executed"))
	unread, err := coord.Unread(ctx, pool, a, 10)
	ok2(t, err)
	var wake string
	for _, m := range unread {
		if strings.HasPrefix(m.Body, "bus: ci.run") {
			wake = m.ID
		}
	}
	_, err = coord.AckMessages(ctx, pool, a, []string{wake})
	ok2(t, err)
	got, err := coord.Wakes(ctx, pool, since)
	ok2(t, err)
	if got.Delivered != 1 || got.Undeliverable != 1 || got.Acked != 1 || got.BySeat[a.Actor] != 1 {
		t.Fatalf("readings: %+v", got)
	}
}
