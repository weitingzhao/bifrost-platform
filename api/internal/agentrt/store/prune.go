package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Cleaning attempt branches (TD-383; Owner 10-20 「分支 B 用 agentd」). Every
// attempt pushes agent/<task>/a<n>; nothing deleted them. The rule
// (agent-config/work/multi-agent/PLAN-td383-merge-and-prune-2026-10-20.md
// 2.4): a branch goes PruneAfter after the event that made it go, counted on
// the runtime's own records:
//
//   - its task moved to cancelled or rejected (the move's event);
//   - its task is accepted and this is another attempt than the accepted one
//     (superseded; from the acceptance);
//   - it is the accepted attempt, and its mission is not to be merged
//     (no_merge; from the acceptance), or its result stands on main: from when
//     the controller first saw it there (RecordLandings), since git's history
//     does not say when a commit reached main, and only while main still holds
//     it when the branch is handed out (a landing reverted since keeps it;
//     review 436 A1).
//
// Kept: a task still open; an accepted result not on main, or reverted there;
// a branch whose task this runtime does not have (PROD and STG push to the
// same repositories; each cleans its own); a branch that names something
// other than its attempt's pushed commit (moved since: nothing here says what
// it now holds); whatever cannot be read now.
//
// The host's agentd deletes (Prunable, through GET /agentrt/prunes), with its
// own Gitea identity and only while the branch still names that commit, and
// reports each (RecordPrunes): a deletion is a note on the task's mission
// (pruned), a refusal a note on the host's stream (prune_refused), Gitea out
// of reach another (prune_failed). Gitea refuses to delete a protected branch,
// and agent/** is protected until the Owner lifts it; so after a refusal
// nothing is handed out for PrunePause (after Gitea was out of reach, for
// PruneRetry), and a refusal is a record, not an error.

const (
	PruneAfter = 7 * 24 * time.Hour
	PrunePause = 6 * time.Hour
	PruneRetry = 30 * time.Minute
	MaxPrunes  = 50 // handed out at a time
)

var (
	// agentRef is an attempt branch: AttemptRef's shape.
	agentRef  = regexp.MustCompile(`^refs/heads/agent/(t_[0-9A-HJKMNP-TV-Z]{26})/a([1-9][0-9]{0,5})$`)
	commitHex = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// repoName is a repository's name as a report may give it.
	repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	// userinfo is a credential in an address that git's words may carry.
	userinfo = regexp.MustCompile(`://[^/\s@'"]+@`)
)

// AgentHead is a branch of a repository on Gitea and the commit it names.
type AgentHead struct{ Repo, Ref, Commit string }

// OnMain is what a repository's main holds of the runtime's work: task → a
// result commit (an attempt's own commit) → the main commit it stands on main
// as (lineage.MainResults). A landing reverted on main is not in it.
type OnMain map[string]map[string]string

// MainResults reads a repository's main as OnMain; ok false when it cannot be
// read now.
type MainResults func(ctx context.Context, repo string) (OnMain, bool)

// Prune is a branch the rule lets go: its host deletes it if it still names
// Commit.
type Prune struct {
	Repo   string `json:"repo"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	Task   string `json:"task_id"`
	Why    string `json:"why"`
}

// PruneRepos are the repositories attempt branches were pushed to.
func PruneRepos(ctx context.Context, db DB) ([]string, error) {
	var repos []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT t.repo FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
		  WHERE a.pushed_ref IS NOT NULL AND t.repo IS NOT NULL ORDER BY 1`)
		if err != nil {
			return err
		}
		repos, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return repos, err
}

