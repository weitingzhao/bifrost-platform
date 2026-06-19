package promote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type Service struct {
	cfg      *config.Config
	prober   *probe.Prober
	store    *Store
	delivery *delivery.Service
}

func NewService(cfg *config.Config) *Service {
	entry := cfg.DefaultCluster()
	return &Service{
		cfg:      cfg,
		prober:   probe.NewProber(),
		store:    NewStore(configDirFrom(cfg)),
		delivery: delivery.NewService(entry),
	}
}

func configDirFrom(cfg *config.Config) string {
	if cfg == nil || cfg.ConfigPath == "" {
		return "config"
	}
	return filepathDir(cfg.ConfigPath)
}

func filepathDir(p string) string {
	return filepath.Dir(p)
}

func (s *Service) LastGate(ctx context.Context, tier GateTier) ReleaseGateResponse {
	now := time.Now().UTC()
	rec, err := s.store.LoadTier(tier)
	if err != nil || rec == nil {
		return ReleaseGateResponse{
			Tier:         tier,
			Result:       "",
			Reachability: probe.ReachUnknown,
			Detail:       fmt.Sprintf("No %s release gate recorded yet", tier),
			GeneratedAt:  now,
		}
	}
	return s.responseFromRecord(ctx, tier, *rec, now)
}

func (s *Service) RunReleaseGate(ctx context.Context, tier GateTier, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	var checks []GateCheck
	switch tier {
	case GateTierStg:
		checks = s.collectStgChecks(ctx)
	default:
		checks = s.collectProdChecks(ctx)
	}
	result := "pass"
	for _, c := range checks {
		if !c.Required {
			continue
		}
		if c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}

	logPath := s.store.Path()
	if envLog := strings.TrimSpace(os.Getenv("PLATFORM_RELEASE_GATE_LOG")); envLog != "" {
		logPath = envLog
	} else if s.cfg != nil && s.cfg.OpsContext != nil {
		logPath = s.cfg.OpsContext.Promotion.LastGate.LogPath
	}

	rec := ReleaseGateRecord{
		Tier:        tier,
		At:          now,
		Result:      result,
		LogPath:     logPath,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     fmt.Sprintf("%s release gate %s (%d checks)", tier, result, len(checks)),
	}
	if err := s.store.SaveTier(tier, rec); err != nil {
		return RunGateResponse{}, err
	}
	_ = s.store.AppendLog(fmt.Sprintf("%s %s by %s — %s", tier, result, triggeredBy, rec.Summary))

	gate := s.responseFromRecord(ctx, tier, rec, now)
	msg := fmt.Sprintf("%s release gate %s", tier, result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "promote.release-gate",
		Target:      string(tier) + "-release-gate",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectStgChecks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 16)

	if run := s.delivery.LastDeliverStgSuccess(ctx); run != nil {
		checks = append(checks, GateCheck{
			ID: "last-deliver-stg", Label: "Last bifrost-deliver-stg success", Required: true,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("%s (%s)", run.Name, run.Status),
		})
	} else {
		checks = append(checks, GateCheck{
			ID: "last-deliver-stg", Label: "Last bifrost-deliver-stg success", Required: true,
			Reachability: probe.ReachFail,
			Detail:       "No succeeded bifrost-deliver-stg PipelineRun found",
		})
	}

	stg := s.delivery.StgSmoke(ctx)
	if len(stg.Targets) == 0 {
		checks = append(checks, GateCheck{
			ID: "stg-smoke", Label: "K3s stg smoke", Required: true,
			Reachability: probe.ReachUnknown,
			Detail:       "stg smoke URLs not configured",
		})
		return checks
	}
	for _, t := range stg.Targets {
		required := t.ID == "stg-frontend" || strings.HasPrefix(t.ID, "stg-api-")
		checks = append(checks, GateCheck{
			ID: t.ID, Label: t.ID, Required: required,
			Reachability: t.Reachability, Detail: t.Detail,
		})
	}
	_ = ctx
	return checks
}

func (s *Service) collectProdChecks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 4)
	checks = append(checks, s.checkCutoverMilestone())
	checks = append(checks, s.checkProdMatrix(ctx)...)
	checks = append(checks, s.checkProdDeliverPipeline()...)

	stg := s.delivery.StgSmoke(ctx)
	if len(stg.Targets) == 0 {
		checks = append(checks, GateCheck{
			ID: "stg-smoke", Label: "K3s stg smoke (informational)", Required: false,
			Reachability: probe.ReachUnknown,
			Detail:       "stg smoke URLs not configured",
		})
	} else {
		for _, t := range stg.Targets {
			required := false
			checks = append(checks, GateCheck{
				ID: t.ID, Label: t.ID, Required: required,
				Reachability: t.Reachability, Detail: t.Detail,
			})
		}
	}
	return checks
}

func (s *Service) checkProdDeliverPipeline() []GateCheck {
	return []GateCheck{{
		ID: "deliver-prod-pipeline", Label: "bifrost-deliver-prod pipeline", Required: true,
		Reachability: probe.ReachFail,
		Detail:       "Not implemented — prod overlay + pipeline-deliver-prod in progress",
	}}
}

