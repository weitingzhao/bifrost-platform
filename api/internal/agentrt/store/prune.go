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
//     does not say when a commit reached main.
//
// Kept: a task still open; an accepted result not seen on main; a branch whose
// task this runtime does not have (PROD and STG push to the same
// repositories; each cleans its own); a branch that names something other
// than its attempt's pushed commit (moved since: nothing here says what it now
// holds).
//
// The host's agentd deletes (Prunable, through GET /agentrt/prunes), with its
// own Gitea identity and only while the branch still names that commit, and
// reports each (RecordPrunes): a deletion is a note on the task's mission
// (pruned), a refusal a note on the host's stream (prune_refused). Gitea
// refuses to delete a protected branch, and agent/** is protected until the
// Owner lifts it; so after a refusal nothing is handed out for PrunePause,
// and a refusal is a record, not an error.

const (
	PruneAfter = 7 * 24 * time.Hour
	PrunePause = 6 * time.Hour
	MaxPrunes  = 50 // handed out at a time
)

// agentRef is an attempt branch: AttemptRef's shape.
var (
	agentRef  = regexp.MustCompile(`^refs/heads/agent/(t_[0-9A-HJKMNP-TV-Z]{26})/a([1-9][0-9]{0,5})$`)
	commitHex = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// AgentHead is a branch of a repository on Gitea and the commit it names.
type AgentHead struct{ Repo, Ref, Commit string }

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

// PrunePausedUntil is when a refusal's pause ends, zero when none holds now.
func PrunePausedUntil(ctx context.Context, db DB, now time.Time) (time.Time, error) {
	var last *time.Time
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE event_type = 'note' AND body_json ? 'prune_refused'`).Scan(&last)
	})
	if err != nil || last == nil || !last.Add(PrunePause).After(now) {
		return time.Time{}, err
	}
	return last.Add(PrunePause), nil
}

// Prunable is the heads the rule lets go at now, at most MaxPrunes, in the
// order of their refs.
func Prunable(ctx context.Context, db DB, heads []AgentHead, now time.Time) ([]Prune, error) {
	heads = append([]AgentHead(nil), heads...)
	sort.Slice(heads, func(i, j int) bool {
		return heads[i].Repo < heads[j].Repo || heads[i].Repo == heads[j].Repo && heads[i].Ref < heads[j].Ref
	})
	var out []Prune
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		for _, h := range heads {
			p, ok, err := prunable(ctx, tx, h, now)
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

func prunable(ctx context.Context, tx pgx.Tx, h AgentHead, now time.Time) (Prune, bool, error) {
	m := agentRef.FindStringSubmatch(h.Ref)
	if m == nil {
		return Prune{}, false, nil
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
		return Prune{}, false, nil // another environment's task
	}
	if err != nil {
		return Prune{}, false, err
	}
	if attempt == "" || ref != h.Ref || commit != h.Commit || repo != h.Repo {
		return Prune{}, false, nil
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
			return Prune{}, false, err
		}
		switch {
		case accepted == "":
			return Prune{}, false, nil // accepted with no accepting verification: nothing here says which attempt
		case accepted != attempt:
			since, err = lastMove(ctx, tx, task, "accepted")
			why = "superseded: another attempt was accepted"
		case noMerge != "":
			since, err = lastMove(ctx, tx, task, "accepted")
			why = "accepted; its mission is not merged: " + noMerge
		default:
			var landed string
			err = tx.QueryRow(ctx, `SELECT at, body_json->>'landed' FROM agentrt.event WHERE task_id = $1 AND event_type = 'note'
			  AND body_json ? 'landed' ORDER BY event_id LIMIT 1`, task).Scan(&since, &landed)
			if errors.Is(err, pgx.ErrNoRows) {
				return Prune{}, false, nil
			}
			why = "accepted and on main as " + landed[:min(12, len(landed))]
		}
	default:
		return Prune{}, false, nil
	}
	if err != nil || since == nil || now.Sub(*since) < PruneAfter {
		return Prune{}, false, err
	}
	return Prune{Repo: h.Repo, Ref: h.Ref, Commit: h.Commit, Task: task, Why: fmt.Sprintf("%s (%s)", why, since.UTC().Format(time.RFC3339))}, true, nil
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
// its task's mission (pruned), a refused or failed one as a note on the
// host's stream (prune_refused), which pauses the handing out (PrunePause).
func RecordPrunes(ctx context.Context, db DB, host string, results []PruneResult) error {
	for _, r := range results {
		m := agentRef.FindStringSubmatch(r.Ref)
		switch {
		case m == nil:
			return invalid("%q is not an attempt branch", r.Ref)
		case !commitHex.MatchString(r.Commit):
			return invalid("%q is not a commit", r.Commit)
		case r.Outcome != "deleted" && r.Outcome != "gone" && r.Outcome != "refused" && r.Outcome != "failed":
			return invalid("outcome %q is none of deleted, gone, refused, failed", r.Outcome)
		}
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		for _, r := range results {
			task := agentRef.FindStringSubmatch(r.Ref)[1]
			detail := strings.Join(strings.Fields(strings.ToValidUTF8(r.Detail, "?")), " ")
			if len(detail) > maxPruneDetail {
				detail = strings.ToValidUTF8(detail[:maxPruneDetail], "") + "…"
			}
			if r.Outcome == "refused" || r.Outcome == "failed" {
				verb := map[string]string{"refused": "Gitea refused to delete", "failed": "could not reach Gitea to delete"}[r.Outcome]
				if _, err := Append(ctx, tx, Event{Stream: "host:" + host, Type: "note", Actor: hostActor(host),
					Body: map[string]any{"text": fmt.Sprintf("%s %s of %s (%s): %s", verb, r.Ref, r.Repo, r.Commit[:12], detail),
						"prune_refused": r.Ref}}); err != nil {
					return err
				}
				continue
			}
			var mission string
			err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&mission)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("no task %s", task)
			}
			if err != nil {
				return err
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
// main, whether main now holds it (mainTasks: a repository's main, task to the
// newest commit whose Agentrt-Task names it; ok false when it cannot be read
// now). The first sighting is the note the seven days count from. A mission
// not to be merged is passed over. It returns how many it recorded.
func RecordLandings(ctx context.Context, db DB, mainTasks func(context.Context, string) (map[string]string, bool)) (int, error) {
	if mainTasks == nil {
		return 0, nil
	}
	type open struct{ task, repo string }
	var tasks []open
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id, t.repo FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
		  WHERE t.role = 'implement' AND t.state = 'accepted' AND t.repo IS NOT NULL AND COALESCE(m.scope_json->>'no_merge', '') = ''
		    AND EXISTS (SELECT 1 FROM agentrt.attempt a WHERE a.task_id = t.task_id AND a.pushed_ref IS NOT NULL)
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'note' AND e.body_json ? 'landed')
		  ORDER BY t.repo, t.task_id`)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (open, error) {
			var o open
			return o, r.Scan(&o.task, &o.repo)
		})
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	mains := map[string]map[string]string{}
	for _, o := range tasks {
		main, read := mains[o.repo]
		if !read {
			var ok bool
			if main, ok = mainTasks(ctx, o.repo); !ok {
				main = nil
			}
			mains[o.repo] = main
		}
		sha := main[o.task]
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
					"text": fmt.Sprintf("its accepted result is on main of %s as %s", o.repo, sha[:12]), "landed": sha}})
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
