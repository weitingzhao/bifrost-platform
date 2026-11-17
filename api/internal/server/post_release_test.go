//bifrost:owner delivery

package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/postrelease"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var postNow = time.Date(2026, 11, 14, 21, 0, 0, 0, time.UTC)

func postFinished(name, status string, completedAgo time.Duration) postRun {
	r := postRun{Status: status}
	r.Name = name
	r.Completed = postNow.Add(-completedAgo)
	r.Started = r.Completed.Add(-6 * time.Minute)
	return r
}

// Which run is judged, and when (design 2026-11-14, section 3.1: one verdict
// per successful PROD release, a settle time after it; ruling 2026-11-14:
// false reds are counted, so a run is not judged by pods another run moved).
func TestPostReleaseDue(t *testing.T) {
	const settle, maxAge = 3 * time.Minute, 30 * time.Minute
	none := func(string) bool { return false }
	running := postRun{Status: "running"}
	running.Name = "prod-3"
	cases := []struct {
		name   string
		runs   []postRun
		judged func(string) bool
		want   string // the run judged, "" for none
		why    string // part of the reason when none
	}{
		{name: "settled and not judged: judged now",
			runs: []postRun{postFinished("prod-2", "succeeded", 4*time.Minute), postFinished("prod-1", "succeeded", time.Hour)}, judged: none, want: "prod-2"},
		{name: "exactly the settle time ago: judged now",
			runs: []postRun{postFinished("prod-2", "succeeded", settle)}, judged: none, want: "prod-2"},
		{name: "not settled yet: waits",
			runs: []postRun{postFinished("prod-2", "succeeded", 2*time.Minute)}, judged: none, why: "settles until"},
		{name: "already judged: not again (the audit log says so after a restart)",
			runs: []postRun{postFinished("prod-2", "succeeded", 4*time.Minute)}, judged: func(run string) bool { return run == "prod-2" }, why: "has its verdict"},
		{name: "completed before the process could have judged it: history is not judged",
			runs: []postRun{postFinished("prod-2", "succeeded", settle+maxAge+time.Second)}, judged: none, why: "too long ago"},
		{name: "at the edge of the age limit: still judged",
			runs: []postRun{postFinished("prod-2", "succeeded", settle+maxAge)}, judged: none, want: "prod-2"},
		{name: "a newer run is under way: the pods are moving, nothing is judged",
			runs: []postRun{running, postFinished("prod-2", "succeeded", 4*time.Minute)}, judged: none, why: "prod-3, is running"},
		{name: "the newest run failed: what runs is not the older release's alone",
			runs: []postRun{postFinished("prod-3", "failed", 4*time.Minute), postFinished("prod-2", "succeeded", 10*time.Minute)}, judged: none, why: "prod-3, is failed"},
		{name: "a succeeded run with no completion time: not judged",
			runs: []postRun{{Run: postrelease.Run{Name: "prod-2"}, Status: "succeeded"}}, judged: none, why: "no completion time"},
		{name: "no runs", judged: none, why: "no runs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := postReleaseDue(c.runs, postNow, settle, maxAge, c.judged)
			switch {
			case c.want == "" && got != nil:
				t.Fatalf("judged %s, want none", got.Name)
			case c.want == "" && (why == "" || !strings.Contains(why, c.why)):
				t.Fatalf("why = %q, want it to say %q", why, c.why)
			case c.want != "" && (got == nil || got.Name != c.want):
				t.Fatalf("got %v (%s), want %s", got, why, c.want)
			}
		})
	}
}

func postListed(name, pipeline string, created time.Time, condition, started, completed string, params map[string]string) unstructured.Unstructured {
	u := headRun(name, pipeline, created, condition, params)
	if condition != "" {
		status := u.Object["status"].(map[string]any)
		status["startTime"], status["completionTime"] = started, completed
	}
	return u
}

