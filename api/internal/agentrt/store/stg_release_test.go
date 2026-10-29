package store

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

var stgTestAt = time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

const stgTestBatch = "stg:bifrost-platform:1"
const stgTestRoot = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
const stgTestRoot2 = "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"

func stgSHA(s string) string { return strings.Repeat(s, 40) }
func stgDig(s string) string { return "sha256:" + strings.Repeat(s, 64) }
func stgTestMember() map[string]any {
	return map[string]any{"root": stgTestRoot, "op": "land:one:1", "R": stgSHA("a")}
}
func stgTestMember2() map[string]any {
	return map[string]any{"root": stgTestRoot2, "op": "land:two:1", "R": stgSHA("2")}
}
func stgClone(v map[string]any) map[string]any {
	b, _ := canon.Encode(v)
	x, _ := canon.Parse(b)
	return x.(map[string]any)
}
func stgRecord(kind string, kv ...any) map[string]any {
	spec := stgSpecs[kind]
	r := map[string]any{"v": "1", "kind": kind, "scope": spec.scope}
	switch spec.scope {
	case "batch":
		r["batch"] = stgTestBatch
	case "ext":
		r["evidence"] = "cluster/cicd/external-1"
	case "grant":
		r["grant"] = "q1"
	}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func stgNote(r map[string]any) Event {
	return Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "STG test", "stg_release": r}}
}

type stgHarness struct {
	t       *testing.T
	entries []StgEntry
	at      time.Time
	samples []stgSample
}
type stgSample struct {
	prefix []StgEntry
	at     time.Time
	record map[string]any
}

func newStgHarness(t *testing.T) *stgHarness { t.Helper(); return &stgHarness{t: t, at: stgTestAt} }
func (h *stgHarness) state() StgState {
	h.t.Helper()
	s, err := StgFold(h.entries)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}
