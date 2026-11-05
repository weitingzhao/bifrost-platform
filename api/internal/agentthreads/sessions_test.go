package agentthreads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// R5:5.5-12 P-c2 (§3, §4.1, S1-3): the side beats, the session's key and its
// diagnoses, each row's binding, the session counts and the list's filters.

type rig2 struct {
	t   *testing.T
	h   *Handler
	rec *Recorder
	clk *clock
}

func newRig2(t *testing.T, read WorksFunc) *rig2 {
	t.Helper()
	clk := &clock{at: t0}
	cfg := DefaultConfig()
	rec := NewRecorder(NewStore(""), cfg)
	rec.now = clk.now
	h := NewHandler(rec, cfg, nil, &memAudit{})
	h.now = clk.now
	if read != nil {
		h.UseWorks(read)
	}
	return &rig2{t: t, h: h, rec: rec, clk: clk}
}

// beat posts one beat as p; fields over a base for thread on host.
func (r *rig2) beat(p actuation.Principal, fields map[string]any) *httptest.ResponseRecorder {
	r.t.Helper()
	body := map[string]any{"vendor": "claude", "host": "h1", "turn_id": "turn-1", "event": "turn_start"}
	for k, v := range fields {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	return as(r.h, p, "POST", "/api/v1/agent/threads/heartbeat", string(raw))
}

func (r *rig2) ok(p actuation.Principal, fields map[string]any) map[string]any {
	r.t.Helper()
	w := r.beat(p, fields)
	if w.Code != http.StatusAccepted {
		r.t.Fatalf("beat %v: %d %s", fields, w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// list reads GET /agent/threads as p with query, its rows by thread id.
func (r *rig2) list(p actuation.Principal, query string) (map[string]map[string]any, string) {
	r.t.Helper()
	w := as(r.h, p, "GET", "/api/v1/agent/threads"+query, "")
	if w.Code != http.StatusOK {
		r.t.Fatalf("list %s: %d %s", query, w.Code, w.Body)
	}
	var body struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		r.t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, row := range body.Threads {
		out[row["vendor"].(string)+"/"+row["thread"].(string)] = row
	}
	return out, w.Body.String()
}

// Enumerations and reasons (§4.1, design §12 D2): the two side beats take
// their closed reasons, fork among a start's; anything else is 400.
func TestSideBeatEnumerationAndReasons(t *testing.T) {
	r := newRig2(t, nil)
	for i, c := range []struct {
		event, reason string
		want          int
	}{
		{"session_start", "startup", 202}, {"session_start", "resume", 202}, {"session_start", "clear", 202}, {"session_start", "compact", 202},
		{"session_start", "fork", 202}, {"session_start", "", 400}, {"session_start", "auto", 400}, {"session_start", "other", 400},
		{"precompact", "auto", 202}, {"precompact", "manual", 202}, {"precompact", "", 400}, {"precompact", "startup", 400},
		{"compact", "auto", 400}, {"session_end", "", 400},
	} {
		w := r.beat(reporterP, map[string]any{"thread": "enum-" + c.event + "-" + c.reason + "-" + string(rune('a'+i)), "seq": 1, "event": c.event, "reason": c.reason})
		if w.Code != c.want {
			t.Errorf("%s %q: %d %s", c.event, c.reason, w.Code, w.Body)
		}
	}
	rows, _ := r.list(viewerP, "")
	if row := rows["claude/enum-session_start-fork-e"]; row["event"] != "session_start" || row["reason"] != "fork" || row["status"] != "idle" {
		t.Fatalf("a fork's start: %v", row)
	}
}

// The side beats belong to no turn (§4.1): a session's start opens none (the
// thread is idle, never silent or paged); a precompact keeps the turn as it
// was, its event, time, tool window and start (it is counted, not shown); a
// start during a wait keeps the wait and its reason; a resent precompact is
// counted once; the next turn event is the turn's.
func TestSideBeatsKeepTheTurn(t *testing.T) {
	r := newRig2(t, nil)
	key := r.ok(reporterP, map[string]any{"thread": "side", "session": "s-side", "epoch": 1, "seq": 1, "event": "session_start", "reason": "startup"})["thread_key"].(string)
	beat := func(seq int, fields map[string]any) map[string]any {
		f := map[string]any{"thread": "side", "session": "s-side", "epoch": 1, "seq": seq, "thread_key": key}
		for k, v := range fields {
			f[k] = v
		}
		return r.ok(reporterP, f)
	}
	r.clk.at = t0.Add(20 * time.Minute)
	rows, _ := r.list(viewerP, "")
	if row := rows["claude/side"]; row["status"] != "idle" || row["event"] != "session_start" {
		t.Fatalf("a started session, 20 minutes on: %v", row)
	}
	if push, mark := r.h.cfg.Due(recState(t, r.rec), r.clk.at); len(push)+len(mark) != 0 {
		t.Fatalf("a started session is paged: %v", push)
	}
	st := recState(t, r.rec)
	if th := st.Threads[Key("claude", "side")]; th.OpenedTurn || !th.idle() {
		t.Fatalf("the start opened a turn: %+v", th)
	}
	beat(2, map[string]any{"event": "turn_start", "turn_id": "turn-2"})
	turnAt := r.clk.at
	beat(3, map[string]any{"event": "before_tool", "turn_id": "turn-2", "tool": "Bash", "tool_timeout_s": 3600})
	r.clk.at = r.clk.at.Add(time.Minute)
	beat(4, map[string]any{"event": "precompact", "reason": "auto", "turn_id": "elsewhere"})
	beat(4, map[string]any{"event": "precompact", "reason": "auto", "turn_id": "elsewhere"}) // resent: ignored
	r.clk.at = r.clk.at.Add(30 * time.Minute)                                                // past ten minutes, inside the tool's window
	rows, _ = r.list(viewerP, "")
	row := rows["claude/side"]
	if row["status"] != "in_turn" || row["event"] != "before_tool" || row["reason"] != nil || row["threshold_seconds"] != float64(3600+120) ||
		row["compactions"] != float64(1) || row["turn_started_at"] != turnAt.Format(time.RFC3339) || row["tool"] != "Bash" || row["at"] != turnAt.Format(time.RFC3339) {
		t.Fatalf("a precompact inside a tool call: %v", row)
	}
	beat(5, map[string]any{"event": "waiting_owner", "reason": "permission_prompt", "turn_id": "turn-2"})
	beat(6, map[string]any{"event": "session_start", "reason": "compact"})
	r.clk.at = r.clk.at.Add(time.Hour)
	rows, _ = r.list(viewerP, "")
	if row := rows["claude/side"]; row["status"] != "waiting_owner" || row["event"] != "waiting_owner" || row["reason"] != "permission_prompt" {
		t.Fatalf("a start during a wait: %v", row)
	}
	beat(7, map[string]any{"event": "after_tool", "turn_id": "turn-2", "tool": "Bash"})
	rows, _ = r.list(viewerP, "")
	if row := rows["claude/side"]; row["event"] != "after_tool" || row["status"] != "in_turn" || row["compactions"] != float64(1) {
		t.Fatalf("the turn goes on after the side beats: %v", row)
	}
}

func recState(t *testing.T, rec *Recorder) State {
	t.Helper()
	st, err := rec.State()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// The session's key and its diagnoses (§3.1, §3.3): first_seen_at for each
// key, an epoch its own; the precompact count and the client's word kept
// with the key and started again with a new one; token_kind written by the
// server; an old client's key its thread id; every field's shape closed.
func TestSessionKeyFields(t *testing.T) {
	r := newRig2(t, nil)
	key := r.ok(collectorH1, map[string]any{"thread": "keyed", "session": "s-key", "epoch": 1, "seq": 1, "event": "session_start", "reason": "startup",
		"client_bind": map[string]any{"state": "refused", "reason": "no_context", "at": t0.Format(time.RFC3339)}, "host_id_source": "file"})["thread_key"].(string)
	next := func(seq int, at time.Time, fields map[string]any) {
		r.clk.at = at
		f := map[string]any{"thread": "keyed", "session": "s-key", "seq": seq, "thread_key": key}
		for k, v := range fields {
			f[k] = v
		}
		r.ok(collectorH1, f)
	}
	next(2, t0.Add(time.Minute), map[string]any{"epoch": 1, "event": "precompact", "reason": "manual"})
	rows, _ := r.list(viewerP, "")
	row := rows["claude/keyed"]
	if row["first_seen_at"] != t0.Format(time.RFC3339) || row["session"] != "s-key" || row["epoch"] != float64(1) || row["compactions"] != float64(1) ||
		row["token_kind"] != "collector" || row["host_id_source"] != "file" || row["client_bind"].(map[string]any)["reason"] != "no_context" {
		t.Fatalf("the key's fields: %v", row)
	}
	next(3, t0.Add(2*time.Minute), map[string]any{"epoch": 2, "event": "session_start", "reason": "resume"})
	rows, _ = r.list(viewerP, "")
	row = rows["claude/keyed"]
	if row["first_seen_at"] != t0.Add(2*time.Minute).Format(time.RFC3339) || row["epoch"] != float64(2) || row["compactions"] != nil || row["client_bind"] != nil ||
		row["host_id_source"] != "file" {
		t.Fatalf("a new epoch: %v", row)
	}
	r.ok(reporterP, map[string]any{"thread": "old-client", "seq": 1, "host": "h2"})
	rows, body := r.list(viewerP, "")
	if row := rows["claude/old-client"]; row["session"] != "old-client" || row["epoch"] != nil || row["token_kind"] != "reporter" {
		t.Fatalf("an old client's row: %v", row)
	}
	if strings.Contains(body, "sidebar") || strings.Contains(body, "mark") {
		t.Fatalf("the list names a stored field it keeps back: %s", body)
	}
	for name, f := range map[string]map[string]any{
		"an epoch without a session":   {"epoch": 1},
		"a negative epoch":             {"session": "s", "epoch": -1},
		"a session of the wrong shape": {"session": "s s"},
		"a client_bind state":          {"client_bind": map[string]any{"state": "maybe"}},
		"a client_bind reason":         {"client_bind": map[string]any{"state": "refused", "reason": "other"}},
		"a client_bind time":           {"client_bind": map[string]any{"state": "linked", "at": "yesterday"}},
		"a host_id_source":             {"host_id_source": "dns"},
	} {
		f["thread"], f["seq"] = "bad-"+strings.ReplaceAll(name, " ", "-"), 1
		if w := r.beat(reporterP, f); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}

// fixedWorks answers list (or err), counting its reads.
func fixedWorks(list []Work, err error, reads *int) WorksFunc {
	return func(context.Context) ([]Work, error) {
		*reads++
		return list, err
	}
}

// The binding of each row (§3.1, §3.2, S1-3): the five states; a key
// recovered under a new thread id still linked; the same session id under
// another vendor apart; the current epoch's work, earlier ones counted as
// history; a client's linked not believed, its refused kept; a record with
// no host unknown; the counts one per key and summing to the keys; the
// runtime read once in ten seconds.
func TestBindingProjection(t *testing.T) {
	works := []Work{
		{ID: "ew_s1", Root: "R5", Node: "R5:5.5-12", State: "Running", Host: "h1", Vendor: "claude", Session: "s1", Epoch: 1},
		{ID: "ew_s2a", Root: "R5", Node: "R5:5.5-12", State: "Submitted", Host: "h1", Vendor: "claude", Session: "s2", Epoch: 1},
		{ID: "ew_s2b", Root: "R5", Node: "R5:5.5-13", State: "Running", Host: "h1", Vendor: "claude", Session: "s2", Epoch: 2},
		{ID: "ew_other_host", Root: "R5", Node: "R5:5.5-12", State: "Running", Host: "h2", Vendor: "claude", Session: "s4", Epoch: 1},
		{ID: "ew_legacy", Root: "R5", Node: "R5:5.5-12", State: "Running", Vendor: "claude", Session: "s9", Epoch: 1},
	}
	reads := 0
	r := newRig2(t, fixedWorks(works, nil, &reads))
	for _, b := range []map[string]any{
		{"thread": "s1", "session": "s1", "epoch": 1},
		{"thread": "s1:1", "session": "s1", "epoch": 1}, // the same key, recovered under a new thread id
		{"thread": "s1", "vendor": "cursor", "session": "s1", "epoch": 1},
		{"thread": "s2", "session": "s2", "epoch": 2},
		{"thread": "s3", "session": "s3", "epoch": 1, "client_bind": map[string]any{"state": "refused", "reason": "no_context"}},
		{"thread": "s4", "session": "s4", "epoch": 1, "client_bind": map[string]any{"state": "linked"}}, // h2's work: not this host's
		{"thread": "s5", "vendor": "codex", "session": "s5", "epoch": 1},
		{"thread": "s9", "session": "s9", "epoch": 1},
		{"thread": "h2-s1", "host": "h2", "session": "s1", "epoch": 1}, // h1's session id on another host: not h1's work
	} {
		b["seq"] = 1
		r.clk.at = r.clk.at.Add(time.Second)
		r.ok(reporterP, b)
	}
	rows, _ := r.list(viewerP, "")
	want := map[string]struct{ state, work, note string }{
		"claude/s1": {"linked", "ew_s1", ""}, "claude/s1:1": {"linked", "ew_s1", ""}, "cursor/s1": {"unlinked", "", ""},
		"claude/s2": {"linked", "ew_s2b", ""}, "claude/s3": {"refused", "", ""}, "claude/s4": {"unlinked", "", ""},
		"codex/s5": {"unsupported", "", NoteCodexBinding}, "claude/s9": {"unknown", "", NoteLegacyNoHost}, "claude/h2-s1": {"unlinked", "", ""},
	}
	for k, w := range want {
		b, _ := rows[k]["binding"].(map[string]any)
		if b["state"] != w.state || w.work != "" && b["external_work_id"] != w.work || (rows[k]["server_note"] == nil) != (w.note == "") || w.note != "" && rows[k]["server_note"] != w.note {
			t.Errorf("%s: binding %v, note %v; want %+v", k, b, rows[k]["server_note"], w)
		}
	}
	if b := rows["claude/s2"]["binding"].(map[string]any); b["history"] != float64(1) || b["plan_node_id"] != "R5:5.5-13" || b["work_state"] != "Running" {
		t.Fatalf("the current epoch's work: %v", b)
	}
	counts, err := r.h.SessionCoverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts.Linked != 2 || counts.Unlinked != 3 || counts.Refused != 1 || counts.Unsupported != 1 || counts.Unknown != 1 ||
		!strings.Contains(counts.Scope, "72 hours") {
		t.Fatalf("counts: %+v", counts)
	}
	if sum := counts.Linked + counts.Unlinked + counts.Refused + counts.Unsupported + counts.Unknown; sum != len(rows)-1 {
		t.Fatalf("the counts sum to %d, the keys are %d", sum, len(rows)-1)
	}
	if reads != 1 {
		t.Fatalf("the runtime was read %d times within ten seconds", reads)
	}
	r.clk.at = r.clk.at.Add(11 * time.Second)
	r.list(viewerP, "")
	if reads != 2 {
		t.Fatalf("the runtime was not read again after ten seconds: %d", reads)
	}
}

// A runtime that cannot be read leaves every binding but a Codex thread's
// unknown, and says so; the counts still sum to the keys.
func TestBindingRuntimeUnreadable(t *testing.T) {
	reads := 0
	r := newRig2(t, fixedWorks(nil, errors.New("connection refused"), &reads))
	for _, b := range []map[string]any{{"thread": "a", "session": "a", "epoch": 1}, {"thread": "b"}, {"thread": "c", "vendor": "codex"}} {
		b["seq"] = 1
		r.ok(reporterP, b)
	}
	rows, body := r.list(viewerP, "")
	for _, k := range []string{"claude/a", "claude/b"} {
		if rows[k]["binding"].(map[string]any)["state"] != "unknown" || rows[k]["server_note"] != NoteUnreadable {
			t.Fatalf("%s: %v", k, rows[k])
		}
	}
	if rows["codex/c"]["binding"].(map[string]any)["state"] != "unsupported" || strings.Contains(body, "connection refused") {
		t.Fatalf("the Codex row, or the error in the answer: %s", body)
	}
	counts, _ := r.h.SessionCoverage(context.Background())
	if counts.Unknown != 2 || counts.Unsupported != 1 || counts.Linked+counts.Unlinked+counts.Refused != 0 {
		t.Fatalf("counts: %+v", counts)
	}
	// Not wired at all: the same.
	bare := newRig2(t, nil)
	bare.ok(reporterP, map[string]any{"thread": "a", "seq": 1})
	if rows, _ := bare.list(viewerP, ""); rows["claude/a"]["server_note"] != NoteUnreadable {
		t.Fatalf("an unwired list: %v", rows["claude/a"])
	}
}

// ?sidebar= and ?session= (§3.3, §5): the rows of a desktop thread, more
// than one session when one was started inside another (design §12 D6),
// each judged on its own key; the rows of a vendor session id, a recovered
// thread id included; both together; a malformed value is 400; the sidebar
// is never in the answer; a collector's filter stays on its host.
func TestThreadFilters(t *testing.T) {
	reads := 0
	r := newRig2(t, fixedWorks([]Work{{ID: "ew_child", Root: "R5", Node: "R5:1", State: "Running", Host: "h1", Vendor: "claude", Session: "child", Epoch: 1}}, nil, &reads))
	const sidebar = "local_0b8e2f4a-aaaa"
	for _, b := range []map[string]any{
		{"thread": "parent", "session": "parent", "epoch": 1, "sidebar": sidebar},
		{"thread": "child", "session": "child", "epoch": 1, "sidebar": sidebar},
		{"thread": "parent:1", "session": "parent", "epoch": 1},
		{"thread": "other", "session": "other", "epoch": 1, "sidebar": "local_other"},
		{"thread": "h2-parent", "session": "parent", "epoch": 1, "sidebar": sidebar, "host": "h2"},
	} {
		b["seq"] = 1
		r.ok(reporterP, b)
	}
	keys := func(rows map[string]map[string]any) string {
		var out []string
		for k := range rows {
			out = append(out, k)
		}
		return strings.Join(sortStrings(out), ",")
	}
	for q, want := range map[string]string{
		"?sidebar=" + sidebar:                     "claude/child,claude/h2-parent,claude/parent",
		"?session=parent":                         "claude/h2-parent,claude/parent,claude/parent:1",
		"?sidebar=" + sidebar + "&session=parent": "claude/h2-parent,claude/parent",
		"?sidebar=local_none":                     "",
	} {
		rows, body := r.list(viewerP, q)
		if keys(rows) != want || strings.Contains(body, sidebar) {
			t.Errorf("%s: %s; want %s (%s)", q, keys(rows), want, body)
		}
	}
	rows, _ := r.list(viewerP, "?sidebar="+sidebar)
	if rows["claude/child"]["binding"].(map[string]any)["state"] != "linked" || rows["claude/parent"]["binding"].(map[string]any)["state"] != "unlinked" {
		t.Fatalf("each session of one sidebar is judged on its own key: %v", rows)
	}
	for _, q := range []string{"?sidebar=", "?session=a%20b", "?sidebar=" + strings.Repeat("x", 129)} {
		if w := as(r.h, viewerP, "GET", "/api/v1/agent/threads"+q, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
	if rows, _ := r.list(collectorH1, "?session=parent"); keys(rows) != "claude/parent,claude/parent:1" {
		t.Fatalf("a collector's filter: %s", keys(rows))
	}
}

func sortStrings(in []string) []string {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	return in
}
