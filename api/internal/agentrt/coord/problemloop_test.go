package coord_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

func verdictBody(t *testing.T, summary string, text any) []byte {
	t.Helper()
	raw, ok := text.(string)
	if !ok {
		b, err := json.Marshal(text)
		ok2(t, err)
		raw = string(b)
	}
	b, err := json.Marshal(map[string]any{"v": 1, "type": "problem_verdict", "summary": summary, "text": raw})
	ok2(t, err)
	return b
}

// 2026-11-18, OWNER_LAW #36: a verdict is read strictly. Unknown fields, a
// word outside the lists, a malformed event id and an over-long ref are
// invalid (listed with their sender, never counted); a message that is not a
// verdict at all (a status, plain text) is left alone. Positive control: the
// exact text the letters ask for, filled in, passes.
func TestParseProblemVerdict(t *testing.T) {
	long := strings.Repeat("r", coord.MaxVerdictRef+1)
	for name, c := range map[string]struct {
		body    string
		ok      bool
		problem string
	}{
		"valid":            {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real\",\"outcome\":\"handled\",\"ref\":\"abc\"}"}`, true, ""},
		"valid no ref":     {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"false\",\"outcome\":\"open\"}"}`, true, ""},
		"unknown field":    {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real\",\"outcome\":\"handled\",\"x\":1}"}`, true, "format"},
		"bad verdict":      {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real|false\",\"outcome\":\"handled\"}"}`, true, "verdict is real or false"},
		"bad outcome":      {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real\",\"outcome\":\"done\"}"}`, true, "outcome"},
		"bad event":        {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"1\",\"verdict\":\"real\",\"outcome\":\"open\"}"}`, true, "event"},
		"long ref":         {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real\",\"outcome\":\"open\",\"ref\":\"` + long + `\"}"}`, true, "ref"},
		"text not json":    {`{"v":1,"type":"problem_verdict","summary":"s","text":"real"}`, true, "format"},
		"two documents":    {`{"v":1,"type":"problem_verdict","summary":"s","text":"{\"event\":\"ev-1\",\"verdict\":\"real\",\"outcome\":\"open\"}{}"}`, true, "more than"},
		"wrong version":    {`{"v":2,"type":"problem_verdict","summary":"s","text":"{}"}`, true, "v is 1"},
		"two-line summary": {`{"v":1,"type":"problem_verdict","summary":"a\nb","text":"{}"}`, true, "summary"},
		"a status":         {`{"v":1,"type":"status","summary":"s","text":"{}"}`, false, ""},
		"plain":            {`hello`, false, ""},
	} {
		_, _, problem, ok := coord.ParseProblemVerdict([]byte(c.body))
		if ok != c.ok || (c.problem == "") != (problem == "") || !strings.Contains(problem, c.problem) {
			t.Errorf("%s: ok=%v problem=%q", name, ok, problem)
		}
	}
}

// 2026-11-18, OWNER_LAW #36: the loop is read from the letters (messages the
// controller sent for the rows that ask) and the verdicts (notes to the
// board). Guards: a letter matches its verdict by the event id in its body;
// the latest valid verdict wins and keeps who and when; an invalid one is
// listed with its sender and counts for nothing; closed is real+handled,
// real+source_fixed or false+source_fixed; a drill's letter, an undeliverable notice and a row
// that does not ask are not cases; the counts are computed from the cases.
func TestProblemLoopPG(t *testing.T) {
	t.Parallel()
	_, pool, a := conclusionDB(t)
	b := actor.Writer{Actor: "thread:local_other", Token: "other"}
	to := a.Actor
	letter := func(row, typ, subject string, n int64) string {
		return "bus: " + typ + " " + subject + " (change firing). Read /api/v1/x. [subscription " + row + ", event ev-" + itoa(n) + "]\nJudge it: ... problem_verdict"
	}
	post := func(key, body string, n int64) {
		t.Helper()
		ok2(t, coord.PostConclusionCaused(ctx, pool, key, to, body, n))
	}
	post("sub:alert-firing-to-duty:ev-1", letter("alert-firing-to-duty", "alert.rule.fired", "KubePodCrashLooping@monitoring", 1), 1)
	post("sub:alert-firing-to-duty:ev-2", letter("alert-firing-to-duty", "alert.rule.fired", "Flap@monitoring", 2), 2)
	post("sub:alert-firing-to-duty:ev-3", letter("alert-firing-to-duty", "alert.rule.fired", "DrillAlert@drill", 3), 3)
	post("sub:deploy-failed-to-duty:ev-4", letter("deploy-failed-to-duty", "ci.run.finished", "bifrost-deliver-x-9@"+strings.Repeat("a", 40), 4), 4)
	post("sub:deploy-failed-to-duty:ev-4:undeliverable", "bus: a wake could not be delivered: thread:x is not a registered thread here. "+letter("deploy-failed-to-duty", "ci.run.finished", "bifrost-deliver-x-9@"+strings.Repeat("a", 40), 4), 4)
	post("sub:other-row:ev-5", letter("other-row", "ci.run.finished", "bifrost-deliver-y-1@"+strings.Repeat("b", 40), 5), 5)
	// A letter from before the loop asked nothing, so it is not a case.
	post("sub:alert-firing-to-duty:ev-6", "bus: alert.rule.fired Old@monitoring (change firing). Read /api/v1/x. [subscription alert-firing-to-duty, event ev-6]", 6)
	kinds := map[string]string{"alert-firing-to-duty": "alert", "deploy-failed-to-duty": "delivery"}

	send := func(w actor.Writer, body []byte) {
		t.Helper()
		_, err := coord.SendMessage(ctx, pool, w, coord.StatusBoard, "note", body)
		ok2(t, err)
	}
	// ev-1: first "real/open", then "real/handled" by the other seat: the latest wins, closed.
	send(a, verdictBody(t, "looking", map[string]string{"event": "ev-1", "verdict": "real", "outcome": "open"}))
	send(b, verdictBody(t, "fixed", map[string]string{"event": "ev-1", "verdict": "real", "outcome": "handled", "ref": "abc123"}))
	// ev-2: a false alarm, not fixed at the source: judged, still open.
	send(a, verdictBody(t, "noise", map[string]string{"event": "ev-2", "verdict": "false", "outcome": "handled"}))
	// An invalid verdict on ev-4 (a word outside the list) must not count; a later valid one is not needed.
	send(b, verdictBody(t, "bad", map[string]string{"event": "ev-4", "verdict": "real|false", "outcome": "open"}))
	send(a, []byte("a plain note"))

	// The receiver acknowledged the letter of ev-1 only.
	mail, err := coord.Unread(ctx, pool, a, 100)
	ok2(t, err)
	for _, m := range mail {
		if strings.Contains(m.Body, "event ev-1]") {
			_, err = coord.AckMessages(ctx, pool, a, []string{m.ID})
			ok2(t, err)
		}
	}

	got, err := coord.ProblemLoop(ctx, pool, time.Now().Add(-time.Hour), kinds)
	ok2(t, err)
	by := map[string]coord.ProblemCase{}
	for _, c := range got.Cases {
		by[c.Event] = c
	}
	if len(got.Cases) != 3 {
		t.Fatalf("%d cases, want 3 (ev-1, ev-2, ev-4): %+v", len(got.Cases), got.Cases)
	}
	if c := by["ev-1"]; !c.Closed || c.State != "closed" || c.VerdictBy != b.Actor || c.Ref != "abc123" || c.Acked == nil || c.Kind != "alert" || c.Receiver != to || c.VerdictAt == nil {
		t.Errorf("ev-1: %+v", c)
	}
	if c := by["ev-2"]; c.Closed || c.State != "judged" || c.Verdict != "false" || c.Acked != nil {
		t.Errorf("ev-2: %+v", c)
	}
	if c := by["ev-4"]; c.Verdict != "" || c.State != "sent" || c.Kind != "delivery" {
		t.Errorf("ev-4: %+v", c)
	}
	n := got.Counts
	if n.Raised != 3 || n.Seen != 1 || n.Judged != 2 || n.Real != 1 || n.False != 1 || n.Closed != 1 || n.Open != 2 || n.AckMedianMinutes == nil || n.VerdictMedianMinutes == nil {
		t.Errorf("counts = %+v", n)
	}
	if len(got.Invalid) != 1 || got.Invalid[0].Sender != b.Actor || !strings.Contains(got.Invalid[0].Problem, "verdict") {
		t.Errorf("invalid = %+v", got.Invalid)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
