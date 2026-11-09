package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// SupersedeRequest is an Owner-approved list of obsolete tasks and the
// delivered implementations replacing them. It never asserts patch equality.
type SupersedeRequest struct {
	Env     string           `json:"env"`
	Entries []SupersedeEntry `json:"entries"`
	Reason  string           `json:"reason"`
}
type SupersedeEntry struct {
	Task        string `json:"task_id"`
	Version     int    `json:"version"`
	Replacement string `json:"replacement_task_id"`
	Commit      string `json:"commit"`
}

func (r SupersedeRequest) Validate() error {
	if r.Env != "prod" || strings.TrimSpace(r.Reason) == "" || len(r.Entries) == 0 || len(r.Entries) > MaxBatch {
		return invalid("superseding needs prod, a reason and 1 to %d entries", MaxBatch)
	}
	if len(r.Reason) > MaxNoMergeBytes || !utf8.ValidString(r.Reason) || strings.ContainsRune(r.Reason, 0) {
		return invalid("invalid superseding reason")
	}
	seen := map[string]bool{}
	for _, e := range r.Entries {
		if !landingRoot.MatchString(e.Task) || !landingRoot.MatchString(e.Replacement) || e.Version < 1 || !landingSHA.MatchString(e.Commit) || seen[e.Task] {
			return invalid("superseding needs unique task ids, positive versions and full commits")
		}
		seen[e.Task] = true
	}
	for _, e := range r.Entries {
		if seen[e.Replacement] {
			return invalid("a replacement cannot itself be superseded in this request")
		}
	}
	return nil
}

// SupersedeTasks rechecks under the task locks and commits the entire card
// atomically. Empty approval means read/check only. Existing task events
// retain the approval and replacement; no new table or event kind is needed.
func SupersedeTasks(ctx context.Context, db DB, r SupersedeRequest, approval string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if approval != "" && !approvalIDRE.MatchString(approval) {
		return invalid("superseding requires an approval id")
	}
	entries := slices.Clone(r.Entries)
	slices.SortFunc(entries, func(a, b SupersedeEntry) int { return strings.Compare(a.Task, b.Task) })
	return InTx(ctx, db, func(tx pgx.Tx) error {
		// NOWAIT follows existing task-close paths: a busy writer is not displaced.
		ids := []string{}
		for _, e := range entries {
			ids = append(ids, e.Task, e.Replacement)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE task_id=ANY($1) ORDER BY task_id FOR NO KEY UPDATE NOWAIT`, ids)
		if err != nil {
			return err
		}
		locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(locked) != len(ids) {
			return invalid("a named task does not exist")
		}
		for _, e := range entries {
			t, err := readTask(ctx, tx, e.Task, false)
			if err != nil {
				return err
			}
			why := fmt.Sprintf("superseded by %s at %s; approval %s: %s", e.Replacement, e.Commit, approval, r.Reason)
			// An ambiguous response may be retried, but another card cannot claim it.
			if approval != "" && t.state == "cancelled" {
				var prior string
				err = tx.QueryRow(ctx, `SELECT COALESCE(body_json->>'reason','') FROM agentrt.event WHERE task_id=$1 AND event_type='task_state_changed' ORDER BY event_id DESC LIMIT 1`, e.Task).Scan(&prior)
				if err != nil {
					return err
				}
				if prior == why {
					continue
				}
			}
			var version int
			if err = tx.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id=$1`, e.Task).Scan(&version); err != nil {
				return err
			}
			if version != e.Version || !slices.Contains([]string{"proposed", "awaiting_owner", "ready", "paused", "blocked", "rework", "submitted", "accepted"}, t.state) {
				return fmt.Errorf("%w: %s changed or is not an idle obsolete task", ErrState, e.Task)
			}
			moves := closeMoves
			if t.state == "accepted" {
				var repo string
				var delivered bool
				if err = tx.QueryRow(ctx, `SELECT COALESCE(repo,''), EXISTS(SELECT 1 FROM agentrt.event WHERE task_id=$1 AND event_type='note' AND body_json ? 'landed') FROM agentrt.task WHERE task_id=$1`, e.Task).Scan(&repo, &delivered); err != nil {
					return err
				}
				if delivered {
					return fmt.Errorf("%w: %s is already delivered", ErrState, e.Task)
				}
				// Lock an existing landing stream without creating one during precheck.
				// The task lock also serializes a first landing claim for this root.
				if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id=$1 FOR UPDATE`, "landing:"+repo); err != nil {
					return err
				}
				state, err := LandingState(ctx, tx, "landing:"+repo)
				if err != nil {
					return err
				}
				for _, op := range state.Operations {
					if op.Task == e.Task && op.Phase != "failed" && op.Phase != "discarded" {
						return fmt.Errorf("%w: %s has a live or delivered landing", ErrState, e.Task)
					}
				}
				// This Owner-approved replacement is narrower than mission closure.
				// Do not make accepted tasks cancellable through other close paths.
				moves = map[string][]string{"accepted": {"cancelled"}}
			}
			replacement, err := readTask(ctx, tx, e.Replacement, false)
			if err != nil {
				return err
			}
			var sameRepo, landed bool
			err = tx.QueryRow(ctx, `SELECT a.repo IS NOT DISTINCT FROM b.repo,
    EXISTS(SELECT 1 FROM agentrt.event WHERE task_id=b.task_id AND event_type='note' AND body_json->>'landed'=$3)
    FROM agentrt.task a JOIN agentrt.task b ON b.task_id=$2 WHERE a.task_id=$1`, e.Task, e.Replacement, e.Commit).Scan(&sameRepo, &landed)
			if err != nil {
				return err
			}
			if replacement.state != "accepted" || replacement.role != "implement" || replacement.mission != t.mission || !sameRepo || !landed {
				return invalid("replacement for %s is not a delivered implementation of this mission and repository", e.Task)
			}
			// Deliberately no cascading cancellation or attempt interruption.
			var busy bool
			err = tx.QueryRow(ctx, `SELECT
    EXISTS(SELECT 1 FROM agentrt.attempt WHERE task_id=$1 AND state IN ('leased','running','stale')) OR
    EXISTS(SELECT 1 FROM agentrt.task WHERE subject_task_id=$1 AND state NOT IN ('accepted','rejected','cancelled')) OR
    EXISTS(SELECT 1 FROM agentrt.task_dependency d JOIN agentrt.task t ON t.task_id=d.task_id WHERE d.depends_on_task_id=$1 AND t.state NOT IN ('accepted','rejected','cancelled'))`, e.Task).Scan(&busy)
			if err != nil {
				return err
			}
			if busy {
				return fmt.Errorf("%w: %s has live work, open checks or unfinished dependents", ErrState, e.Task)
			}
			if approval != "" {
				if err = moveTaskBy(ctx, tx, "owner", t, "cancelled", why, moves); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
