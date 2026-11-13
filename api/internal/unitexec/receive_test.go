package unitexec

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// realRuns are the four TaskRuns the cluster returned on 2026-11-13 (less
// the resolved task spec), each given the labels a dispatched unit task
// carries: those runs were started by hand and had none.
func realRuns(t *testing.T) map[string]TaskRun {
	t.Helper()
	raw, err := os.ReadFile("testdata/real_taskruns.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Items []TaskRun `json:"items"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]TaskRun{}
	for _, tr := range doc.Items {
		name := tr.Metadata.Labels["tekton.dev/pipelineTask"]
		key := strings.Repeat("ab", 32)
		unit := "api/internal/unitplan"
		if name == "pass" {
			unit = "api/internal/agentrt/store"
		}
		for k, v := range map[string]string{LabelPlan: strings.Repeat("c", 16), LabelRepo: "bifrost-platform", LabelTask: "test", LabelKey: key[:32], LabelAttempt: "1"} {
			tr.Metadata.Labels[k] = v
		}
		if tr.Metadata.Annotations == nil {
			tr.Metadata.Annotations = map[string]string{}
		}
		tr.Metadata.Annotations[AnnotationUnit], tr.Metadata.Annotations[AnnotationKey] = unit, key
		out[name] = tr
	}
	if len(out) != 4 {
		t.Fatalf("the fixture has %d runs, want pass, test-red, build-red and bad-image", len(out))
	}
	return out
}

func TestReceiveTheFourRealRuns(t *testing.T) {
	runs := realRuns(t)
	intOf := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	for name, want := range map[string]struct {
		class, reason                string
		exit, expected, ran, skipped int
		failed                       []string
		shard, total, run            int
	}{
		"pass":      {store.UnitRunPassed, "Succeeded", 0, 169, 169, 0, nil, 0, 8, 39},
		"test-red":  {store.UnitRunTest, "Failed", 1, 5, 5, 0, []string{"github.com/weitingzhao/bifrost-platform/api/internal/unitplan.TestProbeRed"}, 0, 1, 2},
		"build-red": {store.UnitRunBuild, "Failed", 1, -1, -1, -1, nil, 0, 1, 0},
		"bad-image": {store.UnitRunInfra, "TaskRunImagePullFailed", -1, -1, -1, -1, nil, 0, 1, -1},
	} {
		row, done, err := Receive(runs[name])
		if err != nil || !done {
			t.Fatalf("%s: done %v, %v", name, done, err)
		}
		if row.Class != want.class || row.Reason != want.reason {
			t.Errorf("%s: %s / %s, want %s / %s", name, row.Class, row.Reason, want.class, want.reason)
		}
		if intOf(row.ExitCode) != want.exit || intOf(row.TestsExpected) != want.expected || intOf(row.TestsRan) != want.ran || intOf(row.TestsSkipped) != want.skipped {
			t.Errorf("%s: exit %d, tests %d %d %d", name, intOf(row.ExitCode), intOf(row.TestsExpected), intOf(row.TestsRan), intOf(row.TestsSkipped))
		}
		if strings.Join(row.FailedTests, ",") != strings.Join(want.failed, ",") || row.FailedTestsCut {
			t.Errorf("%s: failed tests %v (cut %v)", name, row.FailedTests, row.FailedTestsCut)
		}
		if row.ShardIndex != want.shard || row.ShardTotal != want.total || intOf(row.RunSeconds) != want.run {
			t.Errorf("%s: part %d of %d, %d seconds", name, row.ShardIndex, row.ShardTotal, intOf(row.RunSeconds))
		}
		if row.CommitSHA != "9e3b11168949bc110db7106bdb03cd099f927142" || row.RunUID == "" || row.CompletedAt == nil {
			t.Errorf("%s: commit %s, uid %q, completed %v", name, row.CommitSHA, row.RunUID, row.CompletedAt)
		}
		if infra := row.Class == store.UnitRunInfra; infra != (row.QueuedSeconds == nil && row.FetchSeconds == nil) {
			t.Errorf("%s: an infra run takes no step times, a judged run takes them: queued %v fetch %v", name, row.QueuedSeconds, row.FetchSeconds)
		}
	}
	if row, _, _ := Receive(runs["bad-image"]); !strings.Contains(row.Message, "ErrImagePull") || len(row.Message) > messageMax {
		t.Errorf("the image failure keeps Tekton's message, cut: %d bytes %q", len(row.Message), row.Message)
	}
}

// What the four real runs did not show, by the same rule: nothing but the
// run's own word makes it a judged run.
func TestReceiveTakesOnlyTheRunsOwnWord(t *testing.T) {
	runs := realRuns(t)
	edit := func(name string, f func(*TaskRun)) TaskRun {
		raw, _ := json.Marshal(runs[name])
		var tr TaskRun
		if err := json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(err)
		}
		f(&tr)
		return tr
	}
	setResult := func(tr *TaskRun, name, value string) {
		for i := range tr.Status.Results {
			if tr.Status.Results[i].Name == name {
				tr.Status.Results[i].Value = value
			}
		}
	}
	for name, tc := range map[string]struct {
		run   TaskRun
		class string
	}{
		"failed with no fail-class result": {edit("test-red", func(tr *TaskRun) { tr.Status.Results = nil }), store.UnitRunInfra},
		"failed with an empty fail-class":  {edit("test-red", func(tr *TaskRun) { setResult(tr, "fail-class", "") }), store.UnitRunInfra},
		"failed with another fail-class":   {edit("test-red", func(tr *TaskRun) { setResult(tr, "fail-class", "oom") }), store.UnitRunInfra},
		"a timeout that says test":         {edit("test-red", func(tr *TaskRun) { tr.Status.Conditions[0].Reason = "TaskRunTimeout" }), store.UnitRunInfra},
		"a pass whose counts do not add":   {edit("pass", func(tr *TaskRun) { setResult(tr, "tests", "169 "+strings.Repeat("a", 64)+" 160 0") }), store.UnitRunInfra},
		"a pass with skipped tests":        {edit("pass", func(tr *TaskRun) { setResult(tr, "tests", "169 "+strings.Repeat("a", 64)+" 160 9") }), store.UnitRunPassed},
		"a pass with no tests result":      {edit("pass", func(tr *TaskRun) { setResult(tr, "tests", "") }), store.UnitRunPassed},
	} {
		row, done, err := Receive(tc.run)
		if err != nil || !done || row.Class != tc.class {
			t.Errorf("%s: %s (done %v, %v), want %s", name, row.Class, done, err, tc.class)
		}
	}
	cutList := edit("test-red", func(tr *TaskRun) { setResult(tr, "failed-tests", "a.TestA,b.TestB,…") })
	if row, _, _ := Receive(cutList); len(row.FailedTests) != 2 || !row.FailedTestsCut {
		t.Errorf("a cut list: %v cut %v", row.FailedTests, row.FailedTestsCut)
	}
	running := edit("pass", func(tr *TaskRun) { tr.Status.Conditions[0].Status = "Unknown" })
	if _, done, err := Receive(running); done || err != nil {
		t.Errorf("a run still going: done %v, %v", done, err)
	}
	for name, tr := range map[string]TaskRun{
		"no plan label":                  edit("pass", func(tr *TaskRun) { delete(tr.Metadata.Labels, LabelPlan) }),
		"no unit annotation":             edit("pass", func(tr *TaskRun) { delete(tr.Metadata.Annotations, AnnotationUnit) }),
		"a key the label does not begin": edit("pass", func(tr *TaskRun) { tr.Metadata.Labels[LabelKey] = strings.Repeat("0", 32) }),
		"a branch for a revision":        edit("pass", func(tr *TaskRun) { tr.Spec.Params[1].Value = "main" }),
	} {
		if tr.Spec.Params[1].Name != "revision" && name == "a branch for a revision" {
			t.Fatal("the fixture's second parameter is not revision")
		}
		if _, done, err := Receive(tr); err == nil || done {
			t.Errorf("%s was received", name)
		}
	}
}

