// The announcement of a finished release on the bus
// (release.deploy.finished) for the code that records it: the release record
// table writes the record and calls appendAnnouncement in the same
// transaction (2026-11-16, the CI line's draft "发布记录搬进库"; the bus RULES
// §7: state and event in one transaction). The record stays the truth; the
// event points at the release context route. Was package bus/releases, moved
// here 2026-11-20: releasetable was its only user.

package releasetable

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// deployFinishedType is the event: a release run finished, deployed or not.
const deployFinishedType = "release.deploy.finished"

// Changes: the run succeeded or failed (the record keeps failures too).
const (
	changeSucceeded = "succeeded"
	changeFailed    = "failed"
)

// announceWriter is the actor of the events; the platform seeds it.
const announceWriter = "controller"

var (
	envRE  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	laneRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	repoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	sha40  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	runRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
)

// announcement is what the bus says of one finished release run.
type announcement struct {
	Env, Lane, Repo, SHA, Run string
	Succeeded                 bool
	// Cause is the ledger event that woke the run's start (the run's
	// bifrost.io/cause label); 0 for none (2026-11-17).
	Cause int64
}

// announcementSubject is "<env>/<lane>@<sha>", the register's "<env>/<app>@<sha40>".
func announcementSubject(r announcement) string { return r.Env + "/" + r.Lane + "@" + r.SHA }

// announcementRef is where a reader reads what runs now.
func announcementRef(repo string) string { return "/api/v1/delivery/context?repo=" + repo }

// announcementStream is the ledger stream of one lane in one environment.
func announcementStream(r announcement) string { return "release:" + r.Lane + ":" + r.Env }

func announcementChange(r announcement) string {
	if r.Succeeded {
		return changeSucceeded
	}
	return changeFailed
}

// announcementBody is the event's body: the bus's fixed fields and their facts.
func announcementBody(r announcement) map[string]any {
	return map[string]any{
		"env": r.Env, "lane": r.Lane, "repo": r.Repo, "sha": r.SHA, "run": r.Run,
		"subject": announcementSubject(r), "ref": announcementRef(r.Repo), "change": announcementChange(r),
	}
}

// appendAnnouncement writes the event in tx, the transaction that writes the record, and
// returns its ledger event id (the record's event_id). A ledger without the
// controller writes nothing and returns 0.
func appendAnnouncement(ctx context.Context, tx pgx.Tx, r announcement) (int64, error) {
	if ok, err := actor.Exists(ctx, tx, announceWriter); err != nil || !ok {
		return 0, err
	}
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: announcementStream(r), Type: deployFinishedType, Actor: announceWriter, Cause: r.Cause, Body: announcementBody(r)})
	if err != nil {
		return 0, err
	}
	return ev.EventID, nil
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			deployFinishedType: {
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
			deployFinishedType: {
				"env":    eventlog.Shaped("an environment name", envRE),
				"lane":   eventlog.Shaped("a lane name", laneRE),
				"repo":   eventlog.Shaped("a repository name", repoRE),
				"sha":    eventlog.Shaped("a 40-character lowercase commit", sha40),
				"run":    eventlog.Shaped("a pipeline run name", runRE),
				"change": eventlog.OneOf(changeSucceeded, changeFailed),
			},
		},
		After: map[string]func(body map[string]any) error{
			deployFinishedType: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				r := announcement{Env: s("env"), Lane: s("lane"), Repo: s("repo"), SHA: s("sha")}
				if body["subject"] != announcementSubject(r) || body["ref"] != announcementRef(r.Repo) {
					return eventlog.Invalid("%s: subject and ref are made of env, lane, sha and repo", deployFinishedType)
				}
				return nil
			},
		},
	})
}
