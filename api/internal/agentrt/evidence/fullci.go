package evidence

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"time"

	"github.com/jackc/pgx/v5"
)

// The daily full CI's records (controller/fullci.go): a note on the
// controller's stream for the runs a day started, and one for each red run,
// with the pipeline tasks that failed and the landed commits whose own CI
// run left a module out. The Console lists them with the other events and
// through GET /units/full-ci; the controller reads them back so a red run is
// handled once.

// ControllerStream is the controller's own stream, where the notes of the
// daily full CI are written. It is the store's ControllerStream
// (controller.TestEvidenceWritesToTheControllerStream).
const ControllerStream = "controller"

// FullCISuspect is a commit landed since the last green full run whose own
// CI run left a module out: Run is that CI run and Modules its run-modules
// result as written (the names of the modules that ran, each between
// spaces).
type FullCISuspect struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Run     string `json:"run"`
	Modules string `json:"modules"`
}

// FullCI is a full_ci note's body.
type FullCI struct {
	// Started are the runs a day started.
	Started []string `json:"started,omitempty"`
	// Run is a red run, of Commit, and FailedKey its failed tests, sorted
	// and joined ("" when no test was named).
	Run       string `json:"run,omitempty"`
	Commit    string `json:"commit,omitempty"`
	FailedKey string `json:"failed_key,omitempty"`
	// FailedTasks are the pipeline tasks of the red run that failed.
	FailedTasks []string `json:"failed_tasks,omitempty"`
	// Suspects are the commits landed since the last green full run whose
	// own CI run left a module out; Unread counts the landings whose CI run
	// or its run-modules result could not be read. Unreconciled says why
	// the landings were not, or not all, looked at; with it, no suspect
	// does not mean none.
	Suspects     []FullCISuspect `json:"suspects,omitempty"`
	Unread       int             `json:"unread,omitempty"`
	Unreconciled string          `json:"unreconciled,omitempty"`
}

func (f FullCI) body() map[string]any {
	b := map[string]any{}
	if f.Run != "" {
		b["alert"] = true
	}
	list := func(key string, values []string) {
		if len(values) == 0 {
			return
		}
		out := make([]any, len(values))
		for i, s := range values {
			out[i] = s
		}
		b[key] = out
	}
	list("started", f.Started)
	list("failed_tasks", f.FailedTasks)
	if len(f.Suspects) > 0 {
		suspects := make([]any, len(f.Suspects))
		for i, s := range f.Suspects {
			suspects[i] = map[string]any{"commit": s.Commit, "subject": s.Subject, "run": s.Run, "modules": s.Modules}
		}
		b["suspects"] = suspects
	}
	if f.Unread > 0 {
		b["unread"] = f.Unread
	}
	for k, v := range map[string]string{"run": f.Run, "commit": f.Commit, "failed_key": f.FailedKey, "unreconciled": f.Unreconciled} {
		if v != "" {
			b[k] = v
		}
	}
	return b
}

// NoteFullCI records text and f on the controller's stream.
func NoteFullCI(ctx context.Context, db eventlog.DB, text string, f FullCI) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: ControllerStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text, "full_ci": f.body()}})
		return err
	})
}

// FullCIRedNoted says whether red run run has its note.
func FullCIRedNoted(ctx context.Context, db eventlog.DB, run string) (bool, error) {
	var noted bool
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND actor_id = 'controller' AND body_json->'full_ci'->>'run' = $2)`, ControllerStream, run).Scan(&noted)
	})
	return noted, err
}

// FullCINote is one full_ci note as recorded: when, its text and its body.
type FullCINote struct {
	At   time.Time
	Text string
	Body map[string]any
}

// FullCINotes lists the controller's last limit full_ci notes, newest first.
func FullCINotes(ctx context.Context, db eventlog.DB, limit int) ([]FullCINote, error) {
	out := []FullCINote{}
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = out[:0]
		rows, err := tx.Query(ctx, `SELECT at, COALESCE(body_json->>'text', ''), body_json->'full_ci' FROM agentrt.event
		  WHERE event_stream_id = $1 AND event_type = 'note' AND actor_id = 'controller' AND body_json ? 'full_ci'
		  ORDER BY seq DESC LIMIT $2`, ControllerStream, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n FullCINote
			var body []byte
			if err := rows.Scan(&n.At, &n.Text, &body); err != nil {
				return err
			}
			if err := json.Unmarshal(body, &n.Body); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}
