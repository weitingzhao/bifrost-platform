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

var v5PrerequisiteSignoffs = []struct {
	id    string
	store func(*Service) (*V1SignoffRecord, error)
}{
	{"vision-v1-dev-topology", func(s *Service) (*V1SignoffRecord, error) { return s.store.LoadSignoff() }},
	{"vision-s3-briefing-alignment", func(s *Service) (*V1SignoffRecord, error) { return s.s3store.LoadSignoff() }},
	{"vision-v2-dev-agent", func(s *Service) (*V1SignoffRecord, error) { return s.v2store.LoadSignoff() }},
	{"vision-v3-ops-agent", func(s *Service) (*V1SignoffRecord, error) { return s.v3store.LoadSignoff() }},
	{"vision-v4-business-agent", func(s *Service) (*V1SignoffRecord, error) { return s.v4store.LoadSignoff() }},
}

func (s *Service) V5Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.v5store.LoadGate()
	signoff, _ := s.v5store.LoadSignoff()
	checks := s.collectV5Checks(ctx)
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
		Milestone:    "V5",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision V5 full convergence gate %s (%d checks)", result, len(checks)),
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

func (s *Service) RunV5Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectV5Checks(ctx)
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
		Summary:     fmt.Sprintf("Vision V5 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.v5store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.V5Gate(ctx)
	msg := fmt.Sprintf("Vision V5 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.v5-gate",
		Target:      "vision-v5-convergence",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignV5(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.V5Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision V5 gate not ready — run gate first and fix blockers")
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
	if err := s.v5store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.V5Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.v5-signoff",
		Target:      "vision-v5-convergence",
		Changed:     true,
		Message:     "Vision V5 full convergence SIGNED — Dual Flywheel complete",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectV5Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 18)
	checks = append(checks, s.checkV4Prerequisite())
	checks = append(checks, s.checkV5PrerequisiteSignoffs()...)
	checks = append(checks, s.checkConvergenceCatalog())
	checks = append(checks, s.checkFileContains(
		"agent-protocol-v5", "Agent Protocol convergence closed loop", true,
		consolePath(s.configDir, "src/lib/architecture/agentProtocolCatalog.ts"),
		"CONVERGENCE_CLOSED_LOOP",
	))
	checks = append(checks, s.checkConfigFile(
		"unified-mcp", "Unified Cursor MCP config", true,
		filepath.Join(s.configDir, "cursor-unified-mcp.json"),
		"bifrost-platform",
	))
	checks = append(checks, s.checkConfigFile(
		"unified-mcp-trade", "Unified MCP includes trade server", true,
		filepath.Join(s.configDir, "cursor-unified-mcp.json"),
		"bifrost-trade-api",
	))
	checks = append(checks, s.checkConfigFile(
		"ollama-spec", "Ollama local inference spec", true,
		filepath.Join(s.configDir, "ollama-agent.yaml"),
		"ollama",
	))
	checks = append(checks, s.checkConfigFile(
		"feedback-loop", "Convergence feedback loop spec", true,
		filepath.Join(s.configDir, "convergence-feedback-loop.yaml"),
		"L3 PR",
	))
	checks = append(checks, s.checkThreeAgentCatalogs()...)
	checks = append(checks, s.checkFileContains(
		"vision-gate-panels", "V1–V5 gate panels in Console", true,
		consolePath(s.configDir, "src/pages/DualFlywheelVisionPage.tsx"),
		"VisionV5GatePanel",
	))
	checks = append(checks, s.checkFileContains(
		"l3-boundary", "L3 PR governance boundary", true,
		consolePath(s.configDir, "src/lib/architecture/dualFlywheelVisionCatalog.ts"),
		"L3 requires Owner PR approval",
	))
	checks = append(checks, s.checkFileContains(
		"flywheel-convergence", "Dual flywheel convergence thesis", true,
		consolePath(s.configDir, "src/lib/architecture/dualFlywheelVisionCatalog.ts"),
		"FLYWHEEL_CONVERGENCE",
	))
	checks = append(checks, s.checkSpineMilestoneV2("vision-v5-convergence"))
	_ = ctx
	return checks
}

func (s *Service) checkV4Prerequisite() GateCheck {
	check := GateCheck{
		ID: "v4-prerequisite", Label: "Vision V4 signed (prerequisite)", Required: true,
		Reachability: probe.ReachFail, Detail: "V4 signoff not recorded",
	}
	signoff, err := s.v4store.LoadSignoff()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if signoff == nil || signoff.Result != "SIGNED" {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("V4 SIGNED by %s", signoff.SignedBy)
	return check
}

func (s *Service) checkV5PrerequisiteSignoffs() []GateCheck {
	out := make([]GateCheck, 0, len(v5PrerequisiteSignoffs))
	for _, p := range v5PrerequisiteSignoffs {
		check := GateCheck{
			ID: "signed-" + p.id, Label: "Signed: " + p.id, Required: true,
			Reachability: probe.ReachFail, Detail: "not SIGNED",
		}
		signoff, err := p.store(s)
		if err != nil {
			check.Detail = err.Error()
			out = append(out, check)
			continue
		}
		if signoff != nil && signoff.Result == "SIGNED" {
			check.Reachability = probe.ReachOK
			check.Detail = signoff.SignedBy
		}
		out = append(out, check)
	}
	return out
}

func (s *Service) checkConvergenceCatalog() GateCheck {
	check := GateCheck{
		ID: "convergence-catalog", Label: "convergenceLoopCatalog.ts", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := consolePath(s.configDir, "src/lib/architecture/convergenceLoopCatalog.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	body := string(data)
	if !strings.Contains(body, "CONVERGENCE_LOOP_STEPS") || !strings.Contains(body, "CONVERGENCE_PREREQUISITE_MILESTONES") {
		check.Detail = "missing CONVERGENCE_LOOP_STEPS or prerequisites"
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%d convergence steps", strings.Count(body, "order:"))
	return check
}

func (s *Service) checkThreeAgentCatalogs() []GateCheck {
	catalogs := []struct {
		id, file, needle string
	}{
		{"dev-agent-catalog-v5", "devAgentLoopCatalog.ts", "DEV_AGENT_LOOP_STEPS"},
		{"ops-agent-catalog-v5", "opsAgentLoopCatalog.ts", "OPS_AGENT_LOOP_STEPS"},
		{"business-agent-catalog-v5", "businessAgentLoopCatalog.ts", "BUSINESS_AGENT_LOOP_STEPS"},
	}
	out := make([]GateCheck, 0, len(catalogs))
	for _, c := range catalogs {
		check := GateCheck{
			ID: c.id, Label: c.file, Required: true,
			Reachability: probe.ReachFail, Detail: "missing",
		}
		path := consolePath(s.configDir, "src/lib/architecture/"+c.file)
		data, err := os.ReadFile(path)
		if err != nil {
			check.Detail = err.Error()
		} else if strings.Contains(string(data), c.needle) {
			check.Reachability = probe.ReachOK
			check.Detail = "OK"
		}
		out = append(out, check)
	}
	return out
}
