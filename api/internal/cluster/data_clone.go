package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

const (
	dataCloneRemoteDump = "/var/lib/postgresql/data/bifrost-clone-prod.sql"
	// Trade OLTP DBs are slim after Wave hygiene (~100–200 KiB dumps are complete).
	// 1 MiB previously rejected valid full dumps and left Dev stale.
	dataCloneMinDumpB = 50_000
)

var safeIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ---------------------------------------------------------------------------
// Job store
// ---------------------------------------------------------------------------

type DataCloneVerifyResult struct {
	Database   string `json:"database"`
	TableCount int    `json:"table_count"`
	// SampleLabel / SampleRows are the target application's own sample count after the
	// clone (its data probe). SampleRows is null when the probe could not be read; that is
	// reported, never a verify failure.
	SampleLabel string `json:"sample_label,omitempty"`
	SampleRows  *int   `json:"sample_rows"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail,omitempty"`
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
	path string
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
	return &DataCloneScheduleStore{path: path}
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

func decodeSchedule(raw []byte) DataCloneSchedule {
	cfg := defaultDataCloneSchedule()
	if len(raw) == 0 {
		return cfg
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return defaultDataCloneSchedule()
	}
	return cfg
}

func encodeSchedule(cfg DataCloneSchedule) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "  ")
}

// Get reads the schedule from the state backend on every call.
func (s *DataCloneScheduleStore) Get() DataCloneSchedule {
	raw, err := statefile.ReadFile(s.path)
	if err != nil {
		return defaultDataCloneSchedule()
	}
	return decodeSchedule(raw)
}

func normalizeSchedule(cfg DataCloneSchedule) DataCloneSchedule {
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
	return cfg
}

// Put writes the schedule fields onto the latest document. A concurrent
// RecordRun's last-run fields stay, because the write goes through
// statefile.Update instead of replacing a cached copy.
func (s *DataCloneScheduleStore) Put(cfg DataCloneSchedule) DataCloneSchedule {
	cfg = normalizeSchedule(cfg)
	saved := cfg
	err := statefile.Update(s.path, func(old []byte) ([]byte, error) {
		base := decodeSchedule(old)
		base.Enabled = cfg.Enabled
		base.Interval = cfg.Interval
		base.Source = cfg.Source
		base.Targets = cfg.Targets
		base.Mode = cfg.Mode
		base.Tables = cfg.Tables
		base.UpdatedAt = cfg.UpdatedAt
		saved = base
		return encodeSchedule(base)
	})
	if err != nil {
		return cfg
	}
	return saved
}

func (s *DataCloneScheduleStore) RecordRun(jobID, status string) {
	_ = statefile.Update(s.path, func(old []byte) ([]byte, error) {
		cfg := decodeSchedule(old)
		now := time.Now().UTC()
		cfg.LastAutoRunAt = &now
		cfg.LastAutoRunID = jobID
		cfg.LastStatus = status
		cfg.UpdatedAt = now
		return encodeSchedule(cfg)
	})
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
	path string
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
	return &DataCloneLastStore{path: path}
}

func decodeLast(raw []byte) DataCloneLastMeta {
	if len(raw) == 0 {
		return DataCloneLastMeta{}
	}
	var meta DataCloneLastMeta
	if json.Unmarshal(raw, &meta) != nil {
		return DataCloneLastMeta{}
	}
	return meta
}

func (s *DataCloneLastStore) Get() DataCloneLastMeta {
	raw, err := statefile.ReadFile(s.path)
	if err != nil {
		return DataCloneLastMeta{}
	}
	return decodeLast(raw)
}

func (s *DataCloneLastStore) Record(jobID string, targets []string, at time.Time) {
	at = at.UTC()
	_ = statefile.Update(s.path, func(old []byte) ([]byte, error) {
		meta := decodeLast(old)
		meta.LastCloneAt = &at
		meta.LastCloneJobID = jobID
		meta.Targets = append([]string{}, targets...)
		meta.UpdatedAt = at
		return json.MarshalIndent(meta, "", "  ")
	})
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
	// Default on. The STG overlay sets PLATFORM_DATA_CLONE_SCHEDULER=off:
	// STG observes, and this loop's default source is bifrost_prod.
	if !config.LoopEnabled(config.EnvDataCloneScheduler) {
		slog.Info("data-clone scheduler not started", "env", config.EnvDataCloneScheduler)
		return
	}
	s.ensureCloneStores()
	safego.Go("cluster.dataCloneScheduler", func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		safego.Do("cluster.maybeAutoClone", func() { s.maybeAutoClone(ctx) })
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				safego.Do("cluster.maybeAutoClone", func() { s.maybeAutoClone(ctx) })
			}
		}
	})
}