func (h *stgHarness) check(ev Event) (StgRecord, int64, error) {
	h.t.Helper()
	return checkStgReleaseEvent(h.state(), ev, int64(len(h.entries)+1), h.at)
}
func (h *stgHarness) add(r map[string]any) StgEvent {
	h.t.Helper()
	h.samples = append(h.samples, stgSample{slices.Clone(h.entries), h.at, stgClone(r)})
	ev := stgNote(r)
	parsed, dup, err := h.check(ev)
	if err != nil {
		h.t.Fatalf("%s: %v", r["kind"], err)
	}
	if dup > 0 {
		return h.state().Records[dup]
	}
	ev.Body["stg_release"] = map[string]any(parsed)
	seq := int64(len(h.entries) + 1)
	h.entries = append(h.entries, StgEntry{seq, h.at, ev})
	e := StgEvent{seq, h.at, parsed}
	h.at = h.at.Add(time.Minute)
	_ = h.state() // every legal edge is parsed, append-checked and replayed
	return e
}
func (h *stgHarness) reject(r map[string]any, code string) {
	h.t.Helper()
	before := h.state()
	_, _, err := h.check(stgNote(r))
	var se *StgReleaseError
	if !errors.As(err, &se) || code != "" && se.Code != code {
		h.t.Fatalf("%s: error=%v, want StgReleaseError %s", r["kind"], err, code)
	}
	if !reflect.DeepEqual(before, h.state()) {
		h.t.Fatal("rejected edge changed replay state")
	}
}
func (h *stgHarness) raw(body map[string]any) {
	h.t.Helper()
	h.entries = append(h.entries, StgEntry{int64(len(h.entries) + 1), h.at, Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: body}})
	h.at = h.at.Add(time.Minute)
	_ = h.state()
}
func stgTestEvidence(at time.Time, id, source, run string) map[string]any {
	r := stgRecord("evidence", "evidence", id, "source", source, "run", run, "P", stgSHA("b"), "U", stgSHA("c"), "infra", stgSHA("d"),
		"content", map[string]any{"api_digest": stgDig("a"), "console_digest": stgDig("c")},
		"deployed", map[string]any{"pin_sync_ok": true, "workloads": map[string]any{"observed": true}, "at": at.Format(time.RFC3339Nano)}, "health", map[string]any{"status": "ok"})
	if source == "automatic" {
		r["batch"] = stgTestBatch
	}
	return r
}
func stgTestBase() map[string]any {
	return map[string]any{"P": stgSHA("e"), "api_digest": stgDig("a"), "console_digest": stgDig("c"), "evidence": "cluster/cicd/base-uid"}
}
func (h *stgHarness) baseline() {
	r := stgTestEvidence(h.at, "cluster/cicd/base-uid", "external", "base-run")
	r["P"] = stgSHA("e")
	h.add(r)
}
func (h *stgHarness) opened(members ...map[string]any) map[string]any {
	ms := []any{}
	pending := map[string]any{}
	for _, m := range members {
		c := stgClone(m)
		c["gen"] = h.state().generation(m["R"].(string))
		ms = append(ms, c)
		pending[m["R"].(string)] = h.at.Format(time.RFC3339Nano)
	}
	return stgRecord("opened", "members", ms, "first_pending_at", pending)
}
func (h *stgHarness) ci(uid string) map[string]any {
	return stgRecord("ci_accepted", "P", stgSHA("b"), "ci", map[string]any{"name": "ci-run", "uid": uid, "created_at": h.at.Format(time.RFC3339Nano), "platform_commit": stgSHA("b"), "ui_commit": stgSHA("c"), "infra_commit": stgSHA("d"), "pipeline_ref": "bifrost-ci-platform", "repo_url": "https://gitea.example/bifrost/bifrost-platform.git", "read_at": h.at.Format(time.RFC3339Nano)})
}
func (h *stgHarness) snapshot(n int64, ms ...map[string]any) map[string]any {
	set := []any{}
	for _, m := range ms {
		set = append(set, m)
	}
	return stgRecord("snapshot", "try", n, "inputs", map[string]any{"namespace": "cicd", "pipeline": "bifrost-deliver-platform", "revision": stgSHA("b"), "uiRevision": stgSHA("c"), "infraRevision": stgSHA("d"), "params_canonical": map[string]any{"revision": stgSHA("b"), "uiRevision": stgSHA("c"), "infraRevision": stgSHA("d")}}, "base", stgTestBase(), "set", set, "not_before", h.at.Format(time.RFC3339Nano))
}
func stgReady(t *testing.T, ms ...map[string]any) *stgHarness {
	t.Helper()
	h := newStgHarness(t)
	h.baseline()
	h.add(h.opened(ms...))
	h.add(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)))
	h.add(h.ci("ci-uid"))
	h.add(h.snapshot(1, ms...))
	return h
}
func (h *stgHarness) start(n int64) map[string]any {
	return stgRecord("started", "try", n, "intent_seq", 0, "run", fmt.Sprintf("stgrel-1-%d-bbbbbbbb", n), "labels", map[string]any{"bifrost.io/stg-release": "true", "bifrost.io/stg-release-n": "1", "bifrost.io/stg-release-try": fmt.Sprint(n)}, "annotations", map[string]any{"bifrost.io/stg-release-batch": stgTestBatch, "bifrost.io/stg-release-intent": "0"}, "fingerprint", strings.Repeat("0", 64), "spend", []any{}, "lease", map[string]any{"who": fmt.Sprintf("stgrel-bifrost-platform-1-%d", n), "gen": 1}, "deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano))
}
func (h *stgHarness) terminal(n int64, status string) StgEvent {
	h.add(stgRecord("bound", "try", n, "uid", fmt.Sprintf("run-uid-%d", n)))
	return h.add(stgRecord("run_evidence", "try", n, "uid", fmt.Sprintf("run-uid-%d", n), "status", status, "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}, "at", h.at.Format(time.RFC3339Nano)))
}
func (h *stgHarness) effective(m map[string]any, p, validity string) {
	r := stgClone(m)
	r["v"] = "2"
	r["M0"] = stgSHA("e")
	r["head"] = p
	r["validity"] = validity
	r["has_r"] = true
	r["reverted_by"] = ""
	r["verdict"] = "observed"
	r["read_at"] = h.at.Format(time.RFC3339Nano)
	h.raw(map[string]any{"text": "main evidence", "landing_main": r})
}
func (h *stgHarness) block(why string, ms ...map[string]any) StgEvent {
	a := []any{}
	for _, m := range ms {
		a = append(a, m)
	}
	base := stgTestBase()
	if why == "baseline_unknown" {
		base = map[string]any{"P": ""}
	}
	return h.add(stgRecord("blocked", "block", StgBlockID(stgSHA("b"), base["P"].(string), why, "v1"), "why", why, "P", stgSHA("b"), "base", base, "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", map[string]any{"members": a}))
}
func memberRecord(m map[string]any, to string, ref any) map[string]any {
	r := stgRecord("member", "to", to, "ref", ref)
	for k, v := range m {
		r[k] = v
	}
	return r
}
func (h *stgHarness) settle(ms ...map[string]any) {
	a := []any{}
	for _, m := range ms {
		a = append(a, m)
	}
	h.add(stgRecord("settled", "members", a, "why", "complete"))
}

func TestStgSuccessAndIndependentMemberSettlement(t *testing.T) {
	t.Parallel()
	m, m2 := stgTestMember(), stgTestMember2()
	h := stgReady(t, m, m2)
	start := h.add(h.start(1))
	if start.Record.Int("intent_seq") != start.Seq || len(start.Record["spend"].([]any)) != 2 {
		t.Fatal("recorder did not fill intent and all S debits")
	}
	h.reject(h.start(1), "transition")
	h.reject(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""), "transition")
	h.terminal(1, "Succeeded")
	h.add(stgTestEvidence(h.at, "cluster/cicd/run-uid-1", "automatic", start.Record.String("run")))
	verify := h.add(stgRecord("verify", "try", 1, "outcome", "verified", "why", "complete", "evidence", "cluster/cicd/run-uid-1"))
	h.effective(m, stgSHA("b"), "effective")
	h.effective(m2, stgSHA("b"), "unknown")
	h.reject(memberRecord(m2, "delivered", verify.Seq), "transition")
	h.add(memberRecord(m, "delivered", verify.Seq))
	h.reject(memberRecord(m, "delivered", verify.Seq), "transition")
	h.reject(stgRecord("settled", "members", []any{m, m2}, "why", "done"), "transition")
	h.effective(m2, stgSHA("b"), "effective")
	h.add(memberRecord(m2, "delivered", verify.Seq))
	h.settle(m, m2)
	s := h.state()
	if s.Batches[stgTestBatch].Phase != "CLOSED" || s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 || s.Members[stgMember(m)].To != "delivered" {
		t.Fatal("wrong final state", s.Batches[stgTestBatch])
	}
	h.reject(stgRecord("diag", "try", 1, "what", "lease_lost", "gen", 1), "transition")
}

func TestStgSchemaClosedAndTyped(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	good := h.start(1)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"version", func(r map[string]any) { r["v"] = 1 }}, {"scope", func(r map[string]any) { r["scope"] = "ext" }},
		{"unknown field", func(r map[string]any) { r["extra"] = true }}, {"missing field", func(r map[string]any) { delete(r, "deadline") }},
		{"fractional try", func(r map[string]any) { r["try"] = 1.5 }}, {"negative try", func(r map[string]any) { r["try"] = -1 }},
		{"lease unknown field", func(r map[string]any) { r["lease"].(map[string]any)["extra"] = true }},
		{"spend duplicate", func(r map[string]any) {
			m := map[string]any{"R": stgSHA("a"), "gen": 1, "n": 1}
			r["spend"] = []any{m, m}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := stgClone(good)
			tc.change(r)
			_, err := ParseStgRelease(r)
			var se *StgReleaseError
			if !errors.As(err, &se) || se.Code != "invalid" {
				t.Fatal(err)
			}
		})
	}
	if _, err := ParseStgRelease(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Event){func(e *Event) { e.Actor = "owner" }, func(e *Event) { e.Type = "pipeline_seen" }, func(e *Event) { e.Stream = "landing:other" }, func(e *Event) { e.TaskID = stgTestRoot }, func(e *Event) { e.Body["landing"] = map[string]any{} }} {
		ev := stgNote(stgClone(good))
		change(&ev)
		_, _, err := h.check(ev)
		var se *StgReleaseError
		if !errors.As(err, &se) || se.Code != "permission" {
			t.Fatal(err)
		}
	}
	h.add(good)
}

