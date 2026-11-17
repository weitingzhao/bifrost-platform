// Package drift is the bus's entry for a reconciliation that found two
// answers apart: reconcile.drift.found.
//
// 2026-11-16: the landing lets a platform commit in on an incremental CI run
// and the full run of the push to main is the check on it. A full run that
// fails where the incremental run passed is such a drift: the commit is
// failed in the evidence ledger, PROD is held, and the line that pushed the
// commit has to be told. It is told here, on the topic the bus already had
// for "a reconciliation found a difference", not on an alert path of its own.
package drift

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the event.
const Type = "reconcile.drift.found"

// FullVsIncremental is the one kind there is: a commit's full CI run against
// the incremental run that let it land.
const FullVsIncremental = "full-vs-incremental"

// actor writes the events; the platform seeds it.
const actor = "controller"

var (
	repoRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	runRE    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
)

// Found is one drift: the two runs of a commit that do not agree.
type Found struct {
	Repo, Commit, Kind  string
	Passed, Contradicts string // the run that passed, and the run that says otherwise
}

// Stream is the ledger stream of one repository's drifts.
func Stream(repo string) string { return "drift:" + repo }

// Subject is "<repo>@<commit>:<kind>": a line follows its repository with
// match=<repo>@ and finds the commit, and through it who pushed it.
func Subject(f Found) string { return f.Repo + "@" + f.Commit + ":" + f.Kind }

// Ref is where a reader reads the commit's verdict as the ledger has it now.
func Ref(f Found) string { return "/api/v1/evidence/ci?repo=" + f.Repo + "&commit=" + f.Commit }

// Body is the event's body: the bus's fixed fields and the two runs.
func Body(f Found) map[string]any {
	return map[string]any{
		"repo": f.Repo, "commit": f.Commit, "kind": f.Kind, "passed_run": f.Passed, "contradicting_run": f.Contradicts,
		"subject": Subject(f), "ref": Ref(f), "key": f.Repo, "change": "found",
	}
}

// Valid says whether the bus can name the drift.
func Valid(f Found) bool {
	return repoRE.MatchString(f.Repo) && commitRE.MatchString(f.Commit) && f.Kind == FullVsIncremental &&
		runRE.MatchString(f.Passed) && runRE.MatchString(f.Contradicts) && f.Passed != f.Contradicts
}

// Announce appends one event. A drift the bus cannot name, and a ledger
// without the controller (a database the platform has not seeded), announce
// nothing: announced is false and the caller's own record stands.
func Announce(ctx context.Context, db eventlog.DB, f Found) (announced bool, err error) {
	if db == nil || !Valid(f) {
		return false, nil
	}
	err = eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var seeded bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.actor WHERE actor_id = $1)`, actor).Scan(&seeded); err != nil || !seeded {
			return err
		}
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(f.Repo), Type: Type, Actor: actor, Body: Body(f)}); err != nil {
			return err
		}
		announced = true
		return nil
	})
	return announced && err == nil, err
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			Type: {
				{Name: "repo", Kind: eventlog.Str},
				{Name: "commit", Kind: eventlog.Str},
				{Name: "kind", Kind: eventlog.Str},
				{Name: "passed_run", Kind: eventlog.Str},
				{Name: "contradicting_run", Kind: eventlog.Str},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			Type: {
				"repo":   eventlog.Shaped("a repository name", repoRE),
				"commit": eventlog.Shaped("a commit id", commitRE),
				"kind":   eventlog.OneOf(FullVsIncremental),
				"change": eventlog.OneOf("found"),
			},
		},
		After: map[string]func(body map[string]any) error{
			Type: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				f := Found{Repo: s("repo"), Commit: s("commit"), Kind: s("kind"), Passed: s("passed_run"), Contradicts: s("contradicting_run")}
				if !Valid(f) || body["subject"] != Subject(f) || body["ref"] != Ref(f) || body["key"] != f.Repo {
					return eventlog.Invalid("%s: subject, ref and key are made of repo, commit and kind, and the two runs differ", Type)
				}
				return nil
			},
		},
	})
}
