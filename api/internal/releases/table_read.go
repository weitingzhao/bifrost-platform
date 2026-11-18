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

// Moving the release records into the database (2026-11-17, with the
// communication layer's step 3: duty/from-agent-tool-comm-2026-11-16-bus-step3-
// ci-checklist.md §6). Current reads the rows (current.go). The recorder still
// writes the ConfigMaps too, and each pass
//   - copies into the table every ConfigMap record whose run has no row (a row
//     that failed, or a record from before the table), without an
//     announcement: nobody should be woken for those;
//   - asks Current of every deploying lane and environment both ways, and
//     counts and logs where the ConfigMaps and the rows differ.

// TableReader reads the record table and copies a record into it.
type TableReader interface {
	// Records is the newest row of each successful run of this tenant.
	Records(ctx context.Context) ([]Record, error)
	// Deployments is the newest row of each successful deploying run of lane
	// and env.
	Deployments(ctx context.Context, lane, env string) ([]Record, error)
	// Backfill writes rec as a row without an announcement, unless its run
	// has a row already; it says whether it wrote one.
	Backfill(ctx context.Context, rec Record) (bool, error)
}

// SetTableReader hands the service its table reader: Current reads the rows
// and the recorder compares them with the ConfigMaps. nil keeps Current on the
// ConfigMaps and compares nothing.
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

const recordColumns = `run, pipeline, lane, env, deploys, revision, tag, from_run,
		started_at, completed_at, repos, images, missing, recorded_at`

// ReadRecords is TableReader.Records over agentrt.release_record.
func ReadRecords(ctx context.Context, q Querier) ([]Record, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (run) `+recordColumns+`
		FROM agentrt.release_record WHERE tenant = $1 AND succeeded
		ORDER BY run, release_record_id DESC`, Tenant)
	if err != nil {
		return nil, err
	}
	return scanRecords(rows)
}

// ReadDeployments is TableReader.Deployments over agentrt.release_record.
func ReadDeployments(ctx context.Context, q Querier, lane, env string) ([]Record, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (run) `+recordColumns+`
		FROM agentrt.release_record WHERE tenant = $1 AND succeeded AND deploys AND lane = $2 AND env = $3
		ORDER BY run, release_record_id DESC`, Tenant, lane, env)
	if err != nil {
		return nil, err
	}
	return scanRecords(rows)
}

func scanRecords(rows pgx.Rows) ([]Record, error) {
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
	for _, pair := range s.rules.deployingPairs() {
		lane, env := pair[0], pair[1]
		fromMaps, mapsErr := s.currentFromMaps(ctx, lane, env)
		fromRows, rowsErr := s.currentFromTable(ctx, r, lane, env)
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
