//bifrost:owner delivery

package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The row of ci-platform-pgdsv for bifrost-platform c53ea00d as the PROD
// ledger holds it (read 2026-11-13). inputs.beside holds the siblings' TREES
// (c6b1e24e…, 27bcc703…); the commits a delivery takes as uiRevision and
// infraRevision are in the statement's subjects. Taking beside from inputs
// would hand the pipeline a tree id as a revision.
const pgdsvStatement = `{"_type": "https://in-toto.io/Statement/v1", "subject": [
 {"name": "bifrost-platform", "digest": {"gitTree": "062e22be961b9c20d31eec3d3783a6f0ddf51ea8", "gitCommit": "c53ea00db051530cd38b44ad20a07ee72d5da231"}},
 {"name": "bifrost-trade-infra", "digest": {"gitTree": "27bcc703c429c6c1cefe2049246bac464abf96a6", "gitCommit": "1d5a18421e64982ebe69618371531ab5d2233d47"}},
 {"name": "bifrost-ui", "digest": {"gitTree": "c6b1e24ea050585db898fc8780731ce50a32f10f", "gitCommit": "c83f40b40e65e52f8592ec6b73c5f6987c116e93"}}]}`

func TestBesideIsTheCommitsOfTheRowTheVerdictRestsOn(t *testing.T) {
	rows := []evidence.EvidenceRow{
		// The other passing row of the same commit ran beside another infra
		// commit (ci-platform-5d6bb, cf70db50): it is not the verdict's row.
		{RunName: "ci-platform-5d6bb", Statement: json.RawMessage(`{"subject": [{"name": "bifrost-trade-infra", "digest": {"gitCommit": "cf70db500403b0b03a34842062ef5813b1153cff"}}]}`)},
		{RunName: "ci-platform-pgdsv", Statement: json.RawMessage(pgdsvStatement)},
	}
	got, found := besideOf(rows, "bifrost-platform", "ci-platform-pgdsv")
	if !found || len(got) != 2 || got["bifrost-ui"] != "c83f40b40e65e52f8592ec6b73c5f6987c116e93" || got["bifrost-trade-infra"] != "1d5a18421e64982ebe69618371531ab5d2233d47" {
		t.Fatalf("beside %v found %v", got, found)
	}
	if got, found := besideOf(rows, "bifrost-platform", "ci-platform-zzzzz"); found || got != nil {
		t.Fatalf("a run with no row: %v %v", got, found)
	}
}

func headRun(name, pipeline string, created time.Time, condition string, params map[string]string) unstructured.Unstructured {
	var ps []any
	for k, v := range params {
		ps = append(ps, map[string]any{"name": k, "value": v})
	}
	u := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
		"spec":     map[string]any{"pipelineRef": map[string]any{"name": pipeline}, "params": ps},
	}}
	u.SetCreationTimestamp(metav1.NewTime(created))
	if condition != "" {
		u.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": condition}}}
	}
	return u
}

