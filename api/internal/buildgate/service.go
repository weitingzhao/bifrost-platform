package buildgate

import (
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

type Service struct {
	cfg   *config.Config
	store *Store
}

func NewService(cfg *config.Config) *Service {
	dir := "config"
	if cfg != nil && cfg.ConfigPath != "" {
		dir = cfg.ConfigDir()
	}
	return &Service{cfg: cfg, store: NewStore(dir)}
}

var phaseTaskPrefixes = map[string]string{
	"P1": "p1-",
	"P2": "p2-",
	"P3": "p3-",
	"P4": "p4-",
	"P5": "p5-",
	"P6": "p6-",
}

func (s *Service) GetGate(phase string) PhaseGateResponse {
	now := time.Now().UTC()
	tasks := s.tasksForPhase(phase)
	checks := s.buildChecks(tasks)
	signoff, _ := s.store.LoadSignoff(phase)
	lastGate, _ := s.store.LoadGate(phase)

	total := len(checks)
	done := 0
	blockers := []string{}
	for _, c := range checks {
		if c.Status == "pass" {
			done++
		} else if c.Required {
			blockers = append(blockers, c.Label+": "+c.Detail)
		}
	}
	ready := total > 0 && len(blockers) == 0
	result := "pass"
	if len(blockers) > 0 {
		result = "incomplete"
	}
	if total == 0 {
		result = "no_tasks"
		ready = false
	}

	resp := PhaseGateResponse{
		Phase:       phase,
		TotalTasks:  total,
		DoneTasks:   done,
		Ready:       ready,
		Result:      result,
		Checks:      checks,
		Blockers:    blockers,
		GeneratedAt: now,
	}
	if signoff != nil {
		at := signoff.At.UTC()
		resp.SignedAt = &at
		resp.SignedBy = signoff.SignedBy
	}
	if lastGate != nil {
		at := lastGate.At.UTC()
		resp.LastRunAt = &at
		resp.LastRunResult = lastGate.Result
	}
	return resp
}

func (s *Service) RunGate(phase, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	gate := s.GetGate(phase)

	rec := GateRecord{
		At:          now,
		Phase:       phase,
		Result:      gate.Result,
		Checks:      gate.Checks,
		TriggeredBy: triggeredBy,
	}
	if err := s.store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}

	gate = s.GetGate(phase)
	msg := fmt.Sprintf("Build phase %s gate: %d/%d tasks done", phase, gate.DoneTasks, gate.TotalTasks)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          gate.Ready,
		Action:      fmt.Sprintf("buildgate.%s-gate", strings.ToLower(phase)),
		Target:      fmt.Sprintf("build-phase-%s", strings.ToLower(phase)),
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) Signoff(phase, notes, signedBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	gate := s.GetGate(phase)
	if !gate.Ready {
		return RunGateResponse{}, fmt.Errorf("build phase %s gate not ready — %d/%d tasks done, blockers: %s",
			phase, gate.DoneTasks, gate.TotalTasks, strings.Join(gate.Blockers, "; "))
	}

	rec := SignoffRecord{
		At:       now,
		Phase:    phase,
		SignedBy: signedBy,
		Notes:    strings.TrimSpace(notes),
		Result:   "SIGNED",
	}
	if err := s.store.SaveSignoff(rec); err != nil {
		return RunGateResponse{}, err
	}

	gate = s.GetGate(phase)
	return RunGateResponse{
		OK:          true,
		Action:      fmt.Sprintf("buildgate.%s-signoff", strings.ToLower(phase)),
		Target:      fmt.Sprintf("build-phase-%s", strings.ToLower(phase)),
		Changed:     true,
		Message:     fmt.Sprintf("Build phase %s SIGNED by %s", phase, signedBy),
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) ListPhases() []PhaseGateResponse {
	phases := []string{"P1", "P2", "P3", "P4", "P5", "P6"}
	out := make([]PhaseGateResponse, 0, len(phases))
	for _, p := range phases {
		tasks := s.tasksForPhase(p)
		if len(tasks) > 0 {
			out = append(out, s.GetGate(p))
		}
	}
	return out
}

func (s *Service) tasksForPhase(phase string) []opscontext.TrackTask {
	prefix, ok := phaseTaskPrefixes[phase]
	if !ok {
		return nil
	}
	ctx := s.loadSpine()
	if ctx == nil || ctx.Tracks == nil || ctx.Tracks.Build == nil {
		return nil
	}
	out := []opscontext.TrackTask{}
	for _, t := range ctx.Tracks.Build.Tasks {
		if strings.HasPrefix(t.ID, prefix) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Service) buildChecks(tasks []opscontext.TrackTask) []GateCheck {
	checks := make([]GateCheck, 0, len(tasks))
	for _, t := range tasks {
		status := "pending"
		detail := ""
		switch t.Status {
		case "done":
			status = "pass"
			detail = "completed per spine"
		case "in_progress", "next":
			status = "in_progress"
			detail = "work in progress"
		case "blocked":
			status = "blocked"
			detail = "blocked — see spine for details"
		default:
			status = "pending"
			detail = "not yet started"
		}
		checks = append(checks, GateCheck{
			ID:       t.ID,
			Label:    t.Label,
			Status:   status,
			Required: true,
			Detail:   detail,
		})
	}
	return checks
}

func (s *Service) loadSpine() *opscontext.File {
	if s.cfg == nil {
		return nil
	}
	if s.cfg.OpsContext != nil {
		return s.cfg.OpsContext
	}
	path := opscontext.ResolvePath(s.cfg.ConfigPath)
	ctx, err := opscontext.Load(path)
	if err != nil {
		return nil
	}
	return ctx
}
