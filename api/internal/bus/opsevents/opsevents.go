// Package opsevents says on the bus what the platform's own work has just
// come to (2026-11-18, the publishers left in step 3 of the generation change):
// a background loop turning healthy or failing (loop.health.changed; S14,
// X-A26), a commit reaching or missing the Git mirror after a sync
// (git.mirror.settled; X-A13), a data clone ending (data.job.finished; L13), a
// probed service or plugin changing state (env.health.changed: L10, R01, R11,
// R12, and how an automatic patrol fix ended, L11; plugin.status.changed: R09,
// and how an automatic IB repair ended, L12).
// The work stays where it is; an event is a hint, its ref is the truth.
package opsevents

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The events.
const (
	TypeLoop   = "loop.health.changed"
	TypeMirror = "git.mirror.settled"
	TypeJob    = "data.job.finished"
	TypeEnv    = "env.health.changed"
	TypePlugin = "plugin.status.changed"
)

const busActor = "controller"

var (
	loopRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9/_-]{0,119}$`)
	mirrorRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}@[0-9a-f]{40}$`)
	jobRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)
	envRE    = regexp.MustCompile(`^[a-z0-9-]{1,40}/[a-z0-9-]{1,60}$`)
	pluginRE = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)
)

const maxCause = 300

// Event is one thing to say. Source and Cause are set on the state events.
type Event struct {
	Type, Subject, Change string
	Source, Cause         string
}

// EnvHealth is a probed service's new state; subject is <env>/<service>.
func EnvHealth(subject, state, source, cause string) Event {
	return Event{TypeEnv, subject, state, source, oneLine(cause)}
}

// PluginStatus is a plugin's new state.
func PluginStatus(plugin, state, source, cause string) Event {
	return Event{TypePlugin, plugin, state, source, oneLine(cause)}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxCause {
		s = string(r[:maxCause])
	}
	return s
}

// Severity is how bad a state is.
func Severity(state string) string {
	switch state {
	case "fail":
		return "critical"
	case "degraded":
		return "warning"
	}
	return "info"
}

func stateEvent(t string) bool { return t == TypeEnv || t == TypePlugin }

func validState(s string) bool { return s == "ok" || s == "degraded" || s == "fail" }

// Loop is a loop's health after a pass that changed it.
func Loop(id string, healthy bool) Event {
	change := "failing"
	if healthy {
		change = "healthy"
	}
	return Event{Type: TypeLoop, Subject: id, Change: change}
}

// Mirror is where a commit stands after a mirror sync.
func Mirror(repo, commit string, present bool) Event {
	change := "missing"
	if present {
		change = "present"
	}
	return Event{Type: TypeMirror, Subject: repo + "@" + commit, Change: change}
}

// Job is how a data job ended.
func Job(id string, ok bool) Event {
	change := "failed"
	if ok {
		change = "succeeded"
	}
	return Event{Type: TypeJob, Subject: id, Change: change}
}

func refOf(e Event) string {
	switch e.Type {
	case TypeLoop:
		return "/api/v1/agentrt/progress"
	case TypeMirror:
		repo, _, _ := strings.Cut(e.Subject, "@")
		return "/api/v1/delivery/context?repo=" + url.QueryEscape(repo)
	case TypeEnv:
		return "/api/v1/checklist/signals"
	case TypePlugin:
		return "/api/v1/plugins/" + e.Subject + "/status"
	}
	return "/api/v1/data/clone"
}

// Valid says the bus can name e.
func Valid(e Event) bool {
	switch e.Type {
	case TypeLoop:
		return loopRE.MatchString(e.Subject) && (e.Change == "healthy" || e.Change == "failing")
	case TypeMirror:
		return mirrorRE.MatchString(e.Subject) && (e.Change == "present" || e.Change == "missing")
	case TypeJob:
		return jobRE.MatchString(e.Subject) && (e.Change == "succeeded" || e.Change == "failed")
	case TypeEnv:
		return envRE.MatchString(e.Subject) && validState(e.Change) && e.Source != ""
	case TypePlugin:
		return pluginRE.MatchString(e.Subject) && validState(e.Change) && e.Source != ""
	}
	return false
}

// Body is the event's body.
func Body(e Event) map[string]any {
	b := map[string]any{"subject": e.Subject, "ref": refOf(e), "change": e.Change}
	if stateEvent(e.Type) {
		b["source"], b["severity"] = e.Source, Severity(e.Change)
		if e.Cause != "" {
			b["cause"] = e.Cause
		}
	}
	return b
}

// Announce writes e. Before the bus's actor exists it writes nothing.
func Announce(ctx context.Context, db eventlog.DB, e Event) error {
	if !Valid(e) {
		return fmt.Errorf("opsevents: %s %q cannot be named on the bus", e.Type, e.Subject)
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if ok, err := actor.Exists(ctx, tx, busActor); err != nil || !ok {
			return err
		}
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "ops:" + e.Type + ":" + e.Subject, Type: e.Type, Actor: busActor, Body: Body(e)})
		return err
	})
}

func init() {
	fields := []eventlog.Field{{Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str}}
	check := func(typ string) func(map[string]any) error {
		return func(b map[string]any) error {
			subject, _ := b["subject"].(string)
			change, _ := b["change"].(string)
			e := Event{Type: typ, Subject: subject, Change: change}
			if !Valid(e) || b["ref"] != refOf(e) {
				return eventlog.Invalid("%s: subject, ref and change are fixed by the event", typ)
			}
			return nil
		}
	}
	v := eventlog.Vocabulary{Bodies: map[string][]eventlog.Field{}, After: map[string]func(map[string]any) error{}}
	for _, t := range []string{TypeLoop, TypeMirror, TypeJob} {
		v.Bodies[t] = fields
		v.After[t] = check(t)
	}
	stateFields := append(append([]eventlog.Field{}, fields...),
		eventlog.Field{Name: "source", Kind: eventlog.Str}, eventlog.Field{Name: "severity", Kind: eventlog.Str},
		eventlog.Field{Name: "cause", Kind: eventlog.Str, Optional: true})
	checkState := func(typ string) func(map[string]any) error {
		return func(b map[string]any) error {
			subject, _ := b["subject"].(string)
			change, _ := b["change"].(string)
			source, _ := b["source"].(string)
			cause, _ := b["cause"].(string)
			e := Event{typ, subject, change, source, cause}
			if !Valid(e) || b["ref"] != refOf(e) || b["severity"] != Severity(change) || cause != oneLine(cause) {
				return eventlog.Invalid("%s: subject, ref, change, source, severity and cause are fixed by the event", typ)
			}
			return nil
		}
	}
	for _, t := range []string{TypeEnv, TypePlugin} {
		v.Bodies[t] = stateFields
		v.After[t] = checkState(t)
	}
	eventlog.Declare(v)
}