// PrunePausedUntil is when the last refusal's pause, or the last failure's
// shorter one, ends; zero when none holds now.
func PrunePausedUntil(ctx context.Context, db DB, now time.Time) (time.Time, error) {
	var refused, failed *time.Time
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT max(at) FILTER (WHERE body_json ? 'prune_refused'), max(at) FILTER (WHERE body_json ? 'prune_failed')
		  FROM agentrt.event WHERE event_type = 'note' AND (body_json ? 'prune_refused' OR body_json ? 'prune_failed')`).Scan(&refused, &failed)
	})
	var until time.Time
	if refused != nil {
		until = refused.Add(PrunePause)
	}
	if failed != nil && failed.Add(PruneRetry).After(until) {
		until = failed.Add(PruneRetry)
	}
	if err != nil || !until.After(now) {
		return time.Time{}, err
	}
	return until, nil
}

// Prunable is the heads the rule lets go at now, at most MaxPrunes, in the
// order of their refs. onMain is each repository's main as read now; a
// repository missing from it keeps the accepted attempts' branches whose
// results landed (whether they still stand cannot be told).
func Prunable(ctx context.Context, db DB, heads []AgentHead, onMain map[string]OnMain, now time.Time) ([]Prune, error) {
	heads = append([]AgentHead(nil), heads...)
	sort.Slice(heads, func(i, j int) bool {
		return heads[i].Repo < heads[j].Repo || heads[i].Repo == heads[j].Repo && heads[i].Ref < heads[j].Ref
	})
	var out []Prune
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		for _, h := range heads {
			p, ok, _, err := prunable(ctx, tx, h, onMain, now)
			if err != nil {
				return err
			}
			if ok {
				out = append(out, p)
				if len(out) == MaxPrunes {
					return nil
				}
			}
		}
		return nil
	})
	return out, err
}

// PruneMainsNeeded is the repositories whose main must be read now before
// Prunable can answer for heads: those with an accepted attempt's branch
// whose result was first seen on main PruneAfter ago (review 437 B3: main is
// read only where it decides something).
func PruneMainsNeeded(ctx context.Context, db DB, heads []AgentHead, now time.Time) ([]string, error) {
	need := map[string]bool{}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		for _, h := range heads {
			if need[h.Repo] {
				continue
			}
			_, _, main, err := prunable(ctx, tx, h, nil, now)
			if err != nil {
				return err
			}
			need[h.Repo] = main
		}
		return nil
	})
	var repos []string
	for repo, n := range need {
		if n {
			repos = append(repos, repo)
		}
	}
	sort.Strings(repos)
	return repos, err
}

// prunable says whether the rule lets h go at now: ok, or needMain when only
// main, not in onMain, is left to decide it.
func prunable(ctx context.Context, tx pgx.Tx, h AgentHead, onMain map[string]OnMain, now time.Time) (Prune, bool, bool, error) {
	no := func(err error) (Prune, bool, bool, error) { return Prune{}, false, false, err }
	m := agentRef.FindStringSubmatch(h.Ref)
	if m == nil {
		return no(nil)
	}
	task := m[1]
	n, _ := strconv.Atoi(m[2])
	var state, repo, noMerge, attempt, ref, commit string
	err := tx.QueryRow(ctx, `SELECT t.state, COALESCE(t.repo, ''), COALESCE(m.scope_json->>'no_merge', ''), COALESCE(a.attempt_id, ''),
	    COALESCE(a.pushed_ref, ''), COALESCE(a.pushed_commit, '')
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.attempt a ON a.task_id = t.task_id AND a.attempt_no = $2
	  WHERE t.task_id = $1`, task, n).Scan(&state, &repo, &noMerge, &attempt, &ref, &commit)
	if errors.Is(err, pgx.ErrNoRows) {
		return no(nil) // another environment's task
	}
	if err != nil {
		return no(err)
	}
	if attempt == "" || ref != h.Ref || commit != h.Commit || repo != h.Repo {
		return no(nil)
	}
	var since *time.Time
	why := ""
	switch state {
	case "cancelled", "rejected":
		since, err = lastMove(ctx, tx, task, state)
		why = "its task is " + state
	case "accepted":
		var accepted string
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT attempt_id FROM agentrt.verification WHERE task_id = $1 AND accepts_attempt
		  ORDER BY created_at DESC LIMIT 1), '')`, task).Scan(&accepted); err != nil {
			return no(err)
		}
		switch {
		case accepted == "":
			return no(nil) // accepted with no accepting verification: nothing here says which attempt
		case accepted != attempt:
			since, err = lastMove(ctx, tx, task, "accepted")
			why = "superseded: another attempt was accepted"
		case noMerge != "":
			since, err = lastMove(ctx, tx, task, "accepted")
			why = "accepted; its mission is not merged: " + noMerge
		default:
			// The first sighting starts the clock; main must still hold this
			// very commit now.
			err = tx.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE task_id = $1 AND event_type = 'note'
			  AND body_json ? 'landed' ORDER BY event_id LIMIT 1`, task).Scan(&since)
			if errors.Is(err, pgx.ErrNoRows) {
				return no(nil)
			}
			if err != nil || now.Sub(*since) < PruneAfter {
				return no(err)
			}
			om, read := onMain[h.Repo]
			if !read {
				return Prune{}, false, true, nil
			}
			stands := om[task][h.Commit]
			if stands == "" {
				return no(nil)
			}
			why = "accepted and on main as " + stands[:min(12, len(stands))]
		}
	default:
		return no(nil)
	}
	if err != nil || since == nil || now.Sub(*since) < PruneAfter {
		return no(err)
	}
	return Prune{Repo: h.Repo, Ref: h.Ref, Commit: h.Commit, Task: task, Why: fmt.Sprintf("%s (%s)", why, since.UTC().Format(time.RFC3339))}, true, false, nil
}

// lastMove is when task last moved to state, nil for never.
func lastMove(ctx context.Context, tx pgx.Tx, task, state string) (*time.Time, error) {
	var at *time.Time
	err := tx.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = $2`, task, state).Scan(&at)
	return at, err
}

