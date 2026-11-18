// Package postrelease judges what runs after a release: once a delivery run
// has succeeded and had time to settle, it reads a few facts about the
// workloads the run delivered and gives one verdict.
//
// Why (2026-11-14, design ci-speed/auto-prod-design.md section 3.1 and the
// ruling of the same day): after each PROD release of the platform a person
// reads the restart counts, /health and the release policy's answer. Before
// the platform may release to PROD with nobody there, it has to make those
// readings itself, and its verdicts have to be compared with the person's
// over twenty releases. This package is that judgement and nothing else: it
// stops, rolls back and tells nobody.
//
// It knows no pipeline, namespace or workload by name: the caller passes
// them, and takes the readings (server/post_release.go is its hands).
package postrelease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// HealthTimeout is how long the health reading may take. Incident #63
// (2026-11-11): the PROD API was throttled at its CPU limit and answered
// slowly while pages reported something else as down.
const HealthTimeout = 5 * time.Second

// ErrNoReading is what a World returns (wrapped) when it could not take a
// reading at all: no client, nothing wired. It makes the items that rest on
// the reading unknown. Any other error of Health or PolicyTrial is the thing
// read not answering, which fails the item.
var ErrNoReading = errors.New("the reading could not be taken")

// Run is the delivery run judged.
type Run struct {
	Name string `json:"name"`
	// Commit is the commit the run released.
	Commit    string    `json:"commit"`
	Started   time.Time `json:"started"`
	Completed time.Time `json:"completed"`
}

// Container is one container of a pod.
type Container struct {
	Name     string
	Ready    bool
	Restarts int
	// Terminated is when the container last exited (its current state if it
	// is terminated, else its last one); zero when it never has.
	Terminated time.Time
	// Exit says how, for the record: "exit 2 Error".
	Exit string
}

// Pod is one pod of one of the Deployments.
type Pod struct {
	Deployment string
	Name       string
	Phase      string
	Containers []Container
	// HealthURL is where this pod itself answers the health reading; empty
	// for a pod that serves none.
	HealthURL string
}

// Trial is the release policy's answer to a read-only trial.
type Trial struct {
	PolicyID string
	Covered  bool
	Reasons  []string
}

// World is what a judgement reads.
type World interface {
	// Pods lists the pods of the Deployments.
	Pods(ctx context.Context, deployments []string) ([]Pod, error)
	// Health is one GET of the health URL: the status and the body.
	Health(ctx context.Context) (status int, body []byte, err error)
	// PodHealth is one GET of one pod's own health URL.
	PodHealth(ctx context.Context, pod Pod) (status int, body []byte, err error)
	// PolicyTrial asks the release policy, read-only, about the run's commit.
	PolicyTrial(ctx context.Context, run Run) (Trial, error)
}

// An item's state. A reading that could not be taken is unknown, never pass.
const (
	Pass    = "pass"
	Fail    = "fail"
	Unknown = "unknown"
)

// The verdicts.
const (
	Passed        = "passed"
	Failed        = "failed"
	VerdictUnsure = "unknown"
)

// The items, in the order judged.
const (
	ItemRestarts = "restarts"
	ItemReady    = "ready"
	ItemHealth   = "health"
	ItemSchema   = "schema"
	ItemPanics   = "panics"
	ItemPolicy   = "policy"
)

// Item is one check: what it is, how it came out and what was read.
type Item struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Reading string `json:"reading"`
}

// Verdict is the judgement of one run. It is failed when any item failed
// (Failed names every one), else unknown when a reading could not be taken
// (Unknown names which; each item's Reading says why), else passed.
type Verdict struct {
	Run     Run      `json:"run"`
	Verdict string   `json:"verdict"`
	Failed  []string `json:"failed,omitempty"`
	Unknown []string `json:"unknown,omitempty"`
	Items   []Item   `json:"items"`
}

