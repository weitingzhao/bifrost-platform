package postrelease

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var (
	runStarted = time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	theRun     = Run{Name: "deliver-prod-1", Commit: "0123456789abcdef0123456789abcdef01234567",
		Started: runStarted, Completed: runStarted.Add(6 * time.Minute)}
	theDeployments = []string{"api", "workers", "console"}
)

const goodHealth = `{"status":"ok","agentrt_schema":"current","contained_panics":0}`

// fakeWorld is a World whose every reading is good until a test makes one bad.
type fakeWorld struct {
	pods      []Pod
	podsErr   error
	status    int
	body      string
	healthErr error
	// healthHangs makes Health wait for its context to end, as a server
	// that never answers does.
	healthHangs bool
	trial       Trial
	trialErr    error
}

func goodWorld() *fakeWorld {
	pod := func(deployment string) Pod {
		return Pod{Deployment: deployment, Name: deployment + "-7c9f-x2x4z", Phase: "Running",
			Containers: []Container{{Name: "main", Ready: true}}}
	}
	return &fakeWorld{
		pods:   []Pod{pod("api"), pod("workers"), pod("console")},
		status: 200, body: goodHealth,
		trial: Trial{PolicyID: "policy-1", Reasons: []string{"PROD already runs this commit"}},
	}
}

func (w *fakeWorld) Pods(context.Context, []string) ([]Pod, error) { return w.pods, w.podsErr }

func (w *fakeWorld) Health(ctx context.Context) (int, []byte, error) {
	if w.healthHangs {
		<-ctx.Done()
		return 0, nil, ctx.Err()
	}
	return w.status, []byte(w.body), w.healthErr
}

func (w *fakeWorld) PolicyTrial(context.Context, Run) (Trial, error) { return w.trial, w.trialErr }

func judge(w World) Verdict { return Judge(context.Background(), w, theRun, theDeployments) }

func states(v Verdict) map[string]string {
	out := map[string]string{}
	for _, it := range v.Items {
		out[it.Name] = it.State
	}
	return out
}