func (s *Service) checkCutoverMilestone() GateCheck {
	check := GateCheck{
		ID: "cutover-milestone", Label: "2c-b-prod-cutover milestone", Required: true,
		Reachability: probe.ReachOK, Detail: "No cutover blocker",
	}
	if s.cfg == nil || s.cfg.OpsContext == nil {
		check.Reachability = probe.ReachUnknown
		check.Detail = "ops context unavailable"
		return check
	}
	for _, m := range s.cfg.OpsContext.Milestones {
		if m.ID != "2c-b-prod-cutover" {
			continue
		}
		if m.Status == "BLOCKED_ON" {
			check.Reachability = probe.ReachFail
			blocker := m.Blocker
			if blocker == "" {
				blocker = "decision"
			}
			check.Detail = fmt.Sprintf("BLOCKED_ON: %s", blocker)
		}
		return check
	}
	check.Reachability = probe.ReachUnknown
	check.Detail = "milestone 2c-b-prod-cutover not found in spine"
	return check
}

func (s *Service) checkProdMatrix(ctx context.Context) []GateCheck {
	out := []GateCheck{}
	if s.cfg == nil {
		return []GateCheck{{
			ID: "prod-matrix", Label: "Prod matrix", Required: true,
			Reachability: probe.ReachFail, Detail: "config unavailable",
		}}
	}
	env, ok := s.cfg.GetEnvironment("prod")
	if !ok {
		return []GateCheck{{
			ID: "prod-matrix", Label: "Prod matrix", Required: true,
			Reachability: probe.ReachFail, Detail: "prod environment not configured",
		}}
	}
	matrix := s.prober.ProbeEnvironment(ctx, *env)
	failIDs := []string{}
	for _, t := range matrix.Targets {
		if t.Category == "trade_write" {
			continue
		}
		if t.Reachability == probe.ReachFail {
			failIDs = append(failIDs, t.ID)
		}
	}
	check := GateCheck{
		ID: "prod-matrix", Label: "Prod matrix (all trade probes)", Required: true,
		Detail: fmt.Sprintf("%d target(s) probed via %s", len(matrix.Targets), env.NginxBase),
	}
	if len(failIDs) > 0 {
		check.Reachability = probe.ReachFail
		check.Detail = fmt.Sprintf("failing: %s", strings.Join(failIDs, ", "))
	} else {
		check.Reachability = probe.ReachOK
		check.Detail = "no failing prod targets"
	}
	out = append(out, check)
	return out
}

func (s *Service) responseFromRecord(ctx context.Context, tier GateTier, rec ReleaseGateRecord, now time.Time) ReleaseGateResponse {
	blockers := narrativeBlockers(tier, s.cfg, rec)
	ready := rec.Result == "pass" && len(blockers) == 0
	reach := probe.ReachOK
	if rec.Result == "fail" {
		reach = probe.ReachFail
	} else if rec.Result == "" {
		reach = probe.ReachUnknown
	} else if !ready {
		reach = probe.ReachDegraded
	}
	detail := rec.Summary
	if detail == "" {
		detail = fmt.Sprintf("%s release gate %s", tier, rec.Result)
	}
	_ = ctx
	return ReleaseGateResponse{
		Tier:         tier,
		Result:       rec.Result,
		At:           rec.At,
		LogPath:      rec.LogPath,
		Checks:       rec.Checks,
		Ready:        ready,
		Blockers:     blockers,
		GeneratedAt:  now,
		Reachability: reach,
		Detail:       detail,
	}
}

func narrativeBlockers(tier GateTier, cfg *config.Config, rec ReleaseGateRecord) []string {
	var blockers []string
	if rec.Result != "pass" {
		blockers = append(blockers, "Release gate checks failed")
	}
	if tier != GateTierProd {
		return blockers
	}
	if cfg == nil || cfg.OpsContext == nil {
		return blockers
	}
	for _, m := range cfg.OpsContext.Milestones {
		if m.ID == "2c-b-prod-cutover" && m.Status == "BLOCKED_ON" {
			blockers = append(blockers, fmt.Sprintf("Milestone blocked: %s", m.Blocker))
		}
	}
	return blockers
}

// OverlayContext merges persisted gate state into spine context for GET /context.
func OverlayContext(base *opscontext.File, store *Store) *opscontext.File {
	if base == nil || store == nil {
		return base
	}
	rec, err := store.LoadTier(GateTierProd)
	if err != nil || rec == nil {
		return base
	}
	out := *base
	at := rec.At.UTC().Format(time.RFC3339)
	result := rec.Result
	out.Promotion.LastGate.At = &at
	out.Promotion.LastGate.Result = &result
	if rec.LogPath != "" {
		out.Promotion.LastGate.LogPath = rec.LogPath
	}
	if out.EnvironmentsExtended == nil {
		out.EnvironmentsExtended = map[string]opscontext.EnvironmentExtended{}
	}
	stg := out.EnvironmentsExtended["staging"]
	stgSmokeOK := false
	for _, c := range rec.Checks {
		if c.ID == "stg-api-monitor" && (c.Reachability == probe.ReachOK || c.Reachability == probe.ReachDegraded) {
			stgSmokeOK = true
		}
	}
	stgRec, _ := store.LoadTier(GateTierStg)
	if stgRec != nil && stgRec.Result == "pass" {
		stg.Status = "IN_PROGRESS"
		stg.Note = "STG release gate pass — Tier B + Promote prod track separate"
	} else if stgSmokeOK {
		stg.Status = "IN_PROGRESS"
		stg.Note = "K3s bifrost-stg smoke OK — run STG release gate on Promote"
	} else if rec.Result == "pass" {
		stg.Status = "IN_PROGRESS"
		stg.Note = "Prod release gate pass; verify stg on Delivery"
	}
	out.EnvironmentsExtended["staging"] = stg
	return &out
}