// Judge takes the readings once and judges the run.
func Judge(ctx context.Context, w World, run Run, deployments []string) Verdict {
	v := Verdict{Run: run}
	list, err := w.Pods(ctx, deployments)
	v.Items = append(v.Items, pods(list, err, run, deployments)...)
	v.Items = append(v.Items, health(ctx, w)...)
	v.Items = append(v.Items, panics(ctx, w, list, err))
	v.Items = append(v.Items, policy(ctx, w, run))
	for _, it := range v.Items {
		switch it.State {
		case Fail:
			v.Failed = append(v.Failed, it.Name)
		case Unknown:
			v.Unknown = append(v.Unknown, it.Name)
		}
	}
	switch {
	case len(v.Failed) > 0:
		v.Verdict = Failed
	case len(v.Unknown) > 0:
		v.Verdict = VerdictUnsure
	default:
		v.Verdict = Passed
	}
	return v
}

// pods judges restarts and ready from one listing.
func pods(list []Pod, err error, run Run, deployments []string) []Item {
	both := func(state, reading string) []Item {
		return []Item{{ItemRestarts, state, reading}, {ItemReady, state, reading}}
	}
	if err != nil {
		return both(Unknown, "the pods could not be listed: "+err.Error())
	}
	have := map[string]int{}
	for _, p := range list {
		have[p.Deployment]++
	}
	var none []string
	for _, d := range deployments {
		if have[d] == 0 {
			none = append(none, d)
		}
	}
	if len(none) > 0 {
		// A Deployment the run delivered has pods; none listed means the
		// listing is not the reading it should be.
		return both(Unknown, "no pods listed for "+strings.Join(none, ", "))
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	var exited, unready []string
	containers := 0
	for _, p := range list {
		if p.Phase != "Running" {
			unready = append(unready, fmt.Sprintf("%s is %s", p.Name, p.Phase))
		}
		for _, c := range p.Containers {
			containers++
			// An exit before the run started is the pod's history, not this
			// release's: a pod the run did not replace keeps it.
			if !c.Terminated.IsZero() && !c.Terminated.Before(run.Started) {
				exited = append(exited, fmt.Sprintf("%s/%s exited at %s (%s; %d restarts)",
					p.Name, c.Name, c.Terminated.UTC().Format(time.RFC3339), c.Exit, c.Restarts))
			}
			if p.Phase == "Running" && !c.Ready {
				unready = append(unready, fmt.Sprintf("%s/%s is not ready", p.Name, c.Name))
			}
		}
	}
	since := run.Started.UTC().Format(time.RFC3339)
	restarts := Item{ItemRestarts, Pass, fmt.Sprintf("%d containers in %d pods, none exited since the run started (%s)", containers, len(list), since)}
	if len(exited) > 0 {
		restarts.State, restarts.Reading = Fail, strings.Join(exited, "; ")
	}
	ready := Item{ItemReady, Pass, fmt.Sprintf("%d pods running, %d containers ready", len(list), containers)}
	if len(unready) > 0 {
		ready.State, ready.Reading = Fail, strings.Join(unready, "; ")
	}
	return []Item{restarts, ready}
}

// health judges health and schema from one GET of the Service.
func health(ctx context.Context, w World) []Item {
	items := func(h Item, rest, why string) []Item {
		return []Item{h, {ItemSchema, rest, why}}
	}
	ctx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()
	begun := time.Now()
	status, body, err := w.Health(ctx)
	took := time.Since(begun).Round(time.Millisecond)
	if errors.Is(err, ErrNoReading) {
		return items(Item{ItemHealth, Unknown, err.Error()}, Unknown, "health was not read")
	}
	if err != nil {
		return items(Item{ItemHealth, Fail, fmt.Sprintf("no answer within %s: %s", HealthTimeout, err.Error())}, Unknown, "health did not answer")
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil || doc == nil {
		return items(Item{ItemHealth, Fail, fmt.Sprintf("HTTP %d in %s, not a JSON object", status, took)}, Unknown, "health did not answer JSON")
	}
	h := Item{ItemHealth, Pass, fmt.Sprintf("HTTP %d in %s, status %v", status, took, doc["status"])}
	if status != 200 || doc["status"] != "ok" {
		h.State = Fail
	}

	schema := Item{ItemSchema, Unknown, "health has no agentrt_schema"}
	if raw, ok := doc["agentrt_schema"]; ok {
		schema.State, schema.Reading = Fail, fmt.Sprintf("agentrt_schema %v", raw)
		if raw == "current" {
			schema.State = Pass
		}
	}
	return []Item{h, schema}
}

// panics judges the contained panics of every pod that answers a health
// reading, each asked itself. One GET of the Service reaches one replica of
// one Deployment, and a count of zero there says nothing of the others: the
// loops that a contained panic stops run in the workers (ruling 2026-11-14).
// A pod that could not be read leaves the item unknown; a count above zero
// anywhere fails it.
func panics(ctx context.Context, w World, list []Pod, listErr error) Item {
	if listErr != nil {
		return Item{ItemPanics, Unknown, "the pods could not be listed: " + listErr.Error()}
	}
	var asked, bad, unread []string
	for _, p := range list {
		if p.HealthURL == "" {
			continue
		}
		asked = append(asked, p.Name)
		n, why := podPanics(ctx, w, p)
		switch {
		case why != "":
			unread = append(unread, p.Name+": "+why)
		case n != 0:
			bad = append(bad, fmt.Sprintf("%s contained_panics %v", p.Name, n))
		}
	}
	sort.Strings(asked)
	switch {
	case len(bad) > 0:
		return Item{ItemPanics, Fail, strings.Join(append(bad, unread...), "; ")}
	case len(asked) == 0:
		return Item{ItemPanics, Unknown, "no pod answers a health reading"}
	case len(unread) > 0:
		return Item{ItemPanics, Unknown, strings.Join(unread, "; ")}
	}
	return Item{ItemPanics, Pass, fmt.Sprintf("contained_panics 0 in %d pods (%s)", len(asked), strings.Join(asked, ", "))}
}

// podPanics reads one pod's count, or says why it could not.
func podPanics(ctx context.Context, w World, p Pod) (float64, string) {
	ctx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()
	status, body, err := w.PodHealth(ctx, p)
	if err != nil {
		return 0, err.Error()
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil || doc == nil {
		return 0, fmt.Sprintf("HTTP %d, not a JSON object", status)
	}
	raw, ok := doc["contained_panics"]
	if !ok {
		return 0, "health has no contained_panics"
	}
	n, isNumber := raw.(float64)
	if !isNumber {
		return 0, fmt.Sprintf("contained_panics is not a number: %v", raw)
	}
	return n, ""
}

// policy judges that the release policy still answers. Covered or not is
// not the question: after the release it is usually not. An answer with no
// policy and no reason is no answer.
func policy(ctx context.Context, w World, run Run) Item {
	t, err := w.PolicyTrial(ctx, run)
	if errors.Is(err, ErrNoReading) {
		return Item{ItemPolicy, Unknown, err.Error()}
	}
	if err != nil {
		return Item{ItemPolicy, Fail, "the trial answered an error: " + err.Error()}
	}
	if !t.Covered && len(t.Reasons) == 0 {
		return Item{ItemPolicy, Fail, "the trial gave no decision: not covered and no reason"}
	}
	if t.Covered {
		return Item{ItemPolicy, Pass, "decided: covered by policy " + t.PolicyID}
	}
	reading := fmt.Sprintf("decided: not covered, %d reasons", len(t.Reasons))
	if t.PolicyID != "" {
		reading += ", policy " + t.PolicyID
	}
	return Item{ItemPolicy, Pass, reading}
}

// Kicks says whether a ci.run.finished event (subject "<run>@<commit>",
// change the run's result) should start a pass: a succeeded run named as the
// pipeline's ("<pipeline>-<n>"). A wider match is harmless: the pass reads
// each run's pipeline itself; this only starts it (2026-11-17, the bus wakes
// the check beside its 30-second loop).
func Kicks(pipeline, subject, change string) bool {
	run, _, _ := strings.Cut(subject, "@")
	return pipeline != "" && change == "succeeded" && strings.HasPrefix(run, pipeline+"-")
}
