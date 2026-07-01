package promote

import (
	"context"
	"fmt"
	"net/http"
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
	case GateTierPlatformStg:
		checks = s.collectPlatformStgChecks(ctx)
	case GateTierPlatformProd:
		checks = s.collectPlatformProdChecks(ctx)
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

	revision := s.resolveDeployRevision(ctx, tier)

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
		Revision:    revision,
		LogPath:     logPath,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     fmt.Sprintf("%s release gate %s (%d checks)", tier, result, len(checks)),
	}
	if err := s.store.SaveTier(tier, rec); err != nil {
		return RunGateResponse{}, err
	}
	_ = s.store.AppendHistory(tier, rec)
	_ = s.store.AppendLog(fmt.Sprintf("%s %s by %s — %s", tier, result, triggeredBy, rec.Summary))
	if !IsPlatformTier(tier) {
		s.writeBackSpine(rec)
	}

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
	checks = append(checks, s.checkProdDeliverPipeline(ctx)...)

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

func (s *Service) checkProdDeliverPipeline(ctx context.Context) []GateCheck {
	if run := s.delivery.LastDeliverProdSuccess(ctx); run != nil {
		return []GateCheck{{
			ID: "deliver-prod-pipeline", Label: "bifrost-deliver-prod pipeline", Required: true,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("%s (%s)", run.Name, run.Status),
		}}
	}
	smoke := s.delivery.ProdSmoke(ctx)
	smokeOK := smoke.Reachability == probe.ReachOK
	buildsOK, buildDetail := s.delivery.ProdDeliverArtifactsReady(ctx)
	if smokeOK && buildsOK {
		return []GateCheck{{
			ID: "deliver-prod-pipeline", Label: "bifrost-deliver-prod pipeline", Required: true,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("prod smoke OK + %s (full PipelineRun Succeeded pending audit re-run)", buildDetail),
		}}
	}
	last := s.delivery.LastDeliverProdRun(ctx)
	detail := "No succeeded bifrost-deliver-prod PipelineRun found"
	if last != nil {
		detail = fmt.Sprintf("Last run %s (%s)", last.Name, last.Status)
	}
	if !smokeOK {
		detail += "; prod smoke: " + smoke.Detail
	}
	if !buildsOK {
		detail += "; builds: " + buildDetail
	}
	return []GateCheck{{
		ID: "deliver-prod-pipeline", Label: "bifrost-deliver-prod pipeline", Required: true,
		Reachability: probe.ReachFail,
		Detail:       detail,
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
	redisInCluster := false
	for _, t := range matrix.Targets {
		if t.Category == "trade_write" {
			continue
		}
		if t.ID == "redis" {
			if reach, detail := s.delivery.ProdRedisInCluster(ctx); reach == probe.ReachOK {
				redisInCluster = true
				continue
			} else if reach == probe.ReachDegraded {
				failIDs = append(failIDs, t.ID+" (in-cluster: "+detail+")")
				continue
			}
		}
		if t.Reachability == probe.ReachFail {
			failIDs = append(failIDs, t.ID)
		}
	}
	check := GateCheck{
		ID: "prod-matrix", Label: "Prod matrix (all trade probes)", Required: true,
		Detail: fmt.Sprintf("%d target(s) probed via %s", len(matrix.Targets), env.NginxBase),
	}
	if redisInCluster {
		check.Detail += "; redis via in-cluster data/redis-live-prod"
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

func (s *Service) collectPlatformStgChecks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 6)

	if run := s.delivery.LastDeliverPlatformStgSuccess(ctx); run != nil {
		checks = append(checks, GateCheck{
			ID: "platform-deliver-stg", Label: "Last bifrost-deliver-platform success", Required: true,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("%s (%s)", run.Name, run.Status),
		})
	} else {
		checks = append(checks, GateCheck{
			ID: "platform-deliver-stg", Label: "Last bifrost-deliver-platform success", Required: true,
			Reachability: probe.ReachFail,
			Detail:       "No succeeded bifrost-deliver-platform PipelineRun found",
		})
	}

	checks = append(checks, s.probePlatformHTTP(ctx, "platform-stg-console", s.platformStgConsoleURL())...)
	checks = append(checks, s.probePlatformHTTP(ctx, "platform-stg-api", s.platformStgAPIURL())...)
	return checks
}

func (s *Service) collectPlatformProdChecks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 8)

	if run := s.delivery.LastDeliverPlatformProdSuccess(ctx); run != nil {
		checks = append(checks, GateCheck{
			ID: "platform-deliver-prod", Label: "Last bifrost-deliver-platform-prod success", Required: true,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("%s (%s)", run.Name, run.Status),
		})
	} else {
		last := s.delivery.LastDeliverPlatformProdRun(ctx)
		detail := "No succeeded bifrost-deliver-platform-prod PipelineRun found"
		if last != nil {
			detail = fmt.Sprintf("Last run %s (%s)", last.Name, last.Status)
		}
		checks = append(checks, GateCheck{
			ID: "platform-deliver-prod", Label: "Last bifrost-deliver-platform-prod success", Required: true,
			Reachability: probe.ReachFail,
			Detail:       detail,
		})
	}

	checks = append(checks, s.probePlatformHTTP(ctx, "platform-prod-console", s.platformProdConsoleURL())...)
	checks = append(checks, s.probePlatformHTTP(ctx, "platform-prod-api", s.platformProdAPIURL())...)

	stgRec, _ := s.store.LoadTier(GateTierPlatformStg)
	stgCheck := GateCheck{
		ID: "platform-stg-gate", Label: "Platform STG gate pass", Required: true,
	}
	if stgRec != nil && stgRec.Result == "pass" {
		stgCheck.Reachability = probe.ReachOK
		stgCheck.Detail = fmt.Sprintf("STG gate pass at %s", stgRec.At.Format(time.RFC3339))
	} else {
		stgCheck.Reachability = probe.ReachFail
		stgCheck.Detail = "Platform STG gate not yet passed"
		if stgRec != nil {
			stgCheck.Detail = fmt.Sprintf("Platform STG gate %s at %s", stgRec.Result, stgRec.At.Format(time.RFC3339))
		}
	}
	checks = append(checks, stgCheck)

	return checks
}

