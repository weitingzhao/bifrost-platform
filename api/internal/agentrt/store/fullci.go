package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// The daily full CI's records (controller/fullci.go): a note on the
// controller's stream for the runs a day started, and one for each red run,
// with the goal it proposed or the open goal of an earlier red with the
// same failed tests. The Console lists them with the other events; the
// controller reads them back so a red run is handled once.

// FullCI is a full_ci note's body.
type FullCI struct {
	// Started are the runs a day started.
	Started []string `json:"started,omitempty"`
	// Run is a red run, of Commit, and FailedKey its failed tests, sorted
	// and joined ("" when no test was named).
	Run       string `json:"run,omitempty"`
	Commit    string `json:"commit,omitempty"`
	FailedKey string `json:"failed_key,omitempty"`
	// Mission is the goal's mission the red run proposed; OpenMission and
	// OpenRun, the open goal of an earlier red with the same failed tests,
	// when none was proposed for it.
	Mission     string `json:"mission,omitempty"`
	OpenMission string `json:"open_mission,omitempty"`
	OpenRun     string `json:"open_run,omitempty"`
}

func (f FullCI) body() map[string]any {
	b := map[string]any{}
	if len(f.Started) > 0 {
		started := make([]any, len(f.Started))
		for i, s := range f.Started {
			started[i] = s
		}
		b["started"] = started
	}
	for k, v := range map[string]string{"run": f.Run, "commit": f.Commit, "failed_key": f.FailedKey,
		"mission": f.Mission, "open_mission": f.OpenMission, "open_run": f.OpenRun} {
		if v != "" {
			b[k] = v
		}
	}
	return b
}

// NoteFullCI records text and f on the controller's stream.
func NoteFullCI(ctx context.Context, db DB, text string, f FullCI) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text, "full_ci": f.body()}})
		return err
	})
}

// FullCIRedNoted says whether red run run has its note.
func FullCIRedNoted(ctx context.Context, db DB, run string) (bool, error) {
	var noted bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND actor_id = 'controller' AND body_json->'full_ci'->>'run' = $2)`, ControllerStream, run).Scan(&noted)
	})
	return noted, err
}

// FullCIOpenGoal is the latest red run with failed tests key whose goal's
// mission is still open (not done, stopped or expired), and that mission;
// "" for none.
func FullCIOpenGoal(ctx context.Context, db DB, key string) (run, mission string, err error) {
	if key == "" {
		return "", "", nil
	}
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT e.body_json->'full_ci'->>'run', m.mission_id FROM agentrt.event e
		  JOIN agentrt.mission m ON m.mission_id = e.body_json->'full_ci'->>'mission'
		  WHERE e.event_stream_id = $1 AND e.event_type = 'note' AND e.actor_id = 'controller'
		    AND e.body_json->'full_ci'->>'failed_key' = $2 AND m.state NOT IN ('done', 'stopped', 'expired')
		  ORDER BY e.seq DESC LIMIT 1`, ControllerStream, key).Scan(&run, &mission)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return run, mission, err
}
