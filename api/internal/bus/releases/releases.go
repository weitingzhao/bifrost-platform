// Package releases announces a finished release on the bus
// (release.deploy.finished) for the code that records it. The CI line's
// release record table writes the record and calls Append in the same
// transaction (2026-11-16, its draft "发布记录搬进库"; the bus RULES §7:
// state and event in one transaction). The record stays the truth; the
// event points at the release context route.
package releases

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the event: a release run finished, deployed or not.
const Type = "release.deploy.finished"

// Changes: the run succeeded or failed (the record keeps failures too).
const (
	ChangeSucceeded = "succeeded"
	ChangeFailed    = "failed"
)

// writer is the actor of the events; the platform seeds it.
const writer = "controller"

var (
	envRE  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	laneRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	repoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	sha40  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	runRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
)

// Release is what the bus says of one finished release run.
type Release struct {
	Env, Lane, Repo, SHA, Run string
	Succeeded                 bool
	// Cause is the ledger event that woke the run's start (the run's
	// bifrost.io/cause label); 0 for none (2026-11-17).
	Cause int64
}

// Subject is "<env>/<lane>@<sha>", the register's "<env>/<app>@<sha40>".
func Subject(r Release) string { return r.Env + "/" + r.Lane + "@" + r.SHA }

// Ref is where a reader reads what runs now.
func Ref(repo string) string { return "/api/v1/delivery/context?repo=" + repo }

// Stream is the ledger stream of one lane in one environment.
func Stream(r Release) string { return "release:" + r.Lane + ":" + r.Env }

func change(r Release) string {
	if r.Succeeded {
		return ChangeSucceeded
	}
	return ChangeFailed
}

// Body is the event's body: the bus's fixed fields and their facts.
func Body(r Release) map[string]any {
	return map[string]any{
		"env": r.Env, "lane": r.Lane, "repo": r.Repo, "sha": r.SHA, "run": r.Run,
		"subject": Subject(r), "ref": Ref(r.Repo), "change": change(r),
	}
}

// Append writes the event in tx, the transaction that writes the record, and
// returns its ledger event id (the record's event_id). A ledger without the
// controller writes nothing and returns 0.
func Append(ctx context.Context, tx pgx.Tx, r Release) (int64, error) {
	if ok, err := actor.Exists(ctx, tx, writer); err != nil || !ok {
		return 0, err
	}
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(r), Type: Type, Actor: writer, Cause: r.Cause, Body: Body(r)})
	if err != nil {
		return 0, err
	}
	return ev.EventID, nil
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			Type: {
				{Name: "env", Kind: eventlog.Str},
				{Name: "lane", Kind: eventlog.Str},
				{Name: "repo", Kind: eventlog.Str},
				{Name: "sha", Kind: eventlog.Str},
				{Name: "run", Kind: eventlog.Str},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			Type: {
				"env":    eventlog.Shaped("an environment name", envRE),
				"lane":   eventlog.Shaped("a lane name", laneRE),
				"repo":   eventlog.Shaped("a repository name", repoRE),
				"sha":    eventlog.Shaped("a 40-character lowercase commit", sha40),
				"run":    eventlog.Shaped("a pipeline run name", runRE),
				"change": eventlog.OneOf(ChangeSucceeded, ChangeFailed),
			},
		},
		After: map[string]func(body map[string]any) error{
			Type: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				r := Release{Env: s("env"), Lane: s("lane"), Repo: s("repo"), SHA: s("sha")}
				if body["subject"] != Subject(r) || body["ref"] != Ref(r.Repo) {
					return eventlog.Invalid("%s: subject and ref are made of env, lane, sha and repo", Type)
				}
				return nil
			},
		},
	})
}
