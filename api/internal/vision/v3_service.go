package vision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	minMcpL1Tools = 6
	minMcpL2Tools = 3
)

func (s *Service) V3Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.v3store.LoadGate()
	signoff, _ := s.v3store.LoadSignoff()
	checks := s.collectV3Checks(ctx)
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
		Milestone:    "V3",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision V3 Ops Agent L1/L2 gate %s (%d checks)", result, len(checks)),
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

func (s *Service) RunV3Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectV3Checks(ctx)
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
		Summary:     fmt.Sprintf("Vision V3 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.v3store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.V3Gate(ctx)
	msg := fmt.Sprintf("Vision V3 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.v3-gate",
		Target:      "vision-v3-ops-agent",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignV3(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.V3Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision V3 gate not ready — run gate first and fix blockers")
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
	if err := s.v3store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.V3Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.v3-signoff",
		Target:      "vision-v3-ops-agent",
		Changed:     true,
		Message:     "Vision V3 Ops Agent L1/L2 SIGNED",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectV3Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 16)
	checks = append(checks, s.checkV2Prerequisite())
	checks = append(checks, s.checkOpsAgentCatalog())
	checks = append(checks, s.checkFileContains(
		"agent-protocol-v3", "Agent Protocol Ops Agent closed loop", true,
		consolePath(s.configDir, "src/lib/architecture/agentProtocolCatalog.ts"),
		"OPS_AGENT_CLOSED_LOOP",
	))
	checks = append(checks, s.checkMcpPlatformScript())
	checks = append(checks, s.checkMcpL1L2Tools())
	checks = append(checks, s.checkConfigFile(
		"cursor-mcp-bridges", "Cursor MCP bridges config", true,
		filepath.Join(s.configDir, "cursor-mcp-bridges.json"),
		"bifrost-platform",
	))
	checks = append(checks, s.checkConfigFile(
		"alertmanager-spec", "Alertmanager webhook spec", true,
		filepath.Join(s.configDir, "ops-agent-alertmanager.yaml"),
		"ops-agent/alertmanager",
	))
	checks = append(checks, s.checkFileContains(
		"alertmanager-webhook-route", "Ops Agent alertmanager webhook route", true,
		platformAPIPath(s.configDir, "internal/server/server.go"),
		"/ops-agent/alertmanager",
	))
	checks = append(checks, s.checkFileContains(
		"audit-api-route", "Audit log API", true,
		platformAPIPath(s.configDir, "internal/server/server.go"),
		"/audit",
	))
	checks = append(checks, s.checkFileContains(
		"audit-ui", "Audit page in Console", true,
		consolePath(s.configDir, "src/pages/AuditPage.tsx"),
		"AuditRecordsPanel",
	))
	checks = append(checks, s.checkFileContains(
		"mcp-contract-l1-l2", "MCP permission L1/L2 in contract", true,
		consolePath(s.configDir, "src/lib/standards/mcpContractCatalog.ts"),
		"L1 — Routine actuation",
	))
	checks = append(checks, s.checkSpineMilestoneV2("vision-v3-ops-agent"))
	_ = ctx
	return checks
}

func (s *Service) checkV2Prerequisite() GateCheck {
	check := GateCheck{
		ID: "v2-prerequisite", Label: "Vision V2 signed (prerequisite)", Required: true,
		Reachability: probe.ReachFail, Detail: "V2 signoff not recorded",
	}
	signoff, err := s.v2store.LoadSignoff()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if signoff == nil || signoff.Result != "SIGNED" {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("V2 SIGNED by %s", signoff.SignedBy)
	return check
}

func (s *Service) checkOpsAgentCatalog() GateCheck {
	check := GateCheck{
		ID: "ops-agent-catalog", Label: "opsAgentLoopCatalog.ts", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := consolePath(s.configDir, "src/lib/architecture/opsAgentLoopCatalog.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	body := string(data)
	if !strings.Contains(body, "OPS_AGENT_LOOP_STEPS") || !strings.Contains(body, "L1") {
		check.Detail = "missing OPS_AGENT_LOOP_STEPS or L1/L2 levels"
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%d loop steps defined", strings.Count(body, "order:"))
	return check
}

func (s *Service) checkMcpPlatformScript() GateCheck {
	check := GateCheck{
		ID: "mcp-platform-script", Label: "mcp-server-platform script", Required: true,
		Reachability: probe.ReachFail, Detail: "mcp/platform/src/index.ts not found",
	}
	candidates := []string{}
	if s.configDir != "" {
		candidates = append(candidates, filepath.Join(s.configDir, "..", "mcp", "platform", "src", "index.ts"))
	}
	candidates = append(candidates, filepath.Join("mcp", "platform", "src", "index.ts"))
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			check.Reachability = probe.ReachOK
			check.Detail = filepath.Base(abs) + " (" + mcp.ServerName + ")"
			return check
		}
	}
	return check
}

func (s *Service) checkMcpL1L2Tools() GateCheck {
	check := GateCheck{
		ID: "mcp-l1-l2-tools", Label: "MCP L1/L2 actuation tools", Required: true,
		Reachability: probe.ReachFail, Detail: "insufficient routine/confirm tools",
	}
	l1, l2, impl := 0, 0, 0
	for _, t := range mcp.Catalog() {
		if !t.Implemented {
			continue
		}
		impl++
		switch t.Level {
		case "routine":
			l1++
		case "confirm":
			l2++
		}
	}
	if l1 >= minMcpL1Tools && l2 >= minMcpL2Tools {
		check.Reachability = probe.ReachOK
		check.Detail = fmt.Sprintf("%d implemented tools — L1=%d L2=%d", impl, l1, l2)
		return check
	}
	check.Detail = fmt.Sprintf("L1=%d (need %d) L2=%d (need %d)", l1, minMcpL1Tools, l2, minMcpL2Tools)
	return check
}

func (s *Service) checkConfigFile(id, label string, required bool, path, needle string) GateCheck {
	check := GateCheck{
		ID: id, Label: label, Required: required,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if needle != "" && !strings.Contains(string(data), needle) {
		check.Detail = "missing " + needle
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = filepath.Base(path)
	return check
}
