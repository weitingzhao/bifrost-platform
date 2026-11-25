package assembly

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The platform-workers' controller (agentrt/controller) as the platform runs
// it: server lends the database, the passes of other domains it runs in its
// facts-and-rules pass, and the readers of other domains' records; what
// verification decides itself (how often completed runs are recorded, the
// daily full CI and its runner) is set here. Moved out of server
// (agentrt_wire.go agentRuntimeController, agentrt_external_records.go
// wirePipelineRecords, agentrt_full_ci.go wireFullCI) on 2026-11-23,
// unchanged.

// Pass is one pass the controller runs for another domain: a step of the
// facts-and-rules pass, or, with Every, a loop of its own.
type Pass struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// ControllerSources are the readers the controller records from; a nil one
// reads nothing (agentrt/controller Sources says what each is for).
type ControllerSources struct {
	Approvals    func(context.Context) ([]seenfacts.ApprovalFact, error)
	Questions    func(context.Context) ([]seenfacts.Question, error)
	PipelineRuns func(context.Context) ([]seenfacts.PipelineRun, error)
	PipelineRun  func(ctx context.Context, name string) (p seenfacts.PipelineRun, ok bool, err error)
	CommitTree   func(ctx context.Context, repo, commit string) (string, error)
	RunFailure   func(ctx context.Context, run string) (tasks, tests []string, class string, err error)
	RunEvidenced func(ctx context.Context, p seenfacts.PipelineRun)
	UnitRuns     func(ctx context.Context) error
	UnitsDaily   func(ctx context.Context) error
	CINode       func(ctx context.Context) error
	HeadRelease  func(ctx context.Context) error
	Deployed     seenfacts.Deployed
}

// ControllerLent is what the platform lends the controller.
type ControllerLent struct {
	DB  eventlog.DB
	Env string // AGENTRT_ENV
	// Logf and Infof log what needs attention and what does not.
	Logf, Infof func(format string, args ...any)
	// Steps are the facts-and-rules pass's steps, in order; Loops are
	// loops of their own (each with Every).
	Steps, Loops []Pass
	Sources      ControllerSources
	// PipelineFinished names the runs the bus says finished (gate-cluster).
	PipelineFinished <-chan string
	// CI is the landing CI's client for the daily full CI; nil (no cluster)
	// wires none. FullCIZone is the zone of the full CI's day.
	CI         *CILent
	FullCIZone string
}

// Controller is the controller the platform runs.
type Controller struct {
	c         *controller.Controller
	fullCIRun func(ctx context.Context, revision string) (string, error)
}

// NewController builds the controller from what the platform lends.
func NewController(l ControllerLent) *Controller {
	src := l.Sources
	c := &controller.Controller{DB: l.DB, Env: l.Env, Logf: l.Logf, Infof: l.Infof, Sources: controller.Sources{
		Approvals: src.Approvals, Questions: src.Questions, PipelineRuns: src.PipelineRuns, PipelineRun: src.PipelineRun,
		CommitTree: src.CommitTree, RunFailure: src.RunFailure, RunEvidenced: src.RunEvidenced, UnitRuns: src.UnitRuns,
		UnitsDaily: src.UnitsDaily, CINode: src.CINode, HeadRelease: src.HeadRelease, Deployed: src.Deployed,
	}}
	for _, p := range l.Steps {
		c.Steps = append(c.Steps, controller.Step{Name: p.Name, Run: p.Run})
	}
	for _, p := range l.Loops {
		c.Loops = append(c.Loops, controller.Loop{Name: p.Name, Every: p.Every, Run: p.Run})
	}
	pipelineRecords(c, l.PipelineFinished)
	out := &Controller{c: c}
	out.fullCIRun = wireFullCI(c, l.CI, l.FullCIZone)
	wireUICheck(c, l.CI)
	return out
}

// FullCI is the controller's daily full CI, for the gauge /metrics serves;
// nil where the loop is off.
func (c *Controller) FullCI() *fullci.FullCI { return c.c.FullCI }

// FullCIRun starts a candidate's full CI with the daily full CI's runner
// (the PROD head-release loop, headrelease fullCIFor); nil where the loop is
// off.
func (c *Controller) FullCIRun() func(ctx context.Context, revision string) (string, error) {
	return c.fullCIRun
}

// Start starts the controller's loops; wait waits for them once ctx is done.
func (c *Controller) Start(ctx context.Context) (wait func()) { return c.c.Start(ctx) }

// pipelineRecordsEveryEnv sets how often the workers list the retained
// pipeline runs for the ones that completed, and pipelineRecordsFullEveryEnv
// how often such a pass compares every listed run with the database whatever
// it remembers: Go durations (30s, 5m), not under pipelineRecordsEveryMin.
// Unset, the controller's defaults hold (controller.DefaultPipelineEvery,
// controller.DefaultPipelineFullEvery).
const (
	pipelineRecordsEveryEnv     = "AGENTRT_PIPELINE_RECORDS_EVERY"
	pipelineRecordsFullEveryEnv = "AGENTRT_PIPELINE_RECORDS_FULL_EVERY"
	pipelineRecordsEveryMin     = 5 * time.Second
)

// pipelineRecordsDuration reads one of them; zero is the controller's default.
func pipelineRecordsDuration(name string) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < pipelineRecordsEveryMin {
		slog.Error("agentrt: "+name+" is not a duration of at least "+pipelineRecordsEveryMin.String()+"; the default holds", "value", v)
		return 0
	}
	return d
}

// pipelineRecords sets how often the controller records completed runs.
func pipelineRecords(c *controller.Controller, finished <-chan string) {
	c.PipelineEvery = pipelineRecordsDuration(pipelineRecordsEveryEnv)
	c.PipelineFullEvery = pipelineRecordsDuration(pipelineRecordsFullEveryEnv)
	// Bus step 2: gate-cluster names each finished run (startGateCluster).
	c.PipelineFinished = finished
}

// wireFullCI gives PROD's controller the daily full CI (agentrt/fullci): the
// landing CI's client creates and lists the runs, the platform's read-only
// Gitea identity reads main. AGENTRT_FULL_CI=off, another environment or no
// cluster wires none. It returns the full CI's runner, nil when none is wired.
func wireFullCI(c *controller.Controller, lent *CILent, zone string) func(ctx context.Context, revision string) (string, error) {
	on, at, param, err := fullci.FullCISettings(os.Getenv)
	if err != nil {
		slog.Error("agentrt full CI not started", "error", err)
	}
	if !on || c.Env != "prod" || c.DB == nil || lent == nil {
		return nil
	}
	ci := lent.client()
	ci.FullScopeParam = param
	ci.UIRevision = uiPinCommit(lent)
	// The full CI's day is the Owner's, in the zone of the briefs.
	c.FullCIZone = zone
	// The PROD head-release loop starts a candidate's full CI with the same
	// runner when that is all the candidate waits on (headrelease fullCIFor).
	run := fullCIRun(ci, zone)
	c.FullCI = newFullCI(ci, at)
	return run
}