// PruneResult is what a host did with one Prune.
type PruneResult struct {
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
	Outcome string `json:"outcome"` // deleted, gone (it was not there), refused (Gitea said no), failed (Gitea not reached)
	Detail  string `json:"detail"`  // git's own words for refused and failed
}

// maxPruneDetail is the most of a refusal's words a note keeps.
const maxPruneDetail = 500

// RecordPrunes records what host did: a deleted or gone branch as a note on
// its task's mission (pruned; once), a refused one as a note on the host's
// stream (prune_refused), which pauses the handing out for PrunePause, and a
// failed one likewise (prune_failed, PruneRetry). Each result must be of an
// attempt this runtime has, as it pushed it (repository, branch, commit), of
// a task the rule may let go, of a branch the rule lets go at now, main aside
// (review 436 B2, 437 B2); one that is not is refused, with the whole report.
func RecordPrunes(ctx context.Context, db DB, host string, results []PruneResult, now time.Time) error {
	for _, r := range results {
		switch {
		case !repoName.MatchString(r.Repo):
			return invalid("%q is not a repository's name", r.Repo)
		case agentRef.FindStringSubmatch(r.Ref) == nil:
			return invalid("%q is not an attempt branch", r.Ref)
		case !commitHex.MatchString(r.Commit):
			return invalid("%q is not a commit", r.Commit)
		case r.Outcome != "deleted" && r.Outcome != "gone" && r.Outcome != "refused" && r.Outcome != "failed":
			return invalid("outcome %q is none of deleted, gone, refused, failed", r.Outcome)
		}
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		for _, r := range results {
			m := agentRef.FindStringSubmatch(r.Ref)
			task := m[1]
			n, _ := strconv.Atoi(m[2])
			var mission, state, repo, ref, commit string
			err := tx.QueryRow(ctx, `SELECT t.mission_id, t.state, COALESCE(t.repo, ''), COALESCE(a.pushed_ref, ''), COALESCE(a.pushed_commit, '')
			  FROM agentrt.task t JOIN agentrt.attempt a ON a.task_id = t.task_id AND a.attempt_no = $2 WHERE t.task_id = $1`,
				task, n).Scan(&mission, &state, &repo, &ref, &commit)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("no attempt %d of task %s", n, task)
			}
			if err != nil {
				return err
			}
			if repo != r.Repo || ref != r.Ref || commit != r.Commit {
				return invalid("%s of %s naming %s is not what attempt %d of %s pushed", r.Ref, r.Repo, r.Commit[:12], n, task)
			}
			if state != "cancelled" && state != "rejected" && state != "accepted" {
				return invalid("task %s is %s: its branches are not let go", task, state)
			}
			// What the rule lets go stays let go, but for main (read when it is
			// handed out); a report of a branch it does not let go is refused
			// (review 437 B2).
			if _, ok, needMain, err := prunable(ctx, tx, AgentHead{Repo: r.Repo, Ref: r.Ref, Commit: r.Commit}, nil, now); err != nil {
				return err
			} else if !ok && !needMain {
				return invalid("the rule does not let %s go now", r.Ref)
			}
			detail := userinfo.ReplaceAllString(strings.Join(strings.Fields(strings.ToValidUTF8(r.Detail, "?")), " "), "://<secret>@")
			if len(detail) > maxPruneDetail {
				detail = strings.ToValidUTF8(detail[:maxPruneDetail], "") + "…"
			}
			if r.Outcome == "refused" || r.Outcome == "failed" {
				text, field := "Gitea refused to delete", "prune_refused"
				if r.Outcome == "failed" {
					text, field = "could not reach Gitea to delete", "prune_failed"
				}
				if _, err := Append(ctx, tx, Event{Stream: "host:" + host, Type: "note", Actor: hostActor(host),
					Body: map[string]any{"text": fmt.Sprintf("%s %s of %s (%s): %s", text, r.Ref, r.Repo, r.Commit[:12], detail),
						field: r.Ref}}); err != nil {
					return err
				}
				continue
			}
			var told bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'note'
			  AND body_json->>'pruned' = $2)`, task, r.Ref).Scan(&told); err != nil {
				return err
			}
			if told {
				continue // a report sent again after its answer was lost
			}
			text := fmt.Sprintf("host %s deleted %s of %s (it named %s)", host, r.Ref, r.Repo, r.Commit[:12])
			if r.Outcome == "gone" {
				text = fmt.Sprintf("%s of %s was already gone when host %s came to delete it", r.Ref, r.Repo, host)
			}
			if _, err = Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: hostActor(host),
				MissionID: mission, TaskID: task, Body: map[string]any{"text": text, "pruned": r.Ref}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordLandings records, for each accepted implementation not yet seen on
// main, whether main now holds its accepted attempt's very commit (merged as
// a cherry-pick of it, or as it is; a reverted landing does not count; review
// 436 A2). The first sighting is the note the seven days count from. A
// mission not to be merged is passed over; a repository whose main cannot be
// read now is tried again next time. It returns how many it recorded.
func RecordLandings(ctx context.Context, db DB, mainResults MainResults) (int, error) {
	if mainResults == nil {
		return 0, nil
	}
	type open struct{ task, repo, commit string }
	var tasks []open
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id, t.repo, a.pushed_commit FROM agentrt.task t
		  JOIN agentrt.mission m ON m.mission_id = t.mission_id
		  JOIN LATERAL (SELECT v.attempt_id FROM agentrt.verification v WHERE v.task_id = t.task_id AND v.accepts_attempt
		    ORDER BY v.created_at DESC LIMIT 1) v ON true
		  JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id AND a.pushed_commit IS NOT NULL
		  WHERE t.role = 'implement' AND t.state = 'accepted' AND t.repo IS NOT NULL AND COALESCE(m.scope_json->>'no_merge', '') = ''
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'note' AND e.body_json ? 'landed')
		  ORDER BY t.repo, t.task_id`)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (open, error) {
			var o open
			return o, r.Scan(&o.task, &o.repo, &o.commit)
		})
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	mains := map[string]OnMain{}
	for _, o := range tasks {
		main, read := mains[o.repo]
		if !read {
			var ok bool
			if main, ok = mainResults(ctx, o.repo); !ok {
				main = nil
			}
			mains[o.repo] = main
		}
		sha := main[o.task][o.commit]
		if sha == "" {
			continue
		}
		did := false
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			did = false
			t, err := readTask(ctx, tx, o.task, true)
			if err != nil || t.state != "accepted" {
				return err
			}
			var seen bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'note'
			  AND body_json ? 'landed')`, o.task).Scan(&seen); err != nil || seen {
				return err
			}
			did = true
			_, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "note", Actor: "controller", MissionID: t.mission,
				PlanNode: t.step, TaskID: t.id, Body: map[string]any{
					"text": fmt.Sprintf("its accepted result %s is on main of %s as %s", o.commit[:12], o.repo, sha[:12]), "landed": sha}})
			return err
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", o.task, err))
		} else if did {
			n++
		}
	}
	return n, errors.Join(errs...)
}