// The cluster's runs as listed 2026-11-13 22:45Z: STG and PROD deliveries of
// platform side by side, in no order. A deliverable reads only its own
// pipeline's, newest first, and a run with no verdict yet (just created, or
// Unknown while it runs) is running: a second delivery started beside it is
// what failed three times that afternoon.
func TestHeadRunsReadsOnePipelineNewestFirst(t *testing.T) {
	at := time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC)
	items := []unstructured.Unstructured{
		headRun("bifrost-deliver-platform-1794608501", "bifrost-deliver-platform", at.Add(21*time.Minute), "True", map[string]string{"revision": "c53e"}),
		headRun("bifrost-deliver-platform-prod-b5eac58d014a8f97-1", "bifrost-deliver-platform-prod", at.Add(45*time.Minute), "True", map[string]string{"revision": "776f"}),
		headRun("bifrost-deliver-platform-1794609999", "bifrost-deliver-platform", at.Add(50*time.Minute), "", map[string]string{"revision": "aaaa"}),
		headRun("bifrost-deliver-platform-1794609372", "bifrost-deliver-platform", at.Add(36*time.Minute), "True", map[string]string{"revision": "776f", "uiRevision": "c83f"}),
		headRun("bifrost-deliver-platform-1794606777", "bifrost-deliver-platform", at.Add(-3*time.Minute), "False", map[string]string{"revision": "5f56"}),
		headRun("bifrost-deliver-platform-1794609998", "bifrost-deliver-platform", at.Add(49*time.Minute), "Unknown", nil),
	}
	got := headRuns(items, "bifrost-deliver-platform")
	want := []headrelease.Run{
		{Name: "bifrost-deliver-platform-1794609999", Status: headrelease.RunRunning},
		{Name: "bifrost-deliver-platform-1794609998", Status: headrelease.RunRunning},
		{Name: "bifrost-deliver-platform-1794609372", Status: headrelease.RunSucceeded},
		{Name: "bifrost-deliver-platform-1794608501", Status: headrelease.RunSucceeded},
		{Name: "bifrost-deliver-platform-1794606777", Status: headrelease.RunFailed},
	}
	if len(got) != len(want) {
		t.Fatalf("%d runs: %+v", len(got), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Status != want[i].Status {
			t.Errorf("run %d: %s %s, want %s %s", i, got[i].Name, got[i].Status, want[i].Name, want[i].Status)
		}
	}
	if got[2].Params["revision"] != "776f" || got[2].Params["uiRevision"] != "c83f" {
		t.Errorf("params: %v", got[2].Params)
	}
}

// The parameter named revision is the pipeline's own revision and the others
// go beside it (the start refuses a revision that differs from
// params.revision). What stands in the way of a start is said in words that
// reach the record: who holds the window (Codex review 2026-11-13, C1: the
// holder was dropped), or the freeze (B3: while frozen nothing is started, so
// no failed run is left to mark the head "failed once").
func TestHeadWorldStartSplitsTheRevisionAndSaysWhyNotNow(t *testing.T) {
	var gotRevision string
	var gotExtra map[string]string
	held, frozen, calls := "", false, 0
	w := &headWorld{
		start: func(_ context.Context, principal, pipeline, revision string, params map[string]string) (delivery.PipelineRunView, string, error) {
			calls++
			gotRevision, gotExtra = revision, params
			if held != "" {
				return delivery.PipelineRunView{}, held, nil
			}
			return delivery.PipelineRunView{Name: pipeline + "-1"}, "", nil
		},
		frozen: func(context.Context) (bool, string) { return frozen, "incident 12" },
	}
	run, wait, err := w.Start(context.Background(), "bifrost-deliver-platform", map[string]string{"revision": "r", "uiRevision": "u", "infraRevision": "i"})
	if err != nil || wait != "" || run != "bifrost-deliver-platform-1" || gotRevision != "r" || len(gotExtra) != 2 || gotExtra["uiRevision"] != "u" || gotExtra["infraRevision"] != "i" {
		t.Fatalf("start: %q wait=%q %v; revision %q extra %v", run, wait, err, gotRevision, gotExtra)
	}
	held = "the release window is held by biz-align"
	if run, wait, err := w.Start(context.Background(), "bifrost-deliver-platform", map[string]string{"revision": "r"}); err != nil || run != "" || !strings.Contains(wait, "biz-align") {
		t.Fatalf("held: %q wait=%q %v", run, wait, err)
	}
	held, frozen, calls = "", true, 0
	if run, wait, err := w.Start(context.Background(), "bifrost-deliver-platform", map[string]string{"revision": "r"}); err != nil || run != "" || wait != "releases are frozen: incident 12" || calls != 0 {
		t.Fatalf("frozen: %q wait=%q %v, %d starts asked", run, wait, err, calls)
	}
}

