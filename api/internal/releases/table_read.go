package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// The record table is where the records are read from (2026-11-18: all 1,338
// ConfigMap records matched their rows field by field, and the recorder's
// passes had compared 0 differences). Current and List read the rows, and a
// recorder pass asks the table which runs it has. The first pass of a process
// copies in any ConfigMap record whose run has no row, without an announcement:
// nobody should be woken for those.

// TableReader reads the record table and copies a record into it.
type TableReader interface {
	// Records is the newest row of each successful run of this tenant.
	Records(ctx context.Context) ([]Record, error)
	// Deployments is the newest row of each successful deploying run of lane
	// and env.
	Deployments(ctx context.Context, lane, env string) ([]Record, error)
	// MissingByRun is, for those of runs with a successful row, how many
	// sources the newest row could not read.
	MissingByRun(ctx context.Context, runs []string) (map[string]int, error)
	// Backfill writes rec as a row without an announcement, unless its run
	// has a row already; it says whether it wrote one.
	Backfill(ctx context.Context, rec Record) (bool, error)
}

// SetTableReader hands the service its table reader: the records are read from
// and kept in the rows. nil keeps them in the ConfigMaps.
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
		started_at, completed_at, repos, images, missing, recorded_at, build_node, build_node_why`

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

// ReadMissingByRun is TableReader.MissingByRun over agentrt.release_record.
func ReadMissingByRun(ctx context.Context, q Querier, runs []string) (map[string]int, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (run) run, jsonb_array_length(missing)
		FROM agentrt.release_record WHERE tenant = $1 AND succeeded AND run = ANY($2)
		ORDER BY run, release_record_id DESC`, Tenant, runs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int, len(runs))
	for rows.Next() {
		var run string
		var missing int
		if err := rows.Scan(&run, &missing); err != nil {
			return nil, err
		}
		out[run] = missing
	}
	return out, rows.Err()
}

func scanRecords(rows pgx.Rows) ([]Record, error) {
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var rec Record
		var repos, images, missing []byte
		if err := rows.Scan(&rec.Run, &rec.Pipeline, &rec.Lane, &rec.Env, &rec.Deploys, &rec.Revision, &rec.Tag, &rec.FromRun,
			&rec.StartedAt, &rec.CompletedAt, &repos, &images, &missing, &rec.RecordedAt, &rec.BuildNode, &rec.BuildNodeWhy); err != nil {
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

// copyMaps writes a row for every ConfigMap record whose run has none, and says
// how many it wrote.
func (s *Service) copyMaps(ctx context.Context, core kubernetes.Interface, r TableReader) (int, error) {
	cms, err := core.CoreV1().ConfigMaps(s.rules.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labelRecord})
	if err != nil {
		return 0, fmt.Errorf("list records: %w", err)
	}
	rows, err := r.Records(ctx)
	if err != nil {
		return 0, fmt.Errorf("read rows: %w", err)
	}
	have := make(map[string]bool, len(rows))
	for _, row := range rows {
		have[row.Run] = true
	}
	copied := 0
	for _, cm := range cms.Items {
		var rec Record
		if err := json.Unmarshal([]byte(cm.Data[dataKey]), &rec); err != nil || rec.Run == "" || have[rec.Run] {
			continue
		}
		wrote, err := r.Backfill(ctx, rec)
		if err != nil {
			return copied, fmt.Errorf("backfill %s: %w", rec.Run, err)
		}
		if wrote {
			copied++
		}
	}
	return copied, nil
}

// tickRows is a recorder pass over the table: each finished, successful,
// matching run with no row gets one, and a row that could not read a source is
// written again when the run now gives more.
func (s *Service) tickRows(ctx context.Context, r TableReader, core kubernetes.Interface, dyn dynamic.Interface, res *TickResult) {
	ns := s.rules.Namespace
	s.mu.Lock()
	copied := s.copiedMaps
	s.mu.Unlock()
	if !copied {
		n, err := s.copyMaps(ctx, core, r)
		res.Backfilled = n
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			return
		}
		s.mu.Lock()
		s.copiedMaps = true
		s.mu.Unlock()
	}
	runs, err := s.readRuns(ctx, dyn, RouteTick, func(Rule) bool { return true })
	if err != nil {
		res.Errors = append(res.Errors, "list runs: "+err.Error())
		return
	}
	res.Runs = len(runs)
	type candidate struct {
		name string
		rule Rule
	}
	var todo []candidate
	var names []string
	for i := range runs {
		rule, ok := s.rules.Match(runs[i].GetName(), runs[i].GetLabels()["tekton.dev/pipeline"])
		if !ok {
			continue
		}
		res.Matched++
		if succeeded(&runs[i]) {
			todo = append(todo, candidate{runs[i].GetName(), rule})
			names = append(names, runs[i].GetName())
		}
	}
	missing, err := r.MissingByRun(ctx, names)
	if err != nil {
		res.Errors = append(res.Errors, "read rows: "+err.Error())
		return
	}
	writer := s.tableWriter()
	if RecorderQuiet() {
		writer = nil
	}
	for _, c := range todo {
		had, found := missing[c.name]
		if found && (had == 0 || writer == nil) {
			continue
		}
		// The list says which runs to record; the record is built from the run
		// itself (the bus run cache keeps only part of one).
		full, err := dyn.Resource(pipelineRunGVR).Namespace(ns).Get(ctx, c.name, metav1.GetOptions{})
		if err != nil {
			res.Errors = append(res.Errors, c.name+": run: "+err.Error())
			continue
		}
		trs, err := dyn.Resource(taskRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + c.name})
		if err != nil {
			res.Errors = append(res.Errors, c.name+": taskruns: "+err.Error())
			continue
		}
		rec := buildRecord(full, c.rule, trs.Items, s.now())
		if found && len(rec.Missing) >= had {
			continue
		}
		if writer != nil {
			err = writer(ctx, rec, true)
		} else {
			_, err = r.Backfill(ctx, rec)
		}
		if err != nil {
			slog.Warn("release record: the row was not written", "run", rec.Run, "err", err)
			res.Errors = append(res.Errors, c.name+": row: "+err.Error())
			continue
		}
		res.Recorded++
		res.Rows++
	}
}