func TestStgResendUnsentAndRetryBudget(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	first := h.add(h.start(1))
	resend := stgRecord("resend", "try", 1, "intent_seq", first.Seq, "lease", map[string]any{"who": "stgrel-bifrost-platform-1-1", "gen": 2}, "deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano), "why", "not_found_unbound")
	h.reject(resend, "transition")
	h.at = first.Record.Time("deadline")
	resend["deadline"] = h.at.Add(2 * time.Minute).Format(time.RFC3339Nano)
	h.add(resend)
	h.reject(resend, "transition")
	if h.state().Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 {
		t.Fatal("resend charged again")
	}
	h.at = h.at.Add(3 * time.Minute)
	h.add(stgRecord("unsent", "try", 1, "proof", "gate_failed_before_send"))
	h.reject(stgRecord("bound", "try", 1, "uid", "late"), "transition")
	h.reject(stgRecord("retry", "try", 2, "not_before", h.at.Format(time.RFC3339Nano)), "budget")
	for n := int64(2); n <= 3; n++ {
		due := h.state().Debits[stgSHA("a")].NotBefore
		h.add(stgRecord("retry", "try", n, "not_before", due.Format(time.RFC3339Nano)))
		h.at = due
		snap := h.snapshot(n, m)
		bad := stgClone(snap)
		bad["inputs"].(map[string]any)["infraRevision"] = stgSHA("f")
		bad["inputs"].(map[string]any)["params_canonical"].(map[string]any)["infraRevision"] = stgSHA("f")
		h.reject(bad, "conflict")
		h.add(snap)
		h.add(h.start(n))
		h.terminal(n, "Failed")
		h.add(stgRecord("verify", "try", n, "outcome", "failed", "why", "run_failed", "evidence", ""))
		h.add(stgRecord("effect", "try", n, "class", "unknown", "workloads", map[string]any{"error": "timeout"}, "at", h.at.Format(time.RFC3339Nano)))
		h.reject(stgRecord("retry", "try", n+1, "not_before", h.at.Add(time.Hour).Format(time.RFC3339Nano)), "transition")
		h.add(stgRecord("effect", "try", n, "class", "this_try", "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)))
	}
	h.reject(stgRecord("retry", "try", 4, "not_before", h.at.Add(time.Hour).Format(time.RFC3339Nano)), "budget")
	if h.state().Budget[StgBudgetKey{stgSHA("a"), 1}] != 3 {
		t.Fatal("unsent refunded or retry debit was lost")
	}
}

func TestStgQuarantineExternalCoverageDoesNotReleaseExecution(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	start := h.add(h.start(1))
	h.add(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
	count := len(h.entries)
	h.add(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
	if len(h.entries) != count {
		t.Fatal("same UID wrote twice")
	}
	h.reject(stgRecord("bound", "try", 1, "uid", "other"), "conflict")
	h.add(stgRecord("quarantined", "try", 1, "why", "object_lost", "at", h.at.Format(time.RFC3339Nano)))
	h.add(stgTestEvidence(h.at, "cluster/cicd/external-1", "external", "manual-run"))
	h.effective(m, stgSHA("b"), "effective")
	c := stgRecord("covers")
	for k, v := range m {
		c[k] = v
	}
	h.add(c)
	h.reject(memberRecord(m, "covered", "cluster/cicd/external-1"), "transition")
	h.reject(stgRecord("settled", "members", []any{m}, "why", "covered"), "transition")
	h.reject(stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", "cluster/cicd/external-1"), "transition")
	h.add(stgRecord("run_evidence", "try", 1, "uid", "run-uid-1", "status", "Succeeded", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano)))
	h.add(memberRecord(m, "covered", "cluster/cicd/external-1"))
	h.reject(stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", "cluster/cicd/external-1"), "conflict")
	h.add(stgDeploymentCheck(h.at, "cluster/cicd/external-1"))
	h.add(stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", "cluster/cicd/external-1"))
	h.settle(m)
	if start.Seq == 0 || h.state().Batches[stgTestBatch].Tries[1].IsQuarantined() {
		t.Fatal("terminal evidence did not end quarantine")
	}
}
