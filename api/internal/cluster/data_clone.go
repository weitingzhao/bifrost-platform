package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

const (
	dataCloneRemoteDump = "/var/lib/postgresql/data/bifrost-clone-prod.sql"
	dataCloneMinDumpB   = 1_000_000
)

var safeIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ---------------------------------------------------------------------------
// Job store
// ---------------------------------------------------------------------------

type DataCloneVerifyResult struct {
	Database   string `json:"database"`
	TableCount int    `json:"table_count"`
	SampleRows int    `json:"sample_rows"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail,omitempty"`
}

type DataCloneJob struct {
	ID         string                  `json:"id"`
	Action     string                  `json:"action"`
	Status     string                  `json:"status"` // queued|dumping|restoring|verifying|done|failed
	Step       string                  `json:"step"`
	Source     string                  `json:"source"`
	Targets    []string                `json:"targets"`
	Mode       string                  `json:"mode"` // full|selective
	Tables     []string                `json:"tables,omitempty"`
	Progress   float64                 `json:"progress"`
	Detail     string                  `json:"detail"`
	Verify     []DataCloneVerifyResult `json:"verify,omitempty"`
	Actor      string                  `json:"actor,omitempty"`
	Trigger    string                  `json:"trigger"` // manual|schedule
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
}

type DataCloneJobStore struct {
	mu   sync.Mutex
	jobs map[string]*DataCloneJob
	dir  string
}

func NewDataCloneJobStore() *DataCloneJobStore {
	dir := os.Getenv("PLATFORM_DATA_CLONE_JOBS_DIR")
	if dir == "" {
		if data := os.Getenv("PLATFORM_DATA_DIR"); data != "" {
			dir = filepath.Join(data, "data-clone-jobs")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data-clone-jobs")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	s := &DataCloneJobStore{jobs: map[string]*DataCloneJob{}, dir: dir}
	s.loadAll()
	return s
}

func (s *DataCloneJobStore) loadAll() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var job DataCloneJob
		if json.Unmarshal(raw, &job) != nil || job.ID == "" {
			continue
		}
		cp := job
		s.jobs[job.ID] = &cp
	}
}

func (s *DataCloneJobStore) persistLocked(job *DataCloneJob) {
	if s.dir == "" || job == nil {
		return
	}
	raw, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.dir, job.ID+".json"), raw, 0o600)
}