// Every reading good gives passed, with all six items read. Without this
// case the positive controls below prove nothing: a judgement that fails
// everything would pass them all.
func TestEveryReadingGoodPasses(t *testing.T) {
	v := judge(goodWorld())
	if v.Verdict != Passed || len(v.Failed) != 0 || len(v.Unknown) != 0 {
		t.Fatalf("verdict = %+v", v)
	}
	want := []string{ItemRestarts, ItemReady, ItemHealth, ItemSchema, ItemPanics, ItemPolicy}
	var got []string
	for _, it := range v.Items {
		if it.State != Pass || it.Reading == "" {
			t.Errorf("%s: state %s, reading %q", it.Name, it.State, it.Reading)
		}
		got = append(got, it.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

// The positive controls: one reading made bad, and the verdict must fail and
// name that item alone.
func TestOneBadReadingFailsAndNamesItsItem(t *testing.T) {
	cases := []struct {
		name string
		item string
		bad  func(w *fakeWorld)
		// unknown are the items the bad reading leaves unread.
		unknown []string
	}{
		{
			// restarts stands in for a person reading the startup log after
			// a release; no instance caught by it yet (ruling 2026-11-14).
			name: "a container exited after the run started", item: ItemRestarts,
			bad: func(w *fakeWorld) {
				w.pods[1].Containers[0].Terminated = runStarted.Add(7 * time.Minute)
				w.pods[1].Containers[0].Exit = "exit 2 Error"
				w.pods[1].Containers[0].Restarts = 1
			},
		},
		{
			// restarts, the boundary: an exit at the instant the run started
			// counts (ruling 2026-11-14, same origin as above).
			name: "a container exited at the instant the run started", item: ItemRestarts,
			bad: func(w *fakeWorld) { w.pods[0].Containers[0].Terminated = runStarted },
		},
		{
			// ready: the pipeline's own rollout check passes at one instant;
			// this reads the pods again after the settle time (design
			// 2026-11-14, section 2: "ready once, then down").
			name: "a container is not ready", item: ItemReady,
			bad: func(w *fakeWorld) { w.pods[2].Containers[0].Ready = false },
		},
		{
			// ready, same origin (design 2026-11-14, section 2).
			name: "a pod is not running", item: ItemReady,
			bad: func(w *fakeWorld) { w.pods[0].Phase = "Pending" },
		},
		{
			// health: incident #63, 2026-11-11. The PROD API was throttled at
			// its CPU limit while pages reported something else as down.
			name: "health answers 503", item: ItemHealth,
			bad: func(w *fakeWorld) { w.status = 503 },
		},
		{
			// health: incident #63, 2026-11-11 (as above).
			name: "health says a status other than ok", item: ItemHealth,
			bad: func(w *fakeWorld) {
				w.body = `{"status":"degraded","agentrt_schema":"current","contained_panics":0}`
			},
		},
		{
			// health: incident #63, 2026-11-11: an API that does not answer
			// in time. schema and panics come from the same answer and are
			// left unread, not failed.
			name: "health does not answer in time", item: ItemHealth,
			bad:     func(w *fakeWorld) { w.healthErr = context.DeadlineExceeded },
			unknown: []string{ItemSchema, ItemPanics},
		},
		{
			// schema: TD-411. Migrations not applied: the API starts but
			// cannot be used.
			name: "the schema is behind", item: ItemSchema,
			bad: func(w *fakeWorld) {
				w.body = `{"status":"ok","agentrt_schema":"behind","contained_panics":0}`
			},
		},
		{
			// panics: 2026-11-03, the safego guard. A contained panic is
			// counted in /health; a worker died mid-flight while status is ok.
			name: "a panic was contained", item: ItemPanics,
			bad: func(w *fakeWorld) {
				w.body = `{"status":"ok","agentrt_schema":"current","contained_panics":1}`
			},
		},
		{
			// policy: 2026-11-14, ci-feedback 37. The engine could not judge
			// large releases and nobody knew until a lane hit it.
			name: "the policy trial answers an error", item: ItemPolicy,
			bad: func(w *fakeWorld) { w.trialErr = errors.New("HTTP 500: tree walk failed") },
		},
		{
			// policy: 2026-11-14, ci-feedback 37 (as above): an answer that
			// decides nothing.
			name: "the policy trial gives no decision", item: ItemPolicy,
			bad: func(w *fakeWorld) { w.trial = Trial{} },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := goodWorld()
			c.bad(w)
			v := judge(w)
			if v.Verdict != Failed {
				t.Fatalf("verdict = %s, want failed: %+v", v.Verdict, v.Items)
			}
			if !reflect.DeepEqual(v.Failed, []string{c.item}) {
				t.Fatalf("failed = %v, want only %s", v.Failed, c.item)
			}
			if !reflect.DeepEqual(v.Unknown, c.unknown) {
				t.Fatalf("unknown = %v, want %v", v.Unknown, c.unknown)
			}
			for _, it := range v.Items {
				if it.Name == c.item && it.Reading == "" {
					t.Fatalf("%s failed with no reading", c.item)
				}
			}
		})
	}
}

// health: incident #63, 2026-11-11. A server that never answers is cut off
// at HealthTimeout by the judgement itself, whatever deadline the caller
// gave, and fails health.
func TestHealthThatHangsIsCutOffAndFails(t *testing.T) {
	if testing.Short() {
		t.Skip("waits HealthTimeout")
	}
	w := goodWorld()
	w.healthHangs = true
	begun := time.Now()
	v := judge(w)
	if took := time.Since(begun); took < HealthTimeout || took > HealthTimeout+2*time.Second {
		t.Fatalf("took %s, want about %s", took, HealthTimeout)
	}
	if v.Verdict != Failed || !reflect.DeepEqual(v.Failed, []string{ItemHealth}) {
		t.Fatalf("verdict = %+v", v)
	}
}

// restarts (ruling 2026-11-14: stands in for a person reading the startup
// log; no instance caught by it yet). The other side of its control: a pod
// the run did not replace, whose container exited before the run started,
// is not this release's failure. A false red here is counted against the
// item in the observe step.
func TestExitBeforeTheRunIsNotAFailure(t *testing.T) {
	w := goodWorld()
	w.pods[2].Containers[0].Terminated = runStarted.Add(-time.Second)
	w.pods[2].Containers[0].Restarts = 4
	if v := judge(w); v.Verdict != Passed {
		t.Fatalf("verdict = %+v", v)
	}
}

// A reading that could not be taken gives unknown, never passed, and says
// which items and why. One case per kind of reading.
func TestReadingNotTakenIsUnknown(t *testing.T) {
	notWired := fmt.Errorf("%w: nothing is wired here", ErrNoReading)
	cases := []struct {
		name    string
		bad     func(w *fakeWorld)
		unknown []string
	}{
		{
			// restarts and ready (ruling 2026-11-14; design section 7: whether
			// the platform's identity may read the pods was not verified).
			name:    "the pods cannot be listed",
			bad:     func(w *fakeWorld) { w.podsErr = errors.New("pods is forbidden") },
			unknown: []string{ItemRestarts, ItemReady},
		},
		{
			// restarts and ready (ruling 2026-11-14): a Deployment with no
			// pods listed was not read; three of three is not what was seen.
			name:    "a Deployment has no pods listed",
			bad:     func(w *fakeWorld) { w.pods = w.pods[:2] },
			unknown: []string{ItemRestarts, ItemReady},
		},
		{
			// health, schema, panics (incident #63, TD-411, safego 2026-11-03):
			// a process with no health URL to read has read nothing.
			name:    "health cannot be read at all",
			bad:     func(w *fakeWorld) { w.healthErr = notWired },
			unknown: []string{ItemHealth, ItemSchema, ItemPanics},
		},
		{
			// schema (TD-411): a health answer without the field says nothing
			// about the schema.
			name:    "health has no agentrt_schema",
			bad:     func(w *fakeWorld) { w.body = `{"status":"ok","contained_panics":0}` },
			unknown: []string{ItemSchema},
		},
		{
			// panics (safego, 2026-11-03): no count is not a count of zero.
			name:    "health has no contained_panics",
			bad:     func(w *fakeWorld) { w.body = `{"status":"ok","agentrt_schema":"current"}` },
			unknown: []string{ItemPanics},
		},
		{
			// policy (2026-11-14, ci-feedback 37): a process with no policy
			// engine cannot say the engine answers.
			name:    "the policy cannot be asked at all",
			bad:     func(w *fakeWorld) { w.trialErr = notWired },
			unknown: []string{ItemPolicy},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := goodWorld()
			c.bad(w)
			v := judge(w)
			if v.Verdict != VerdictUnsure || len(v.Failed) != 0 {
				t.Fatalf("verdict = %s, failed = %v: %+v", v.Verdict, v.Failed, v.Items)
			}
			if !reflect.DeepEqual(v.Unknown, c.unknown) {
				t.Fatalf("unknown = %v, want %v", v.Unknown, c.unknown)
			}
			for name, state := range states(v) {
				if state == Unknown {
					continue
				}
				if state != Pass {
					t.Errorf("%s = %s", name, state)
				}
			}
			for _, it := range v.Items {
				if it.State == Unknown && it.Reading == "" {
					t.Errorf("%s is unknown with no reason", it.Name)
				}
			}
		})
	}
}

// A failure is not hidden by a reading that could not be taken elsewhere:
// the verdict is failed and both are named (schema: TD-411; pods: ruling
// 2026-11-14).
func TestFailedWinsOverUnknown(t *testing.T) {
	w := goodWorld()
	w.podsErr = errors.New("pods is forbidden")
	w.body = `{"status":"ok","agentrt_schema":"behind","contained_panics":0}`
	v := judge(w)
	if v.Verdict != Failed || !reflect.DeepEqual(v.Failed, []string{ItemSchema}) ||
		!reflect.DeepEqual(v.Unknown, []string{ItemRestarts, ItemReady}) {
		t.Fatalf("verdict = %+v", v)
	}
}
