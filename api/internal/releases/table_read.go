package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	corev1 "k8s.io/api/core/v1"
)

// Step 2 of moving the release records into the database (2026-11-17, with
// the communication layer's step 3: duty/from-agent-tool-comm-2026-11-16-
// bus-step3-ci-checklist.md §6): reads move to the table only after the two
// are compared side by side. Each recorder pass now
//   - copies into the table every ConfigMap record whose run has no row
//     (records from before the table was written, 12:27 2026-11-17), without
//     an announcement: those releases happened long ago, and nobody should be
//     woken for them;
//   - asks Current of every deploying lane and environment twice, from the
//     ConfigMaps (the answer readers get) and from the rows, and counts and
//     logs where they differ.
// The ConfigMaps stay the truth. Nothing reads the rows' answer yet.

// TableReader reads the record table and copies a record into it.
type TableReader interface {
	// Records is the newest row of each successful run of this tenant.
	Records(ctx context.Context) ([]Record, error)
	// Backfill writes rec as a row without an announcement, unless its run
	// has a row already; it says whether it wrote one.
	Backfill(ctx context.Context, rec Record) (bool, error)
}

// SetTableReader hands the recorder its table reader; nil compares nothing.
func (s *Service) SetTableReader(r TableReader) {
	s.mu.Lock()
	s.reader = r
	s.mu.Unlock()
}

func (s *Service) tableReader() TableReader {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reader
}

// Querier is what ReadRecords needs of a pool.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ReadRecords is TableReader.Records over agentrt.release_record.
func ReadRecords(ctx context.Context, q Querier) ([]Record, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (run) run, pipeline, lane, env, deploys, revision, tag, from_run,
		started_at, completed_at, repos, images, missing, recorded_at
		FROM agentrt.release_record WHERE tenant = $1 AND succeeded
		ORDER BY run, release_record_id DESC`, Tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var rec Record
		var repos, images, missing []byte
		if err := rows.Scan(&rec.Run, &rec.Pipeline, &rec.Lane, &rec.Env, &rec.Deploys, &rec.Revision, &rec.Tag, &rec.FromRun,
			&rec.StartedAt, &rec.CompletedAt, &repos, &images, &missing, &rec.RecordedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(repos, &rec.Repos); err != nil {
			return nil, fmt.Errorf("row of %s: repos: %w", rec.Run, err)
		}
		if err := json.Unmarshal(images, &rec.Images); err != nil {
			return nil, fmt.Errorf("row of %s: images: %w", rec.Run, err)
		}
		if err := json.Unmarshal(missing, &rec.Missing); err != nil {
			return nil, fmt.Errorf("row of %s: missing: %w", rec.Run, err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// BackfillRecord is TableReader.Backfill in tx.
func BackfillRecord(ctx context.Context, tx pgx.Tx, rec Record) (bool, error) {
	if _, found, err := PriorEvent(ctx, tx, rec.Run); err != nil || found {
		return false, err
	}
	return true, InsertRecord(ctx, tx, rec, true, 0)
}

// Comparison is one pass's side-by-side reading.
type Comparison struct {
	// Backfilled: ConfigMap records copied into the table this pass.
	Backfilled int `json:"backfilled"`
	// Compared: deploying lane/environment pairs asked both ways; Differ of
	// them answered differently (Diffs say how).
	Compared int      `json:"compared"`
	Differ   int      `json:"differ"`
	Diffs    []string `json:"diffs,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// compare backfills and compares (see the top of this file). cms are the
// record ConfigMaps the pass listed.
func (s *Service) compare(ctx context.Context, cms []corev1.ConfigMap) *Comparison {
	r := s.tableReader()
	if r == nil {
		return nil
	}
	out := &Comparison{Diffs: []string{}}
	rows, err := r.Records(ctx)
	if err != nil {
		out.Err = "read rows: " + err.Error()
		return out
	}
	have := map[string]bool{}
	for _, row := range rows {
		have[row.Run] = true
	}
	for _, cm := range cms {
		var rec Record
		if err := json.Unmarshal([]byte(cm.Data[dataKey]), &rec); err != nil || rec.Run == "" || have[rec.Run] {
			continue
		}
		wrote, err := r.Backfill(ctx, rec)
		if err != nil {
			out.Err = "backfill " + rec.Run + ": " + err.Error()
			return out
		}
		if wrote {
			out.Backfilled++
			have[rec.Run] = true
		}
	}
	if out.Backfilled > 0 {
		if rows, err = r.Records(ctx); err != nil {
			out.Err = "read rows: " + err.Error()
			return out
		}
	}
	for _, pair := range s.rules.deployingPairs() {
		lane, env := pair[0], pair[1]
		var mine []Record
		for _, row := range rows {
			if row.Deploys && row.Lane == lane && row.Env == env {
				mine = append(mine, row)
			}
		}
		fromMaps, mapsErr := s.Current(ctx, lane, env)
		fromRows, rowsErr := s.settledNewest(ctx, mine, lane, env)
		out.Compared++
		if a, b := answerOf(fromMaps, mapsErr), answerOf(fromRows, rowsErr); a != b {
			out.Differ++
			out.Diffs = append(out.Diffs, fmt.Sprintf("%s/%s: configmaps %s, rows %s", lane, env, a, b))
		}
	}
	if out.Differ > 0 {
		slog.Warn("release records: the table answers differently", "differ", out.Differ, "diffs", out.Diffs)
	}
	return out
}

// answerOf names a Current answer by what a reader acts on: the run, or the
// kind of failure.
func answerOf(rec Record, err error) string {
	switch {
	case err == nil:
		return "run " + rec.Run
	case errors.Is(err, ErrUnsettled):
		return "unsettled"
	case errors.Is(err, ErrNoDeployment):
		return "no deployment"
	default:
		return "error"
	}
}

// deployingPairs are the lane/environment pairs a deploying rule names, in order.
func (r Rules) deployingPairs() [][2]string {
	seen := map[[2]string]bool{}
	var out [][2]string
	for _, rule := range r.Rules {
		p := [2]string{rule.Lane, rule.Env}
		if rule.Deploys && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0]+"/"+out[i][1] < out[j][0]+"/"+out[j][1] })
	return out
}