func (s *DataCloneJobStore) Create(job DataCloneJob) DataCloneJob {
	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = now.Format("20060102T150405.000000000Z")
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Action == "" {
		job.Action = "cluster.data.clone"
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.Step == "" {
		job.Step = "queued"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := job
	s.jobs[job.ID] = &cp
	s.persistLocked(&cp)
	return cp
}

func (s *DataCloneJobStore) Update(id string, mut func(*DataCloneJob)) (*DataCloneJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	mut(job)
	job.UpdatedAt = time.Now().UTC()
	s.persistLocked(job)
	cp := *job
	return &cp, true
}

func (s *DataCloneJobStore) Get(id string) (*DataCloneJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *job
	return &cp, true
}

func (s *DataCloneJobStore) List() []DataCloneJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DataCloneJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	// newest first
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

var dataCloneActiveStatuses = map[string]bool{
	"queued":    true,
	"dumping":   true,
	"restoring": true,
	"verifying": true,
}

// FindActive returns the newest job whose status is still in-flight, if any.
func (s *DataCloneJobStore) FindActive() (*DataCloneJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findActiveLocked()
}

func (s *DataCloneJobStore) findActiveLocked() (*DataCloneJob, bool) {
	var best *DataCloneJob
	for _, j := range s.jobs {
		if !dataCloneActiveStatuses[j.Status] {
			continue
		}
		if best == nil || j.CreatedAt.After(best.CreatedAt) {
			cp := *j
			best = &cp
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

// CreateIfNoActive creates a job only when no in-flight clone exists.
func (s *DataCloneJobStore) CreateIfNoActive(job DataCloneJob) (DataCloneJob, *ErrCloneInProgress) {
	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = now.Format("20060102T150405.000000000Z")
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Action == "" {
		job.Action = "cluster.data.clone"
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.Step == "" {
		job.Step = "queued"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if active, ok := s.findActiveLocked(); ok {
		return DataCloneJob{}, &ErrCloneInProgress{ExistingJobID: active.ID, Status: active.Status}
	}
	cp := job
	s.jobs[job.ID] = &cp
	s.persistLocked(&cp)
	return cp, nil
}

// ErrCloneInProgress is returned when startDataClone refuses a concurrent job.
type ErrCloneInProgress struct {
	ExistingJobID string
	Status        string
}

func (e *ErrCloneInProgress) Error() string {
	return fmt.Sprintf("data clone already in progress: job=%s status=%s", e.ExistingJobID, e.Status)
}

// ---------------------------------------------------------------------------
// Schedule store
// ---------------------------------------------------------------------------

type DataCloneSchedule struct {
	Enabled       bool       `json:"enabled"`
	Interval      string     `json:"interval"` // disabled|daily|weekly
	Source        string     `json:"source"`
	Targets       []string   `json:"targets"`
	Mode          string     `json:"mode"`
	Tables        []string   `json:"tables,omitempty"`
	LastAutoRunAt *time.Time `json:"last_auto_run_at,omitempty"`
	LastAutoRunID string     `json:"last_auto_run_id,omitempty"`
	LastStatus    string     `json:"last_status,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type DataCloneScheduleStore struct {
	mu   sync.Mutex
	path string
	cfg  DataCloneSchedule
}

func NewDataCloneScheduleStore() *DataCloneScheduleStore {
	path := os.Getenv("PLATFORM_DATA_CLONE_SCHEDULE")
	if path == "" {
		if data := os.Getenv("PLATFORM_DATA_DIR"); data != "" {
			path = filepath.Join(data, "data-clone-schedule.json")
		} else {
			path = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data-clone-schedule.json")
		}
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	s := &DataCloneScheduleStore{path: path}
	s.cfg = defaultDataCloneSchedule()
	s.load()
	return s
}

func defaultDataCloneSchedule() DataCloneSchedule {
	return DataCloneSchedule{
		Enabled:   false,
		Interval:  "disabled",
		Source:    "bifrost_prod",
		Targets:   []string{"bifrost_dev", "bifrost_stg"},
		Mode:      "full",
		UpdatedAt: time.Now().UTC(),
	}
}

func (s *DataCloneScheduleStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var cfg DataCloneSchedule
	if json.Unmarshal(raw, &cfg) != nil {
		return
	}
	s.cfg = cfg
}

func (s *DataCloneScheduleStore) persistLocked() {
	raw, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o600)
}

func (s *DataCloneScheduleStore) Get() DataCloneSchedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *DataCloneScheduleStore) Put(cfg DataCloneSchedule) DataCloneSchedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Source == "" {
		cfg.Source = "bifrost_prod"
	}
	if len(cfg.Targets) == 0 {
		cfg.Targets = []string{"bifrost_dev", "bifrost_stg"}
	}
	if cfg.Mode == "" {
		cfg.Mode = "full"
	}
	if cfg.Interval == "" {
		cfg.Interval = "disabled"
	}
	if cfg.Interval == "disabled" {
		cfg.Enabled = false
	}
	cfg.UpdatedAt = time.Now().UTC()
	s.cfg = cfg
	s.persistLocked()
	return s.cfg
}

func (s *DataCloneScheduleStore) RecordRun(jobID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cfg.LastAutoRunAt = &now
	s.cfg.LastAutoRunID = jobID
	s.cfg.LastStatus = status
	s.cfg.UpdatedAt = now
	s.persistLocked()
}

// ---------------------------------------------------------------------------
// Last successful clone (for freshness Last clone display)
// ---------------------------------------------------------------------------

type DataCloneLastMeta struct {
	LastCloneAt    *time.Time `json:"last_clone_at,omitempty"`
	LastCloneJobID string     `json:"last_clone_job_id,omitempty"`
	Targets        []string   `json:"targets,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type DataCloneLastStore struct {
	mu   sync.Mutex
	path string
	meta DataCloneLastMeta
}

func NewDataCloneLastStore() *DataCloneLastStore {
	path := os.Getenv("PLATFORM_DATA_CLONE_LAST")
	if path == "" {
		if data := os.Getenv("PLATFORM_DATA_DIR"); data != "" {
			path = filepath.Join(data, "data-clone-last.json")
		} else {
			path = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data-clone-last.json")
		}
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	s := &DataCloneLastStore{path: path}
	s.load()
	return s
}

func (s *DataCloneLastStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var meta DataCloneLastMeta
	if json.Unmarshal(raw, &meta) != nil {
		return
	}
	s.meta = meta
}

func (s *DataCloneLastStore) persistLocked() {
	raw, err := json.MarshalIndent(s.meta, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o600)
}

func (s *DataCloneLastStore) Get() DataCloneLastMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta
}

func (s *DataCloneLastStore) Record(jobID string, targets []string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at = at.UTC()
	s.meta.LastCloneAt = &at
	s.meta.LastCloneJobID = jobID
	s.meta.Targets = append([]string{}, targets...)
	s.meta.UpdatedAt = at
	s.persistLocked()
}

// ---------------------------------------------------------------------------
// Request / response
// ---------------------------------------------------------------------------

type DataCloneRequest struct {
	Source            string   `json:"source"`
	Targets           []string `json:"targets"`
	Mode              string   `json:"mode"`
	Tables            []string `json:"tables"`
	ConfirmationToken string   `json:"confirmation_token"`
	// Confirm must be true (explicit JSON boolean) in addition to confirmation_token.
	Confirm bool `json:"confirm"`
}

type DataCloneScheduleRequest struct {
	Enabled  *bool    `json:"enabled"`
	Interval string   `json:"interval"`
	Source   string   `json:"source"`
	Targets  []string `json:"targets"`
	Mode     string   `json:"mode"`
	Tables   []string `json:"tables"`
}

// ---------------------------------------------------------------------------
// Service wiring
// ---------------------------------------------------------------------------

func (s *Service) ensureCloneStores() {
	if s.cloneJobs == nil {
		s.cloneJobs = NewDataCloneJobStore()
	}
	if s.cloneSched == nil {
		s.cloneSched = NewDataCloneScheduleStore()
	}
	if s.cloneLast == nil {
		s.cloneLast = NewDataCloneLastStore()
	}
}

func (s *Service) StartDataCloneScheduler(ctx context.Context) {
	s.ensureCloneStores()
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		s.maybeAutoClone(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.maybeAutoClone(ctx)
			}
		}
	}()
}

func (s *Service) maybeAutoClone(ctx context.Context) {
	s.ensureCloneStores()
	cfg := s.cloneSched.Get()
	if !cfg.Enabled || cfg.Interval == "disabled" {
		return
	}
	if cfg.LastAutoRunAt != nil {
		elapsed := time.Since(*cfg.LastAutoRunAt)
		switch cfg.Interval {
		case "daily":
			if elapsed < 23*time.Hour {
				return
			}
		case "weekly":
			if elapsed < 6*24*time.Hour {
				return
			}
		default:
			return
		}
	}
	job, err := s.startDataClone(ctx, DataCloneRequest{
		Source:            cfg.Source,
		Targets:           cfg.Targets,
		Mode:              cfg.Mode,
		Tables:            cfg.Tables,
		ConfirmationToken: dataCloneConfirmTok,
		Confirm:           true,
	}, "schedule", "scheduler")
	if err != nil {
		s.cloneSched.RecordRun("", "failed:"+err.Error())
		return
	}
	s.cloneSched.RecordRun(job.ID, "started")
}

func (s *Service) startDataClone(ctx context.Context, req DataCloneRequest, trigger, actor string) (*DataCloneJob, error) {
	s.ensureCloneStores()
	normalizeCloneRequest(&req)
	if err := validateDataCloneRequest(req); err != nil {
		return nil, err
	}
	job, busy := s.cloneJobs.CreateIfNoActive(DataCloneJob{
		Status:  "queued",
		Step:    "queued",
		Source:  req.Source,
		Targets: req.Targets,
		Mode:    req.Mode,
		Tables:  req.Tables,
		Trigger: trigger,
		Actor:   actor,
		Detail:  "queued",
	})
	if busy != nil {
		return nil, busy
	}
	go s.runDataClone(context.Background(), job.ID)
	_ = ctx
	return &job, nil
}

func validateDataCloneRequest(req DataCloneRequest) error {
	if !req.Confirm {
		return fmt.Errorf("confirm must be true")
	}
	if strings.TrimSpace(req.ConfirmationToken) != dataCloneConfirmTok {
		return fmt.Errorf("confirmation_token must be %q", dataCloneConfirmTok)
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "bifrost_prod"
	}
	if source != "bifrost_prod" {
		return fmt.Errorf("source must be bifrost_prod (refusing non-prod source)")
	}
	if len(req.Targets) == 0 {
		return fmt.Errorf("targets required")
	}
	for _, t := range req.Targets {
		if t != "bifrost_dev" && t != "bifrost_stg" {
			return fmt.Errorf("target %q not allowed (only bifrost_dev / bifrost_stg)", t)
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "full"
	}
	if mode != "full" && mode != "selective" {
		return fmt.Errorf("mode must be full or selective")
	}
	if mode == "selective" {
		if len(req.Tables) == 0 {
			return fmt.Errorf("selective mode requires tables[]")
		}
		for _, table := range req.Tables {
			if !safeIdentRe.MatchString(table) {
				return fmt.Errorf("invalid table name %q", table)
			}
		}
	}
	return nil
}

func normalizeCloneRequest(req *DataCloneRequest) {
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "bifrost_prod"
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = "full"
	}
	if len(req.Targets) == 0 {
		req.Targets = []string{"bifrost_dev", "bifrost_stg"}
	}
}

// ---------------------------------------------------------------------------
// Executor (aligned with clone-cnpg-prod-to-dev-stg.sh)
// ---------------------------------------------------------------------------

func (s *Service) runDataClone(ctx context.Context, jobID string) {
	s.ensureCloneStores()
	update := func(status, step, detail string, progress float64) {
		_, _ = s.cloneJobs.Update(jobID, func(j *DataCloneJob) {
			j.Status = status
			j.Step = step
			j.Detail = detail
			j.Progress = progress
		})
	}
	fail := func(detail string) {
		now := time.Now().UTC()
		_, _ = s.cloneJobs.Update(jobID, func(j *DataCloneJob) {
			j.Status = "failed"
			j.Step = "failed"
			j.Detail = detail
			j.FinishedAt = &now
		})
	}

	job, ok := s.cloneJobs.Get(jobID)
	if !ok {
		return
	}

	primary, err := s.resolveCNPGPrimary(ctx)
	if err != nil {
		fail(err.Error())
		return
	}

	// Always remove remote dump on exit (done or failed) once primary is known.
	defer func() {
		_, _ = s.execOnPrimary(ctx, primary, "rm", "-f", dataCloneRemoteDump)
	}()
	// Clear an orphaned dump before starting; the deferred cleanup covers this run.
	if _, err := s.execOnPrimary(ctx, primary, "rm", "-f", dataCloneRemoteDump); err != nil {
		fail("dump preflight cleanup failed: " + err.Error())
		return
	}

	update("dumping", "dumping", fmt.Sprintf("pg_dump %s on %s", job.Source, primary), 0.1)

	if job.Mode == "selective" {
		if err := s.validateSelectiveSourceTables(ctx, primary, job.Source, job.Tables); err != nil {
			fail(err.Error())
			return
		}
	}
	dumpArgs := dataCloneDumpArgs(job.Source, job.Mode, job.Tables)
	if _, err := s.execOnPrimary(ctx, primary, dumpArgs...); err != nil {
		fail("dump failed: " + err.Error())
		return
	}

	sizeOut, err := s.execOnPrimary(ctx, primary, "sh", "-c", fmt.Sprintf("wc -c < '%s'", dataCloneRemoteDump))
	if err != nil {
		fail("dump size check failed: " + err.Error())
		return
	}
	sizeOut = strings.TrimSpace(sizeOut)
	var dumpSize int
	fmt.Sscanf(sizeOut, "%d", &dumpSize)
	if job.Mode == "full" && dumpSize < dataCloneMinDumpB {
		fail(fmt.Sprintf("dump suspiciously small: %d bytes", dumpSize))
		return
	}
	update("dumping", "dumping", fmt.Sprintf("dump size %d bytes", dumpSize), 0.35)

	for i, target := range job.Targets {
		progress := 0.35 + 0.45*float64(i)/float64(len(job.Targets))
		update("restoring", "restoring", fmt.Sprintf("restoring %s", target), progress)
		if err := s.restoreTarget(ctx, primary, target, job.Mode, job.Tables); err != nil {
			fail(fmt.Sprintf("restore %s: %v", target, err))
			return
		}
	}

	update("verifying", "verifying", "post-clone verify", 0.85)
	results := make([]DataCloneVerifyResult, 0, len(job.Targets))
	for _, target := range job.Targets {
		vr := s.verifyTarget(ctx, primary, target, job.Source)
		results = append(results, vr)
		if !vr.OK {
			fail(fmt.Sprintf("verify %s failed: %s", target, vr.Detail))
			return
		}
	}

	now := time.Now().UTC()
	var targets []string
	_, _ = s.cloneJobs.Update(jobID, func(j *DataCloneJob) {
		j.Status = "done"
		j.Step = "done"
		j.Detail = "clone complete"
		j.Progress = 1
		j.Verify = results
		j.FinishedAt = &now
		targets = append([]string{}, j.Targets...)
	})
	s.ensureCloneStores()
	s.cloneLast.Record(jobID, targets, now)
	sharedFreshnessCache.mu.Lock()
	sharedFreshnessCache.at = time.Time{} // invalidate freshness cache
	sharedFreshnessCache.mu.Unlock()
}

// dataCloneDumpArgs returns a schema-inclusive dump for full clone, and a data-only
// dump for selective clone. Selective restore assumes destination tables already exist.
func dataCloneDumpArgs(source, mode string, tables []string) []string {
	args := []string{"pg_dump", "-U", "postgres", "-d", source, "--no-owner", "--no-acl", "--format=plain"}
	if mode == "selective" {
		args = append(args, "--data-only")
		for _, table := range tables {
			args = append(args, "-t", table)
		}
	}
	return append(args, "-f", dataCloneRemoteDump)
}

func (s *Service) validateSelectiveSourceTables(ctx context.Context, primary, source string, tables []string) error {
	for _, table := range tables {
		if !safeIdentRe.MatchString(table) {
			return fmt.Errorf("invalid table %q", table)
		}
		out, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", source, "-tAc",
			fmt.Sprintf("SELECT to_regclass('public.%s') IS NOT NULL", table))
		if err != nil {
			return fmt.Errorf("selective source table check %q failed: %w", table, err)
		}
		if strings.TrimSpace(out) != "t" {
			return fmt.Errorf("selective source table %q does not exist in %s", table, source)
		}
	}
	return nil
}

func (s *Service) restoreTarget(ctx context.Context, primary, target, mode string, tables []string) error {
	if mode == "selective" {
		for _, table := range tables {
			if !safeIdentRe.MatchString(table) {
				return fmt.Errorf("invalid table %q", table)
			}
			_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1", "-c",
				fmt.Sprintf("TRUNCATE TABLE %s CASCADE;", table))
			if err != nil {
				return fmt.Errorf("truncate selective table %q in %s: %w", table, target, err)
			}
		}
		_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1", "-f", dataCloneRemoteDump)
		if err != nil {
			return err
		}
	} else {
		// Full clone dumps may include non-public schemas (market / data_ops / …).
		// Dropping only public leaves those schemas and CREATE SCHEMA in the dump fails.
		_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1", "-c",
			dataCloneFullResetSQL)
		if err != nil {
			return err
		}
		_, err = s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1", "-f", dataCloneRemoteDump)
		if err != nil {
			return err
		}
	}
	_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-c", dataCloneGrantBifrostSQL)
	return err
}

// dataCloneFullResetSQL drops every user schema so a multi-schema pg_dump restore is clean.
const dataCloneFullResetSQL = `
DO $$
DECLARE r RECORD;
BEGIN
  FOR r IN
    SELECT nspname FROM pg_namespace
    WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND nspname NOT LIKE 'pg_temp_%'
      AND nspname NOT LIKE 'pg_toast_temp_%'
  LOOP
    EXECUTE format('DROP SCHEMA IF EXISTS %I CASCADE', r.nspname);
  END LOOP;
END $$;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO bifrost;
GRANT ALL ON SCHEMA public TO public;
`

// dataCloneGrantBifrostSQL grants bifrost on all user schemas after restore (--no-acl dumps).
const dataCloneGrantBifrostSQL = `
DO $$
DECLARE r RECORD;
BEGIN
  FOR r IN
    SELECT nspname FROM pg_namespace
    WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND nspname NOT LIKE 'pg_temp_%'
      AND nspname NOT LIKE 'pg_toast_temp_%'
  LOOP
    EXECUTE format('GRANT ALL ON SCHEMA %I TO bifrost', r.nspname);
    EXECUTE format('GRANT ALL ON ALL TABLES IN SCHEMA %I TO bifrost', r.nspname);
    EXECUTE format('GRANT ALL ON ALL SEQUENCES IN SCHEMA %I TO bifrost', r.nspname);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT ALL ON TABLES TO bifrost', r.nspname);
  END LOOP;
END $$;
`

func (s *Service) verifyTarget(ctx context.Context, primary, target, source string) DataCloneVerifyResult {
	vr := DataCloneVerifyResult{Database: target}
	countOut, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-tAc",
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
	if err != nil {
		vr.Detail = err.Error()
		return vr
	}
	fmt.Sscanf(strings.TrimSpace(countOut), "%d", &vr.TableCount)
	if vr.TableCount <= 0 {
		vr.Detail = "no public tables"
		return vr
	}

	sampleOut, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-tAc",
		"SELECT count(*) FROM daemon_control")
	if err == nil {
		fmt.Sscanf(strings.TrimSpace(sampleOut), "%d", &vr.SampleRows)
	}

	srcCountOut, _ := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", source, "-tAc",
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
	var srcCount int
	fmt.Sscanf(strings.TrimSpace(srcCountOut), "%d", &srcCount)
	if srcCount > 0 && vr.TableCount < srcCount/2 {
		vr.Detail = fmt.Sprintf("table count %d << source %d", vr.TableCount, srcCount)
		return vr
	}
	vr.OK = true
	vr.Detail = fmt.Sprintf("%d tables · daemon_control rows=%d", vr.TableCount, vr.SampleRows)
	return vr
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func (h *Handler) HandleDataFreshness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.DataFreshness(r.Context()))
}