func (s *Service) maybeAutoClone(ctx context.Context) {
	id := maintainer.PlatformID(maintainer.LoopDataClone)
	s.ensureCloneStores()
	cfg := s.cloneSched.Get()
	if !cfg.Enabled || cfg.Interval == "disabled" {
		maintainer.Success(id)
		return
	}
	if cfg.LastAutoRunAt != nil {
		elapsed := time.Since(*cfg.LastAutoRunAt)
		switch cfg.Interval {
		case "daily":
			if elapsed < 23*time.Hour {
				maintainer.Success(id)
				return
			}
		case "weekly":
			if elapsed < 6*24*time.Hour {
				maintainer.Success(id)
				return
			}
		default:
			maintainer.Success(id)
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
		maintainer.Failure(id)
		return
	}
	s.cloneSched.RecordRun(job.ID, "started")
	maintainer.Success(id)
}

func (s *Service) startDataClone(ctx context.Context, req DataCloneRequest, trigger, actor string) (*DataCloneJob, error) {
	s.ensureCloneStores()
	normalizeCloneRequest(&req)
	if err := validateDataCloneRequest(req); err != nil {
		return nil, err
	}
	if req.Mode == "selective" {
		primary, err := s.resolveCNPGPrimary(ctx)
		if err != nil {
			return nil, err
		}
		if err := s.checkSelectiveFKClosure(ctx, primary, req.Targets, req.Tables); err != nil {
			return nil, err
		}
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
	safego.Go("cluster.runDataClone", func() { s.runDataClone(context.Background(), job.ID) })
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
// One data-only pg_dump reads all tables from one snapshot and orders their data by
// foreign key (referenced tables first), so the restore loads parents before children.
func dataCloneDumpArgs(source, mode string, tables []string) []string {
	args := []string{"pg_dump", "-U", "postgres", "-d", source, "--no-owner", "--no-acl", "--format=plain"}
	if mode == "selective" {
		args = append(args, "--data-only")
		for _, table := range tables {
			args = append(args, "-t", dataCloneQualify(table))
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
		qualified := make([]string, 0, len(tables))
		for _, table := range tables {
			if !safeIdentRe.MatchString(table) {
				return fmt.Errorf("invalid table %q", table)
			}
			qualified = append(qualified, dataCloneQualify(table))
		}
		// One TRUNCATE without CASCADE and the restore in one transaction: a table that
		// references a selected table and is not selected fails the TRUNCATE instead of being
		// emptied (startDataClone refuses such a selection up front), and any failure leaves
		// the target as it was.
		_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1",
			"--single-transaction",
			"-c", fmt.Sprintf("TRUNCATE TABLE %s;", strings.Join(qualified, ", ")),
			"-f", dataCloneRemoteDump)
		if err != nil {
			return fmt.Errorf("truncate and restore selective tables in %s (rolled back): %w", target, err)
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
	if _, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-v", "ON_ERROR_STOP=1", "-c",
		dataCloneOwnerSQL); err != nil {
		return fmt.Errorf("reassign public ownership in %s: %w", target, err)
	}
	_, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-c", dataCloneGrantBifrostSQL)
	return err
}

// dataCloneOwnerSQL hands every public relation and enum/domain type that the restore left
// owned by postgres to the database owner (CNPG Database.spec.owner). pg_dump --no-owner
// restored by psql -U postgres makes postgres the owner of everything, and GRANT ALL does
// not confer ownership: the app's schema DDL on connect (ALTER TABLE … IF NOT EXISTS,
// CREATE INDEX IF NOT EXISTS) then fails with "must be owner of table". Indexes and
// OWNED BY sequences follow their table. Mirrors bifrost-trade-infra
// scripts/k3s/fix-cnpg-db-ownership.sh, which the shell clone path runs.
const dataCloneOwnerSQL = `
DO $$
DECLARE
  db_owner text;
  r RECORD;
BEGIN
  SELECT pg_get_userbyid(datdba) INTO db_owner FROM pg_database WHERE datname = current_database();
  FOR r IN
    SELECT c.relname, c.relkind
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relkind IN ('r','p','v','m','f')
      AND pg_get_userbyid(c.relowner) = 'postgres'
  LOOP
    EXECUTE format('ALTER %s public.%I OWNER TO %I',
      CASE r.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'f' THEN 'FOREIGN TABLE' ELSE 'TABLE' END,
      r.relname, db_owner);
  END LOOP;
  FOR r IN
    SELECT t.typname, t.typtype
    FROM pg_type t
    JOIN pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname = 'public'
      AND t.typtype IN ('e','d')
      AND pg_get_userbyid(t.typowner) = 'postgres'
  LOOP
    EXECUTE format('ALTER %s public.%I OWNER TO %I',
      CASE r.typtype WHEN 'd' THEN 'DOMAIN' ELSE 'TYPE' END,
      r.typname, db_owner);
  END LOOP;
END $$;
`

// dataCloneOwnerLeftoverSQL counts the public objects dataCloneOwnerSQL should have moved
// but postgres still owns (zero when postgres is itself the database owner).
const dataCloneOwnerLeftoverSQL = `
SELECT
  (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relkind IN ('r','p','v','m','f')
      AND pg_get_userbyid(c.relowner) = 'postgres'
      AND c.relowner <> (SELECT datdba FROM pg_database WHERE datname = current_database()))
+ (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname = 'public' AND t.typtype IN ('e','d')
      AND pg_get_userbyid(t.typowner) = 'postgres'
      AND t.typowner <> (SELECT datdba FROM pg_database WHERE datname = current_database()))
`

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

	srcCountOut, _ := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", source, "-tAc",
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
	var srcCount int
	fmt.Sscanf(strings.TrimSpace(srcCountOut), "%d", &srcCount)
	if srcCount > 0 && vr.TableCount < srcCount/2 {
		vr.Detail = fmt.Sprintf("table count %d << source %d", vr.TableCount, srcCount)
		return vr
	}

	leftoverOut, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-tAc",
		dataCloneOwnerLeftoverSQL)
	if err != nil {
		vr.Detail = "ownership check: " + err.Error()
		return vr
	}
	var leftover int
	if _, err = fmt.Sscanf(strings.TrimSpace(leftoverOut), "%d", &leftover); err != nil {
		vr.Detail = fmt.Sprintf("ownership check: unreadable count %q", strings.TrimSpace(leftoverOut))
		return vr
	}
	if leftover > 0 {
		vr.Detail = fmt.Sprintf("%d public objects still owned by postgres, not the database owner", leftover)
		return vr
	}
	vr.OK = true
	vr.Detail = fmt.Sprintf("%d tables · %s", vr.TableCount, s.verifySample(ctx, target, &vr))
	return vr
}

// verifySample reads the target application's sample count from its data probe. The platform
// does not count any application table itself; an unreadable probe leaves the sample unknown
// and does not fail the clone.
func (s *Service) verifySample(ctx context.Context, target string, vr *DataCloneVerifyResult) string {
	env, ok := dataProbeDatabases[target]
	if !ok {
		return "sample unknown (no environment for " + target + ")"
	}
	probe, err := s.fetchDataProbe(ctx, env)
	if err != nil {
		return "sample unknown: " + err.Error()
	}
	if probe.Sample == nil || probe.Sample.Rows == nil {
		return "sample unknown: data-probe has no sample"
	}
	rows := *probe.Sample.Rows
	vr.SampleRows = &rows
	vr.SampleLabel = strings.TrimSpace(probe.Sample.Label)
	label := vr.SampleLabel
	if label == "" {
		label = "sample"
	}
	return fmt.Sprintf("%s rows=%d", label, rows)
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
		var refused *ErrCloneFKClosure
		if errors.As(err, &refused) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":          refused.Error(),
				"target":         refused.Target,
				"missing_tables": refused.MissingTables(),
				"references":     refused.Missing,
			})
			return
		}
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
