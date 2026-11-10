package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Wave is the ledger fold of a coordinator's batch of branch work.
type Wave struct {
	ID       string `json:"id"`
	Repo     string `json:"repo"`
	PlanRoot string `json:"plan_root"`
	N        int    `json:"n"`
	Base     string `json:"base"`
	State    string `json:"state"`
}

// Serialize numbering, membership checks and closure before any proposal
// stream is written. Ending a wave takes no task or mission row locks.
func waveLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742115)`)
	return err
}

func wavesTx(ctx context.Context, tx pgx.Tx, repo string) ([]Wave, error) {
	rows, err := tx.Query(ctx, `SELECT event_type, body_json FROM agentrt.event WHERE event_type IN ('wave_opened','wave_closed','wave_abandoned') ORDER BY event_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Wave{}
	index := map[string]int{}
	for rows.Next() {
		var kind string
		var body []byte
		if err := rows.Scan(&kind, &body); err != nil {
			return nil, err
		}
		var v Wave
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, err
		}
		if kind == "wave_opened" {
			v.State = "open"
			index[v.ID] = len(out)
			out = append(out, v)
		} else if i, ok := index[v.ID]; ok {
			if kind == "wave_closed" {
				out[i].State = "closed"
			} else {
				out[i].State = "abandoned"
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filtered := []Wave{}
	for _, v := range out {
		if repo == "" || v.Repo == repo {
			filtered = append(filtered, v)
		}
	}
	return filtered, nil
}

func Waves(ctx context.Context, db DB, repo string) ([]Wave, error) {
	var out []Wave
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = wavesTx(ctx, tx, repo)
		return err
	})
	return out, err
}

func OpenWave(ctx context.Context, db DB, w Writer, repo, root string, head BranchHead) (Wave, error) {
	var out Wave
	if repo != "bifrost-platform" {
		return out, invalid("only bifrost-platform takes waves for now")
	}
	if root == "" {
		return out, invalid("plan_root is required")
	}
	if head == nil {
		return out, invalid("branch-head reader is unavailable")
	}
	base, err := head(ctx, repo, "refs/heads/main")
	if err != nil {
		return out, err
	}
	if !commitHex.MatchString(base) {
		return out, invalid("main has no valid head")
	}
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return err
		}
		if err := waveLock(ctx, tx); err != nil {
			return err
		}
		all, err := wavesTx(ctx, tx, "")
		if err != nil {
			return err
		}
		n := 1
		for _, v := range all {
			if v.PlanRoot == root && v.N >= n {
				n = v.N + 1
			}
		}
		out = Wave{ID: "wave_" + NewTaskID(), Repo: repo, PlanRoot: root, N: n, Base: base, State: "open"}
		_, err = Append(ctx, tx, Event{Stream: "waves", Type: "wave_opened", Actor: w.Actor, Body: map[string]any{"id": out.ID, "repo": repo, "plan_root": root, "n": n, "base": base}})
		return err
	})
	return out, err
}

func EndWave(ctx context.Context, db DB, w Writer, id string, abandon bool) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return err
		}
		if err := waveLock(ctx, tx); err != nil {
			return err
		}
		all, err := wavesTx(ctx, tx, "")
		if err != nil {
			return err
		}
		var found *Wave
		for i := range all {
			if all[i].ID == id {
				found = &all[i]
				break
			}
		}
		if found == nil || found.State != "open" {
			return invalid("wave %s is not open", id)
		}
		if !abandon {
			members, err := waveMembers(ctx, tx, id)
			if err != nil {
				return err
			}
			pending := []string{}
			for _, m := range members {
				if m.ready {
					continue
				}
				pending = append(pending, m.task)
			}
			if len(pending) > 0 {
				return invalid("wave %s has members not yet accepted: %v", id, pending)
			}
		}
		kind := "wave_closed"
		if abandon {
			kind = "wave_abandoned"
		}
		_, err = Append(ctx, tx, Event{Stream: "waves", Type: kind, Actor: w.Actor, Body: map[string]any{"id": id}})
		return err
	})
}

func checkTaskWave(ctx context.Context, tx pgx.Tx, t TaskProposal) error {
	if t.Wave == "" {
		return nil
	}
	if t.Effect != "branch" || t.Urgent {
		return invalid("task %s: a wave takes branch tasks that are not urgent", t.Ref)
	}
	if err := waveLock(ctx, tx); err != nil {
		return err
	}
	all, err := wavesTx(ctx, tx, "")
	if err != nil {
		return err
	}
	for _, v := range all {
		if v.ID == t.Wave {
			if v.State != "open" {
				return invalid("wave %s is not open", t.Wave)
			}
			if v.Repo != t.Repo {
				return invalid("wave %s is for another repo", t.Wave)
			}
			return nil
		}
	}
	return invalid("wave %s is not open", t.Wave)
}

// waveMember is one task proposed into a wave, as the hold reads it.
type waveMember struct {
	task, wave string
	// ready: accepted, rejected, cancelled or already landed; a wave closes
	// and lands only when every member is.
	ready bool
	// landed: its change is on main already.
	landed bool
}

// waveMembers lists the tasks proposed into wave id (id "" lists every
// wave's), in task order.
func waveMembers(ctx context.Context, tx pgx.Tx, id string) ([]waveMember, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT t.task_id, e.body_json->>'wave', t.state FROM agentrt.task t
	  JOIN agentrt.event e ON e.task_id=t.task_id AND e.event_type='task_proposed'
	  WHERE COALESCE(e.body_json->>'wave','') <> '' AND ($1 = '' OR e.body_json->>'wave' = $1)
	  ORDER BY t.task_id`, id)
	if err != nil {
		return nil, err
	}
	type row struct{ task, wave, state string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.task, &r.wave, &r.state); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]waveMember, 0, len(all))
	for _, r := range all {
		landed, err := rootLandedCommit(ctx, tx, r.task)
		if err != nil {
			return nil, err
		}
		m := waveMember{task: r.task, wave: r.wave, landed: landed != ""}
		m.ready = m.landed || r.state == "accepted" || r.state == "rejected" || r.state == "cancelled"
		out = append(out, m)
	}
	return out, nil
}

// WaveHeld is the set of tasks of repo a wave keeps out of the landing
// queue (AW, option 1 of 2026-11-10: hold and release). A member of an open
// or abandoned wave is held: it lands only with its wave, and an abandoned
// wave lands nothing. A member of a closed wave is held until every member
// of that wave is ready (accepted, rejected, cancelled or landed): then the
// members not yet landed are released together, so the batch landing takes
// them in one batch (one landing CI, one grant, one push). A member sent
// back for rework by a red batch holds its wave again until it is accepted.
func WaveHeld(ctx context.Context, tx pgx.Tx, repo string) (map[string]bool, error) {
	waves, err := wavesTx(ctx, tx, repo)
	if err != nil {
		return nil, err
	}
	state := map[string]string{}
	for _, w := range waves {
		state[w.ID] = w.State
	}
	members, err := waveMembers(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	ready := map[string]bool{}
	for _, m := range members {
		if _, ok := state[m.wave]; !ok {
			continue // another repository's wave
		}
		if _, seen := ready[m.wave]; !seen {
			ready[m.wave] = true
		}
		ready[m.wave] = ready[m.wave] && m.ready
	}
	held := map[string]bool{}
	for _, m := range members {
		st, ok := state[m.wave]
		if !ok || m.landed {
			continue
		}
		if st != "closed" || !ready[m.wave] {
			held[m.task] = true
		}
	}
	return held, nil
}