func (h *Handler) HandleDataClone(w http.ResponseWriter, r *http.Request) {
	var req DataCloneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	normalizeCloneRequest(&req)
	if err := validateDataCloneRequest(req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	actor := actuation.PrincipalFromContext(r.Context()).Name
	job, err := h.svc.startDataClone(r.Context(), req, "manual", actor)
	if err != nil {
		h.recordAudit(r, "cluster.data.clone", strings.Join(req.Targets, ","), "failed", err.Error())
		if busy, ok := err.(*ErrCloneInProgress); ok {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":           busy.Error(),
				"existing_job_id": busy.ExistingJobID,
				"status":          busy.Status,
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.recordAudit(r, "cluster.data.clone", strings.Join(req.Targets, ","), "ok",
		fmt.Sprintf("job=%s mode=%s", job.ID, job.Mode))
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) HandleDataCloneStatus(w http.ResponseWriter, r *http.Request) {
	h.svc.ensureCloneStores()
	id := chi.URLParam(r, "id")
	job, ok := h.svc.cloneJobs.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) HandleDataCloneList(w http.ResponseWriter, r *http.Request) {
	h.svc.ensureCloneStores()
	writeJSON(w, http.StatusOK, map[string]any{"jobs": h.svc.cloneJobs.List()})
}

func (h *Handler) HandleDataCloneScheduleGet(w http.ResponseWriter, r *http.Request) {
	h.svc.ensureCloneStores()
	writeJSON(w, http.StatusOK, h.svc.cloneSched.Get())
}

func (h *Handler) HandleDataCloneSchedulePut(w http.ResponseWriter, r *http.Request) {
	h.svc.ensureCloneStores()
	var req DataCloneScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	cur := h.svc.cloneSched.Get()
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.Interval != "" {
		cur.Interval = req.Interval
	}
	if req.Source != "" {
		cur.Source = req.Source
	}
	if len(req.Targets) > 0 {
		cur.Targets = req.Targets
	}
	if req.Mode != "" {
		cur.Mode = req.Mode
	}
	if req.Tables != nil {
		cur.Tables = req.Tables
	}
	if cur.Source != "bifrost_prod" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source must be bifrost_prod"})
		return
	}
	for _, t := range cur.Targets {
		if t != "bifrost_dev" && t != "bifrost_stg" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid target " + t})
			return
		}
	}
	out := h.svc.cloneSched.Put(cur)
	h.recordAudit(r, "cluster.data.clone.schedule", cur.Interval, "ok",
		fmt.Sprintf("enabled=%v interval=%s", out.Enabled, out.Interval))
	writeJSON(w, http.StatusOK, out)
}
