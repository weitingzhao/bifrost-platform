package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A ready task no executor may take any more (TD-395; PROD M-22, 2026-10-20:
// codex, cursor and claude each failed its only task three times in a row,
// TD-381 withdrew all three, and the task sat at ready with no card, its
// mission open, nobody told).
//
// The controller's pass blocks a ready task of an active mission once every
// candidate on its route that may still take it has been withdrawn by the
// backoff (retry.go), naming each and the attempts it failed; the task then
// gets the blocked card (blockedcard.go), which recommends not doing it
// (blockedrecommend.go R6). "Give it one more round" offers it once more to
// each withdrawn executor. The pass reads the tasks' state, not a change of
// it, so a task left this way before the pass existed is blocked by its
// first run.
//
// Not blocked here: a gate task (gates.go ends one that cannot run itself),
// a task with no candidate left that was never withdrawn
// (an empty route, candidates retired or excluded: no answer on a card makes
// one), a brief (its sender is answered by brief.go, and briefs get no
// card), a task or mission a stop or pause holds, and work of a mission
// started in a standing envelope whose rule is not in force.

// withdrawnBlockMark starts the reason of a task blocked because every
// executor on its route was withdrawn (a card's recommendation tells such a
// block by it).
const withdrawnBlockMark = "no executor on its route may take it any more: "

// BlockWithdrawn is one pass blocking every ready task no executor may take
// any more, with the reason. It returns how many tasks it blocked. One that
// cannot be blocked now does not hold up the others; the next pass tries it
// again.
func BlockWithdrawn(ctx context.Context, db DB) (int, error) {
	n := 0
	var errs []error
	after := ""
	page := int(blockedPage.Load())
	for {
		var ids []string
		// Only tasks with a run of failuresToStop at their end are read
		// further: most ready tasks have none.
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
			  WHERE t.state = 'ready' AND m.state = 'active' AND t.role <> 'brief' AND t.effect_class = ANY($1) AND t.task_id > $2
			    AND (SELECT count(*) FROM (SELECT a.state, a.end_how FROM agentrt.attempt a WHERE a.task_id = t.task_id
			      ORDER BY a.attempt_no DESC LIMIT $3) l WHERE l.state = 'failed' AND l.end_how = ANY($4)) = $3
			  ORDER BY t.task_id LIMIT $5`, dispatched, after, failuresToStop, executorEnds, page)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			did, err := blockNoneLeft(ctx, db, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", id, err))
			} else if did {
				n++
			}
		}
		if len(ids) < page {
			return n, errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

// blockNoneLeft blocks one task if no executor may take it any more, read
// under the locks a claim takes, in its order: the capability lock (what the
// candidates and restrictions are), the task, then its mission. A claim
// skips a task this holds, and one that holds the task first leases it or
// leaves it as it was.
func blockNoneLeft(ctx context.Context, db DB, taskID string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.state != "ready" || t.role == "brief" {
			return err
		}
		if member, err := isGateMember(ctx, tx, taskID); err != nil || member {
			return err // its batch ends it (gateend.go)
		}
		m, err := readMissionBudget(ctx, tx, t.mission, "FOR SHARE")
		if err != nil || m.state != "active" {
			return err
		}
		if stop, err := held(ctx, tx, []scope{{"mission", t.mission}, {"task", t.id}}, "stop", "pause"); err != nil || stop {
			return err
		}
		why, err := noneLeft(ctx, tx, t.id, m.envelope)
		if err != nil || why == "" {
			return err
		}
		did = true
		return moveTask(ctx, tx, "controller", t, "blocked", why)
	})
	return did && err == nil, err
}

// noneLeft is the reason to block task id when every candidate on its route
// that may still take it (its hard checks and competence as a claim applies
// them, its executor one the mission's envelope allows) is withdrawn by its
// failures, and at least one is: else "".
func noneLeft(ctx context.Context, tx pgx.Tx, id, envelope string) (string, error) {
	c, err := candidateOf(ctx, tx, id)
	if err != nil || !slices.Contains(dispatched, c.effect) {
		return "", err
	}
	var allowed []string
	if envelope != "" {
		env, err := (ruleCache{}).get(ctx, tx, envelope)
		if err != nil || env == nil || env.Envelope == nil {
			return "", err
		}
		allowed = env.Envelope.Executors
	}
	cp, err := readCapabilities(ctx, tx)
	if err != nil {
		return "", err
	}
	in, err := inputWith(ctx, tx, cp, c)
	if err != nil {
		return "", err
	}
	run, err := readFailureRun(ctx, tx, id)
	if err != nil {
		return "", err
	}
	byID := map[string]jobCandidate{}
	for _, jc := range in.candidates {
		byID[jc.id] = jc
	}
	var gone []string
	seen := map[string]bool{}
	for _, cid := range c.route {
		jc, ok := byID[cid]
		if !ok || (envelope != "" && !slices.Contains(allowed, jc.executor)) {
			continue
		}
		tl := Tool{Executor: jc.executor, Provider: jc.provider}
		if len(in.exclusion(tl)) > 0 || len(in.unfit(jc)) > 0 {
			continue
		}
		if _, withdrawn := run.hold(jc.executor); !withdrawn {
			return "", nil
		}
		if !seen[jc.executor] {
			seen[jc.executor] = true
			gone = append(gone, withdrawnText(jc.executor, run.by[jc.executor]))
		}
	}
	if len(gone) == 0 {
		return "", nil
	}
	return withdrawnBlockMark + strings.Join(gone, "; ") +
		" (an executor is withdrawn after " + strconv.Itoa(failuresToStop) + " failures in a row of its own doing)", nil
}

// withdrawnText names an executor and the attempts of its run, oldest first,
// and how the latest ended: "exec:codex failed attempts 1, 4, 7 in a row (the
// last: exit)".
func withdrawnText(executor string, r runOf) string {
	nos := make([]string, 0, len(r.nos))
	for i := len(r.nos) - 1; i >= 0; i-- {
		nos = append(nos, strconv.Itoa(r.nos[i]))
	}
	return fmt.Sprintf("%s failed attempts %s in a row (the last: %s)", executor, strings.Join(nos, ", "), r.how)
}