// Owner 2026-11-13: the loop asks nobody, so every decision leaves a record
// that says what a start brings and whose it is. The message is the shape
// lineage.sh leaves (platform main 688c63eb's own trailer block): the thread
// is the Claude-Session trailer, and a commit without one (a merger's) is
// listed with none rather than dropped.
func TestHeadBroughtReadsEachCommitsThread(t *testing.T) {
	var with, without headListed
	with.SHA = "688c63eb89a7e6d858d19079e6bcbd23a3deb3cd"
	with.Commit.Message = "feat(delivery): the platform releases main's head to STG by itself (LANE-ci-w1)\n\nbody mentions Work: in a sentence\n\nCo-Authored-By: Claude Fable 5.2 <noreply@anthropic.com>\nClaude-Session: local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff\nClaude-Transcript: 2df547db-4e82-4e11-b33e-82bb6165ccac\nChange-Id: Ifa634ee165f537de54f67adc6568b63b4842f1d3\nWork: LANE-ci-w1\n"
	without.SHA = "88c0ab27"
	without.Commit.Message = "merge by hand"
	got := headBroughtOf("bifrost-platform", []headListed{with, without})
	if len(got) != 2 {
		t.Fatalf("%d commits", len(got))
	}
	if got[0].Session != "local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff" || got[0].Work != "LANE-ci-w1" || got[0].Subject != "feat(delivery): the platform releases main's head to STG by itself (LANE-ci-w1)" || got[0].Repo != "bifrost-platform" {
		t.Errorf("the commit with trailers: %+v", got[0])
	}
	if got[1].Session != "" || got[1].Subject != "merge by hand" {
		t.Errorf("the commit without: %+v", got[1])
	}
}

// 2026-11-16 night: of 564 s from main to STG released, 267 s were waiting
// for the release window; a pass that found it held waited for the next
// 5-minute reconciliation. The window's change now wakes head-release too.
func TestHeadReleaseWakesOnTheWindow(t *testing.T) {
	want := map[string]bool{"git.main.advanced": false, "ci.run.finished": false, "release.window.changed": false}
	for _, typ := range headReleaseWakes {
		want[typ] = true
	}
	for typ, ok := range want {
		if !ok {
			t.Errorf("head-release is not woken by %s", typ)
		}
	}
}

// 2026-11-17: head-release listed every run in cicd on each read (~470
// pipelineruns LISTs in 10 minutes with the PROD shadow). It now asks for its
// pipeline's runs only; a run of another pipeline returned anyway is still
// left out.
func TestHeadReleaseAsksForItsPipelineOnly(t *testing.T) {
	var asked []string
	run := func(name, pipeline string) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": name}, "spec": map[string]any{"pipelineRef": map[string]any{"name": pipeline}}}}
	}
	w := &headWorld{runs: func(_ context.Context, pipeline string) ([]unstructured.Unstructured, error) {
		asked = append(asked, pipeline)
		return []unstructured.Unstructured{run("a", "bifrost-deliver-platform"), run("b", "other")}, nil
	}}
	got, err := w.Runs(context.Background(), "bifrost-deliver-platform")
	if err != nil || len(asked) != 1 || asked[0] != "bifrost-deliver-platform" || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("asked %v, got %+v, err %v", asked, got, err)
	}
}

// 2026-11-17, with the communication layer: the newest event worth a
// head-release pass is the cause a started run names; a start clears it only
// if no newer wake came meanwhile. Positive control: with clearWake storing
// "" unconditionally, the last check sees "" instead of ev-3.
func TestHeadReleaseRemembersTheNewestWake(t *testing.T) {
	var b busWiring
	if b.lastWake() != "" {
		t.Fatal("no wake yet")
	}
	b.clearWake("ev-1") // nothing kept: no panic, nothing changes
	b.noteWake("ev-1")
	b.noteWake("ev-2")
	if b.lastWake() != "ev-2" {
		t.Fatalf("lastWake = %q", b.lastWake())
	}
	b.clearWake("ev-2")
	if b.lastWake() != "" {
		t.Fatalf("a run started with ev-2 left %q", b.lastWake())
	}
	b.noteWake("ev-3")
	b.clearWake("ev-2") // a start that read ev-2 before ev-3 came
	if b.lastWake() != "ev-3" {
		t.Fatalf("a newer wake was lost: %q", b.lastWake())
	}
}