// The runs as the cluster lists them (the shape head_release_test.go took
// from the listing of 2026-11-13): only the pipeline followed, newest first,
// with the run's own start and completion times and its revision as the
// commit released.
func TestPostRunsReadsThePipelinesRunsNewestFirst(t *testing.T) {
	const prod, stg = "bifrost-deliver-platform-prod", "bifrost-deliver-platform"
	sha := strings.Repeat("a", 40)
	at := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	items := []unstructured.Unstructured{
		postListed("prod-1", prod, at, "True", "2026-11-14T20:00:05Z", "2026-11-14T20:06:00Z", map[string]string{"revision": sha, "uiRevision": strings.Repeat("b", 40)}),
		postListed("stg-9", stg, at.Add(time.Hour), "True", "2026-11-14T21:00:05Z", "2026-11-14T21:06:00Z", nil),
		postListed("prod-3", prod, at.Add(20*time.Minute), "", "", "", nil),
		postListed("prod-2", prod, at.Add(10*time.Minute), "False", "2026-11-14T20:10:05Z", "2026-11-14T20:11:00Z", nil),
	}
	runs := postRuns(items, prod)
	if len(runs) != 3 || runs[0].Name != "prod-3" || runs[1].Name != "prod-2" || runs[2].Name != "prod-1" {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].Status != "running" || runs[1].Status != "failed" || runs[2].Status != "succeeded" {
		t.Fatalf("statuses = %s %s %s", runs[0].Status, runs[1].Status, runs[2].Status)
	}
	first := runs[2]
	if first.Commit != sha || !first.Started.Equal(at.Add(5*time.Second)) || !first.Completed.Equal(at.Add(6*time.Minute)) {
		t.Fatalf("prod-1 = %+v", first)
	}
	if got := postTrialParams(first.Params); len(got) != 1 || got["uiRevision"] != strings.Repeat("b", 40) {
		t.Fatalf("trial params = %v", got)
	}
}

// restarts (ruling 2026-11-14: stands in for a person reading the startup
// log after a release): the exit read is the latest of the container's
// current and last terminations, init containers are not read, and a pod
// being deleted is not the Deployment's any more.
func TestPostPodOfReadsTheLatestExit(t *testing.T) {
	earlier := metav1.NewTime(time.Date(2026, 11, 14, 19, 0, 0, 0, time.UTC))
	later := metav1.NewTime(time.Date(2026, 11, 14, 20, 30, 0, 0, time.UTC))
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-api-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "init",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: later}}}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "platform-api", Ready: true, RestartCount: 2,
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: later, ExitCode: 2, Reason: "Error"}}},
				{Name: "sidecar", Ready: false,
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: earlier, ExitCode: 1}},
					State:                corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: later, ExitCode: 137, Reason: "OOMKilled"}}},
				{Name: "quiet", Ready: true},
			},
		},
	}
	pod, ok := postPodOf("platform-api", p)
	if !ok || pod.Deployment != "platform-api" || pod.Phase != "Running" || len(pod.Containers) != 3 {
		t.Fatalf("pod = %+v ok=%v", pod, ok)
	}
	if c := pod.Containers[0]; !c.Terminated.Equal(later.Time) || c.Exit != "exit 2 Error" || c.Restarts != 2 || !c.Ready {
		t.Fatalf("platform-api = %+v", c)
	}
	if c := pod.Containers[1]; !c.Terminated.Equal(later.Time) || c.Exit != "exit 137 OOMKilled" || c.Ready {
		t.Fatalf("sidecar = %+v", c)
	}
	if c := pod.Containers[2]; !c.Terminated.IsZero() {
		t.Fatalf("quiet = %+v", c)
	}
	// Each pod of the api and the workers is asked for its own contained
	// panics at its own address; the console is not (ruling 2026-11-14: the
	// first version read one replica behind the Service, never the workers).
	if pod.HealthURL != "" {
		t.Fatalf("a pod with no address has a health URL: %q", pod.HealthURL)
	}
	p.Status.PodIP = "10.42.8.4"
	for deployment, want := range map[string]string{
		"platform-api": "http://10.42.8.4:8780/health", "platform-workers": "http://10.42.8.4:8780/health", "platform-console": "",
	} {
		if got, _ := postPodOf(deployment, p); got.HealthURL != want {
			t.Fatalf("%s health URL = %q, want %q", deployment, got.HealthURL, want)
		}
	}
	p.DeletionTimestamp = &later
	if _, ok := postPodOf("platform-api", p); ok {
		t.Fatal("a pod being deleted was read")
	}
}

