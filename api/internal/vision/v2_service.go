package vision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	tektonPipelineStg  = "bifrost-deliver-stg"
	tektonPipelineProd = "bifrost-deliver-prod"
)

func (s *Service) V2Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.v2store.LoadGate()
	signoff, _ := s.v2store.LoadSignoff()
	checks := s.collectV2Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	blockers := gateBlockers(checks)
	ready := result == "pass" && len(blockers) == 0
	reach := probe.ReachOK
	if result == "fail" {
		reach = probe.ReachFail
	} else if !ready {
		reach = probe.ReachDegraded
	}
	out := V1GateResponse{
		Milestone:    "V2",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision V2 Dev Agent closed-loop gate %s (%d checks)", result, len(checks)),
		GeneratedAt:  now,
	}
	if rec != nil {
		at := rec.At.UTC()
		out.At = &at
		if rec.Result != "" {
			out.Result = rec.Result
			out.Ready = rec.Result == "pass" && len(blockers) == 0
		}
	}
	if signoff != nil {
		at := signoff.At.UTC()
		out.SignedAt = &at
		out.SignedBy = signoff.SignedBy
	}
	return out
}

func (s *Service) RunV2Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectV2Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	rec := GateRecord{
		At:          now,
		Result:      result,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     fmt.Sprintf("Vision V2 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.v2store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.V2Gate(ctx)
	msg := fmt.Sprintf("Vision V2 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.v2-gate",
		Target:      "vision-v2-dev-agent",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignV2(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.V2Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision V2 gate not ready — run gate first and fix blockers")
	}
	rec := V1SignoffRecord{
		At:       now,
		SignedBy: signedBy,
		Notes:    strings.TrimSpace(notes),
		GateAt:   now,
		Result:   "SIGNED",
	}
	if gate.At != nil {
		rec.GateAt = *gate.At
	}
	if err := s.v2store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.V2Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.v2-signoff",
		Target:      "vision-v2-dev-agent",
		Changed:     true,
		Message:     "Vision V2 Dev Agent closed-loop SIGNED",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectV2Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 14)
	checks = append(checks, s.checkS3Prerequisite())
	checks = append(checks, s.checkDevAgentCatalog())
	checks = append(checks, s.checkPrePushScript())
	checks = append(checks, s.checkFileContains(
		"agent-protocol-v2", "Agent Protocol Dev Agent closed loop", true,
		consolePath(s.configDir, "src/lib/architecture/agentProtocolCatalog.ts"),
		"DEV_AGENT_CLOSED_LOOP",
	))
	checks = append(checks, s.checkTektonPipeline(ctx, tektonPipelineStg))
	checks = append(checks, s.checkTektonPipeline(ctx, tektonPipelineProd))
	checks = append(checks, s.checkLastDeliverStg(ctx))
	checks = append(checks, s.checkStgSmoke(ctx))
	checks = append(checks, s.checkFileContains(
		"promote-gate-route", "Promote release-gate API registered", true,
		platformAPIPath(s.configDir, "internal/server/server.go"),
		"/promote/release-gate",
	))
	checks = append(checks, s.checkFileContains(
		"delivery-run-route", "Delivery pipeline run API registered", true,
		platformAPIPath(s.configDir, "internal/server/server.go"),
		"/delivery/pipelines/{name}/runs",
	))
	checks = append(checks, s.checkSpineMilestoneV2("vision-v2-dev-agent"))
	return checks
}

func (s *Service) checkS3Prerequisite() GateCheck {
	check := GateCheck{
		ID: "s3-prerequisite", Label: "Vision S3 signed (prerequisite)", Required: true,
		Reachability: probe.ReachFail, Detail: "S3 signoff not recorded",
	}
	signoff, err := s.s3store.LoadSignoff()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if signoff == nil || signoff.Result != "SIGNED" {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("S3 SIGNED by %s", signoff.SignedBy)
	return check
}

func (s *Service) checkDevAgentCatalog() GateCheck {
	check := GateCheck{
		ID: "dev-agent-catalog", Label: "devAgentLoopCatalog.ts", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := consolePath(s.configDir, "src/lib/architecture/devAgentLoopCatalog.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	body := string(data)
	if !strings.Contains(body, "DEV_AGENT_LOOP_STEPS") || !strings.Contains(body, tektonPipelineStg) {
		check.Detail = "missing DEV_AGENT_LOOP_STEPS or pipeline refs"
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%d loop steps defined", strings.Count(body, "order:"))
	return check
}

func (s *Service) checkPrePushScript() GateCheck {
	check := GateCheck{
		ID: "pre-push-script", Label: "Frontend agent-pre-push.sh", Required: true,
		Reachability: probe.ReachFail, Detail: "script not found",
	}
	candidates := []string{}
	if p := os.Getenv("PLATFORM_VISION_V2_PRE_PUSH_SCRIPT"); p != "" {
		candidates = append(candidates, p)
	}
	if s.configDir != "" {
		candidates = append(candidates, filepath.Join(s.configDir, "..", "..", "bifrost-trade-frontend", "scripts", "agent-pre-push.sh"))
	}
	candidates = append(candidates, filepath.Join("..", "bifrost-trade-frontend", "scripts", "agent-pre-push.sh"))
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		body := string(data)
		if strings.Contains(body, "npm run lint") && strings.Contains(body, "check:legacy-css") {
			check.Reachability = probe.ReachOK
			check.Detail = filepath.Base(abs) + " (lint + build + legacy-css)"
			return check
		}
	}
	check.Detail = "create bifrost-trade-frontend/scripts/agent-pre-push.sh"
	return check
}

func (s *Service) checkTektonPipeline(ctx context.Context, name string) GateCheck {
	check := GateCheck{
		ID: "tekton-" + name, Label: "Tekton pipeline: " + name, Required: true,
		Reachability: probe.ReachFail, Detail: "pipeline not found",
	}
	resp := s.delivery.Pipelines(ctx)
	if resp.Reachability == probe.ReachFail {
		check.Detail = resp.Detail
		return check
	}
	for _, p := range resp.Pipelines {
		if p.Name == name {
			check.Reachability = probe.ReachOK
			check.Detail = fmt.Sprintf("registered in %s", resp.Namespace)
			return check
		}
	}
	if resp.Reachability == probe.ReachDegraded {
		check.Reachability = probe.ReachDegraded
	}
	check.Detail = fmt.Sprintf("%s not in namespace %s", name, resp.Namespace)
	return check
}

func (s *Service) checkLastDeliverStg(ctx context.Context) GateCheck {
	check := GateCheck{
		ID: "deliver-stg-success", Label: "Last deliver-stg success", Required: true,
		Reachability: probe.ReachFail, Detail: "no succeeded PipelineRun",
	}
	run := s.delivery.LastDeliverStgSuccess(ctx)
	if run == nil {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%s @ %s", run.Name, run.CompletionTime)
	return check
}

func (s *Service) checkStgSmoke(ctx context.Context) GateCheck {
	check := GateCheck{
		ID: "stg-smoke", Label: "STG smoke (9 APIs)", Required: true,
		Reachability: probe.ReachFail, Detail: "smoke probe failed",
	}
	smoke := s.delivery.StgSmoke(ctx)
	if smoke.Reachability != probe.ReachOK {
		check.Detail = smoke.Detail
		return check
	}
	fail := 0
	for _, t := range smoke.Targets {
		if t.Reachability == probe.ReachFail {
			fail++
		}
	}
	if fail > 0 {
		check.Detail = fmt.Sprintf("%d failing target(s)", fail)
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%d target(s) ok", len(smoke.Targets))
	return check
}

func (s *Service) checkSpineMilestoneV2(id string) GateCheck {
	check := GateCheck{
		ID: "spine-" + id, Label: "Spine milestone: " + id, Required: true,
		Reachability: probe.ReachFail, Detail: "not in ops-context.yaml",
	}
	if s.cfg == nil || s.cfg.OpsContext == nil {
		return check
	}
	for _, m := range s.cfg.OpsContext.Milestones {
		if m.ID != id {
			continue
		}
		check.Reachability = probe.ReachOK
		check.Detail = "status=" + m.Status
		return check
	}
	return check
}

func platformAPIPath(configDir, rel string) string {
	candidates := []string{}
	if configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "..", "api", rel))
	}
	candidates = append(candidates, filepath.Join("api", rel))
	for _, p := range candidates {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	if configDir != "" {
		return filepath.Join(configDir, "..", "api", rel)
	}
	return filepath.Join("api", rel)
}