func (s *Service) probePlatformHTTP(ctx context.Context, id, url string) []GateCheck {
	if url == "" {
		return []GateCheck{{
			ID: id, Label: id, Required: true,
			Reachability: probe.ReachUnknown,
			Detail:       "URL not configured",
		}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return []GateCheck{{
			ID: id, Label: id, Required: true,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("build request: %v", err),
		}}
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return []GateCheck{{
			ID: id, Label: id, Required: true,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("HTTP probe failed: %v", err),
		}}
	}
	defer resp.Body.Close()
	reach := probe.ReachOK
	if resp.StatusCode >= 400 {
		reach = probe.ReachFail
	}
	return []GateCheck{{
		ID: id, Label: id, Required: true,
		Reachability: reach,
		Detail:       fmt.Sprintf("HTTP %d from %s", resp.StatusCode, url),
	}}
}

func (s *Service) platformStgConsoleURL() string {
	e := s.cfg.DefaultCluster()
	if e != nil && e.StgSmoke.PlatformConsoleURL != "" {
		return e.StgSmoke.PlatformConsoleURL
	}
	return "http://192.168.10.73:30879"
}

func (s *Service) platformStgAPIURL() string {
	e := s.cfg.DefaultCluster()
	if e != nil && e.StgSmoke.PlatformAPIHealthURL != "" {
		return e.StgSmoke.PlatformAPIHealthURL
	}
	return "http://192.168.10.73:30878/health"
}

func (s *Service) platformProdConsoleURL() string {
	e := s.cfg.DefaultCluster()
	if e != nil && e.ProdSmoke.PlatformConsoleURL != "" {
		return e.ProdSmoke.PlatformConsoleURL
	}
	return "http://192.168.10.73:30877"
}

func (s *Service) platformProdAPIURL() string {
	e := s.cfg.DefaultCluster()
	if e != nil && e.ProdSmoke.PlatformAPIHealthURL != "" {
		return e.ProdSmoke.PlatformAPIHealthURL
	}
	return "http://192.168.10.73:30876/health"
}

func (s *Service) resolveDeployRevision(ctx context.Context, tier GateTier) string {
	var run *delivery.PipelineRunView
	switch tier {
	case GateTierStg:
		run = s.delivery.LastDeliverStgSuccess(ctx)
	case GateTierPlatformStg:
		run = s.delivery.LastDeliverPlatformStgSuccess(ctx)
	case GateTierPlatformProd:
		run = s.delivery.LastDeliverPlatformProdSuccess(ctx)
	default:
		run = s.delivery.LastDeliverProdSuccess(ctx)
	}
	if run != nil {
		return run.Revision
	}
	return ""
}

func (s *Service) ReleaseState(ctx context.Context, tier string) ReleaseStateResponse {
	now := time.Now().UTC()
	resp := ReleaseStateResponse{
		Consistent:  true,
		GeneratedAt: now,
	}

	isPlatform := tier == "platform"

	stgPipeline := "bifrost-deliver-stg"
	prodPipeline := "bifrost-deliver-prod"
	stgGateTier := GateTierStg
	prodGateTier := GateTierProd
	if isPlatform {
		stgPipeline = "bifrost-deliver-platform"
		prodPipeline = "bifrost-deliver-platform-prod"
		stgGateTier = GateTierPlatformStg
		prodGateTier = GateTierPlatformProd
	}

	stgRun := s.delivery.LastDeliverStgSuccess(ctx)
	if isPlatform {
		stgRun = s.delivery.LastDeliverPlatformStgSuccess(ctx)
	}
	if stgRun != nil {
		at := parseTimeOpt(stgRun.StartTime)
		resp.StgDeploy = ReleaseStageState{
			Revision: stgRun.Revision,
			Status:   "succeeded",
			At:       at,
			Detail:   stgRun.Name,
		}
	} else {
		resp.StgDeploy = ReleaseStageState{
			Status: "none",
			Detail: "No succeeded " + stgPipeline + " PipelineRun",
		}
	}

	stgGate, _ := s.store.LoadTier(stgGateTier)
	if stgGate != nil {
		resp.StgGate = ReleaseStageState{
			Revision: stgGate.Revision,
			Status:   stgGate.Result,
			At:       &stgGate.At,
			Detail:   stgGate.Summary,
		}
	} else {
		resp.StgGate = ReleaseStageState{
			Status: "none",
			Detail: "No STG gate recorded",
		}
	}

	prodRun := s.delivery.LastDeliverProdSuccess(ctx)
	if isPlatform {
		prodRun = s.delivery.LastDeliverPlatformProdSuccess(ctx)
	}
	if prodRun != nil {
		at := parseTimeOpt(prodRun.StartTime)
		resp.ProdDeploy = ReleaseStageState{
			Revision: prodRun.Revision,
			Status:   "succeeded",
			At:       at,
			Detail:   prodRun.Name,
		}
	} else {
		resp.ProdDeploy = ReleaseStageState{
			Status: "none",
			Detail: "No succeeded " + prodPipeline + " PipelineRun",
		}
	}

	prodGate, _ := s.store.LoadTier(prodGateTier)
	if prodGate != nil {
		resp.ProdGate = ReleaseStageState{
			Revision: prodGate.Revision,
			Status:   prodGate.Result,
			At:       &prodGate.At,
			Detail:   prodGate.Summary,
		}
	} else {
		resp.ProdGate = ReleaseStageState{
			Status: "none",
			Detail: "No PROD gate recorded",
		}
	}

	revisions := []string{}
	for _, rev := range []string{
		resp.StgDeploy.Revision, resp.StgGate.Revision,
		resp.ProdDeploy.Revision, resp.ProdGate.Revision,
	} {
		if rev != "" {
			revisions = append(revisions, rev)
		}
	}
	if len(revisions) >= 2 {
		first := revisions[0]
		for _, r := range revisions[1:] {
			if r != first {
				resp.Consistent = false
				resp.Warnings = append(resp.Warnings, fmt.Sprintf(
					"Revision mismatch across stages: %s", strings.Join(unique(revisions), ", ")))
				break
			}
		}
	}

	if resp.StgDeploy.Revision != "" && resp.ProdDeploy.Revision != "" &&
		resp.StgDeploy.Revision != resp.ProdDeploy.Revision {
		resp.Warnings = appendUnique(resp.Warnings,
			fmt.Sprintf("STG deployed %s but PROD deployed %s", resp.StgDeploy.Revision, resp.ProdDeploy.Revision))
	}

	resp.AvailableActions, resp.NextAction = s.resolveReleaseActions(resp, stgPipeline, prodPipeline, stgGateTier, prodGateTier)

	return resp
}

func (s *Service) resolveReleaseActions(
	state ReleaseStateResponse,
	stgPipeline, prodPipeline string,
	stgGateTier, prodGateTier GateTier,
) ([]ReleaseAction, *ReleaseAction) {
	var actions []ReleaseAction
	var next *ReleaseAction

	deploySTG := ReleaseAction{
		Action:      "deploy_stg",
		Label:       "Deploy to STG",
		Description: "Start " + stgPipeline + " pipeline with a Gitea tag revision",
		MCPTool:     "start_pipeline_run",
		Params:      map[string]string{"name": stgPipeline},
	}
	runSTGGate := ReleaseAction{
		Action:      "run_stg_gate",
		Label:       "Run STG Gate",
		Description: "Execute STG release gate checks",
		MCPTool:     "run_release_gate",
		Params:      map[string]string{"tier": string(stgGateTier)},
	}
	deployPROD := ReleaseAction{
		Action:      "deploy_prod",
		Label:       "Deploy to PROD",
		Description: "Start " + prodPipeline + " pipeline (use same revision as STG)",
		MCPTool:     "start_pipeline_run",
		Params:      map[string]string{"name": prodPipeline},
	}
	runPRODGate := ReleaseAction{
		Action:      "run_prod_gate",
		Label:       "Run PROD Gate",
		Description: "Execute PROD release gate checks",
		MCPTool:     "run_release_gate",
		Params:      map[string]string{"tier": string(prodGateTier)},
	}
	checkState := ReleaseAction{
		Action:      "check_state",
		Label:       "Check Release State",
		Description: "Re-query release state for updated status",
		MCPTool:     "get_release_state",
	}

	switch {
	case state.StgDeploy.Status == "none":
		actions = append(actions, deploySTG)
		next = &deploySTG
	case state.StgGate.Status == "none" || state.StgGate.Status == "fail":
		actions = append(actions, runSTGGate, deploySTG)
		next = &runSTGGate
	case state.StgGate.Status == "pass" && state.ProdDeploy.Status == "none":
		actions = append(actions, deployPROD, deploySTG)
		next = &deployPROD
		if state.StgDeploy.Revision != "" {
			deployPROD.Description += " — revision: " + state.StgDeploy.Revision
			deployPROD.Params["revision"] = state.StgDeploy.Revision
		}
	case state.ProdDeploy.Status == "succeeded" && (state.ProdGate.Status == "none" || state.ProdGate.Status == "fail"):
		actions = append(actions, runPRODGate, deployPROD)
		next = &runPRODGate
	case state.ProdGate.Status == "pass":
		released := ReleaseAction{
			Action:      "released",
			Label:       "Released",
			Description: "All stages passed — release complete",
		}
		actions = append(actions, released, deploySTG)
		next = &released
	default:
		actions = append(actions, checkState, deploySTG)
		next = &checkState
	}

	return actions, next
}

func parseTimeOpt(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func unique(ss []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
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
		Revision:     rec.Revision,
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

func (s *Service) writeBackSpine(rec ReleaseGateRecord) {
	if s.cfg == nil || s.cfg.ConfigPath == "" {
		return
	}
	spinePath := opscontext.ResolvePath(s.cfg.ConfigPath)
	at := rec.At.UTC().Format(time.RFC3339)
	_ = opscontext.UpdateLastGate(spinePath, at, rec.Result)
}

func (s *Service) GateHistory(tier GateTier) ([]ReleaseGateRecord, error) {
	return s.store.LoadHistory(tier)
}

func narrativeBlockers(tier GateTier, cfg *config.Config, rec ReleaseGateRecord) []string {
	var blockers []string
	if rec.Result != "pass" {
		blockers = append(blockers, "Release gate checks failed")
	}
	if IsPlatformTier(tier) {
		return blockers
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
