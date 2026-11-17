package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// The release record table (agentrt 0027, agentrt.release_record) next to the
// ConfigMaps (2026-11-17, step 1 of moving the records into the database):
// every record the recorder writes as a ConfigMap is also a row, written with
// the bus's release.deploy.finished in one transaction by the writer the
// server hands in (SetTable). The ConfigMaps stay the truth until the rows
// are compared with them; a row that cannot be written is logged and counted,
// and never stops a ConfigMap.

// TableWriter writes one record as a row (and announces it). succeeded is
// whether the run succeeded; the recorder writes successful runs only, so far.
type TableWriter func(ctx context.Context, rec Record, succeeded bool) error

// SetTable hands the recorder its table writer; nil writes no rows.
func (s *Service) SetTable(w TableWriter) {
	s.mu.Lock()
	s.table = w
	s.mu.Unlock()
}

func (s *Service) tableWriter() TableWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.table
}

// writeRow writes rec through the table writer, when there is one, and says
// whether a row was written.
func (s *Service) writeRow(ctx context.Context, rec Record, succeeded bool) (bool, error) {
	w := s.tableWriter()
	if w == nil {
		return false, nil
	}
	if err := w(ctx, rec, succeeded); err != nil {
		slog.Warn("release record: the row was not written", "run", rec.Run, "err", err)
		return false, err
	}
	return true, nil
}

// Tenant is the platform's own tenant in the record table.
const Tenant = "rocket"

// InsertRecord appends rec as a row of agentrt.release_record in tx. eventID
// is the ledger event that announced it, 0 for none (NULL).
func InsertRecord(ctx context.Context, tx pgx.Tx, rec Record, succeeded bool, eventID int64) error {
	repos, err := json.Marshal(nonNilRepos(rec.Repos))
	if err != nil {
		return err
	}
	images, err := json.Marshal(nonNilImages(rec.Images))
	if err != nil {
		return err
	}
	missing, err := json.Marshal(nonNilMissing(rec.Missing))
	if err != nil {
		return err
	}
	var event any
	if eventID > 0 {
		event = eventID
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.release_record
		(tenant, run, pipeline, lane, env, deploys, succeeded, revision, tag, from_run,
		 started_at, completed_at, repos, images, missing, record_version, event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,$15::jsonb,$16,$17)`,
		Tenant, rec.Run, rec.Pipeline, rec.Lane, rec.Env, rec.Deploys, succeeded, rec.Revision, rec.Tag, rec.FromRun,
		rec.StartedAt, rec.CompletedAt, string(repos), string(images), string(missing), recordVersion, event)
	if err != nil {
		return fmt.Errorf("insert release record %s: %w", rec.Run, err)
	}
	return nil
}

// PriorEvent reports whether the table already has a row of run (for this
// tenant), and the ledger event its first row names (0 for none). A record
// built again is a new row (0027) but the same release: it is not announced
// again, and its row names the first announcement.
func PriorEvent(ctx context.Context, tx pgx.Tx, run string) (eventID int64, found bool, err error) {
	var id *int64
	err = tx.QueryRow(ctx, `SELECT event_id FROM agentrt.release_record
		WHERE tenant = $1 AND run = $2 ORDER BY release_record_id LIMIT 1`, Tenant, run).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if id != nil {
		eventID = *id
	}
	return eventID, true, nil
}

// Announced is the repository and commit a record's announcement names: the
// repository whose built commit is the run's revision. ok is false when no
// repository's is (the record is written without an announcement).
func Announced(rec Record) (repo, sha string, ok bool) {
	if rec.Revision == "" {
		return "", "", false
	}
	var found []string
	for name, b := range rec.Repos {
		if b.SHA == rec.Revision {
			found = append(found, name)
		}
	}
	if len(found) != 1 {
		return "", "", false
	}
	return found[0], rec.Revision, true
}

func nonNilRepos(m map[string]RepoBuild) map[string]RepoBuild {
	if m == nil {
		return map[string]RepoBuild{}
	}
	return m
}

func nonNilImages(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilMissing(m []string) []string {
	if m == nil {
		return []string{}
	}
	return m
}