// The switch and the settle time (design 2026-11-14, section 4: every step
// is a switch, and off is the default).
func TestPostReleaseSwitchAndSettle(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "observe": true, " Observe ": true, "stg": false, "on": false} {
		t.Setenv(postReleaseEnv, value)
		if postReleaseOn() != want {
			t.Errorf("%s=%q: on = %v, want %v", postReleaseEnv, value, !want, want)
		}
	}
	for value, want := range map[string]time.Duration{"": 180 * time.Second, "60": time.Minute, "0": 0, "abc": 180 * time.Second, "-5": 180 * time.Second, "99999": 180 * time.Second} {
		if got := postReleaseSettleOf(value); got != want {
			t.Errorf("settle %q = %s, want %s", value, got, want)
		}
	}
	t.Setenv(postReleaseEnv, "off")
	if (&Server{}).postReleaseSource() != nil {
		t.Fatal("switched off, the loop has a pass")
	}
}

// policy (2026-11-14, ci-feedback 37: the engine could not judge and nobody
// knew). The trial goes through the policy-check handler itself: against a
// signed policy it comes back with a decision, and a process with no engine
// says the reading could not be taken instead of an answer.
func TestPostWorldPolicyTrialAsksThePolicyCheckHandler(t *testing.T) {
	h := newPolicyHarness(t)
	w := &postWorld{s: h.srv, pipeline: policyPipeline, params: map[string]string{"uiRevision": strings.Repeat("b", 40), "image": "x"}}
	run := postrelease.Run{Name: "prod-1", Commit: strings.Repeat("a", 40)}
	trial, err := w.PolicyTrial(context.Background(), run)
	if err != nil {
		t.Fatalf("trial: %v", err)
	}
	if trial.PolicyID != "rp-20261008-1100" || (!trial.Covered && len(trial.Reasons) == 0) {
		t.Fatalf("trial = %+v", trial)
	}
	if text := h.auditText(t); strings.Contains(text, "release_policy") {
		t.Fatalf("the trial wrote to the audit log: %s", text)
	}

	h.srv.releasePolicy = nil
	if _, err := w.PolicyTrial(context.Background(), run); !errors.Is(err, postrelease.ErrNoReading) {
		t.Fatalf("no engine: err = %v, want ErrNoReading", err)
	}
}

// One verdict per run across a restart: the record is found in the audit log
// a new process loads (the Owner's "not judged yet must survive a restart",
// task of 2026-11-14).
func TestPostReleaseVerdictIsFoundAfterARestart(t *testing.T) {
	h := newPolicyHarness(t)
	if h.srv.audit.Has(postReleaseAction, "run/prod-1") {
		t.Fatal("found before it was recorded")
	}
	h.srv.recordPostRelease(postrelease.Verdict{Run: postrelease.Run{Name: "prod-1"}, Verdict: postrelease.Passed})
	if !h.srv.audit.Has(postReleaseAction, "run/prod-1") || h.srv.audit.Has(postReleaseAction, "run/prod-2") {
		t.Fatal("the record is not found by its run")
	}
	text := h.auditText(t)
	if !strings.Contains(text, `"action":"post_release_check"`) || !strings.Contains(text, `"status":"passed"`) {
		t.Fatalf("audit = %s", text)
	}
}
