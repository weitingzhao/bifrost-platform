package gatecluster

import (
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The types gate-cluster writes (bus/registry/rocket.yaml registers them;
// migration 0026 lets the database take them).
const (
	TypeRunStarted    = "ci.run.started"
	TypeRunFinished   = "ci.run.finished"
	TypeWindowChanged = "release.window.changed"
)

// The change of ci.run.started: the run was created.
const ChangeCreated = "created"

// The changes of ci.run.finished, from the run's Succeeded condition.
const (
	ChangeSucceeded = "succeeded"
	ChangeFailed    = "failed"
	ChangeCancelled = "cancelled"
	ChangeTimedOut  = "timedout"
)

// The changes of release.window.changed (RULES.md §2 names them).
const (
	ChangeHeld     = "held"
	ChangeReleased = "released"
	ChangeExpired  = "expired"
	ChangeQueued   = "queued"
)

// WindowKey is the key and the window field of release.window.changed: there
// is one release window.
const WindowKey = "release-window"

var (
	sha40   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	nameRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	digestR = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// RunRef is where a reader of a run event reads the run.
func RunRef(run string) string { return "/api/v1/delivery/runs/" + run }

// WindowRef is where a reader of a window event reads the window.
const WindowRef = "/api/v1/delivery/release-window"

// RunSubject is "<run>@<commit>", or the run alone when it names no commit.
func RunSubject(run, commit string) string {
	if commit == "" {
		return run
	}
	return run + "@" + commit
}

func runFields(extra ...eventlog.Field) []eventlog.Field {
	return append([]eventlog.Field{
		{Name: "subject", Kind: eventlog.Str},
		{Name: "ref", Kind: eventlog.Str},
		{Name: "change", Kind: eventlog.Str},
		{Name: "run", Kind: eventlog.ID},
		{Name: "pipeline", Kind: eventlog.Str, Optional: true},
		{Name: "commit", Kind: eventlog.ID, Optional: true},
	}, extra...)
}

func runAfter(typ string) func(body map[string]any) error {
	return func(body map[string]any) error {
		run, _ := body["run"].(string)
		commit, _ := body["commit"].(string)
		if body["subject"] != RunSubject(run, commit) || body["ref"] != RunRef(run) {
			return eventlog.Invalid("%s: subject and ref are made of run and commit", typ)
		}
		return nil
	}
}

func init() {
	runValues := func(changes ...string) map[string]eventlog.Value {
		return map[string]eventlog.Value{
			"run":    eventlog.Shaped("a PipelineRun name", nameRE),
			"commit": eventlog.Shaped("a 40-character lowercase commit", sha40),
			"change": eventlog.OneOf(changes...),
		}
	}
	finishedValues := runValues(ChangeSucceeded, ChangeFailed, ChangeCancelled, ChangeTimedOut)
	finishedValues["status"] = eventlog.OneOf("True", "False")
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			TypeRunStarted: runFields(),
			TypeRunFinished: runFields(
				eventlog.Field{Name: "status", Kind: eventlog.Str},
				eventlog.Field{Name: "reason", Kind: eventlog.Str, Optional: true},
			),
			TypeWindowChanged: {
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "window", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
				{Name: "who", Kind: eventlog.Str, Optional: true},
				{Name: "what", Kind: eventlog.Str, Optional: true},
				{Name: "since", Kind: eventlog.Str, Optional: true},
				{Name: "queue", Kind: eventlog.Int},
				{Name: "digest", Kind: eventlog.ID},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			TypeRunStarted:  runValues(ChangeCreated),
			TypeRunFinished: finishedValues,
			TypeWindowChanged: {
				"change": eventlog.OneOf(ChangeHeld, ChangeReleased, ChangeExpired, ChangeQueued),
				"digest": eventlog.Shaped("16 lowercase hex digits", digestR),
			},
		},
		After: map[string]func(body map[string]any) error{
			TypeRunStarted:  runAfter(TypeRunStarted),
			TypeRunFinished: runAfter(TypeRunFinished),
			TypeWindowChanged: func(body map[string]any) error {
				if body["subject"] != "window" || body["ref"] != WindowRef || body["key"] != WindowKey || body["window"] != WindowKey {
					return eventlog.Invalid("%s: subject, ref, key and window are the release window's", TypeWindowChanged)
				}
				return nil
			},
		},
	})
}