// The rows go into the table as read, an infra row with its empty columns,
// and a run seen again writes nothing.
func TestReceivedRunsAreRecordedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	runs := realRuns(t)
	for _, name := range []string{"pass", "test-red", "build-red", "bad-image"} {
		row, _, err := Receive(runs[name])
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []bool{true, false} {
			wrote, err := store.RecordUnitRun(ctx, pool, row)
			if err != nil || wrote != want {
				t.Fatalf("%s, time %d: wrote %v, %v", name, i, wrote, err)
			}
		}
	}
	key := strings.Repeat("ab", 32)
	got, err := store.UnitRunsByKey(ctx, pool, "bifrost-platform", "api/internal/unitplan", "test", key, 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("%d rows, %v", len(got), err)
	}
	classes := map[string]store.UnitRun{}
	for _, r := range got {
		classes[r.Class] = r
	}
	red, infra := classes[store.UnitRunTest], classes[store.UnitRunInfra]
	if len(red.FailedTests) != 1 || red.TestsListSHA == nil || red.StartedAt == nil || red.CompletedAt.Sub(*red.StartedAt) != 8*time.Second {
		t.Errorf("the red row: %+v", red)
	}
	if infra.Reason != "TaskRunImagePullFailed" || infra.ExitCode != nil || infra.TestsExpected != nil || len(infra.FailedTests) != 0 {
		t.Errorf("the infra row: %+v", infra)
	}
}
