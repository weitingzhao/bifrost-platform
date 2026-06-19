package vision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/tradeagent"
)

const tradeDomainCount = 9

func (s *Service) V4Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.v4store.LoadGate()
	signoff, _ := s.v4store.LoadSignoff()
	checks := s.collectV4Checks(ctx)
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
		Milestone:    "V4",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision V4 Business Agent read-only gate %s (%d checks)", result, len(checks)),
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

func (s *Service) RunV4Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectV4Checks(ctx)
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
		Summary:     fmt.Sprintf("Vision V4 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.v4store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.V4Gate(ctx)
	msg := fmt.Sprintf("Vision V4 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.v4-gate",
		Target:      "vision-v4-business-agent",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignV4(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.V4Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision V4 gate not ready — run gate first and fix blockers")
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
	if err := s.v4store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.V4Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.v4-signoff",
		Target:      "vision-v4-business-agent",
		Changed:     true,
		Message:     "Vision V4 Business Agent read-only SIGNED",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectV4Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 16)
	checks = append(checks, s.checkV3Prerequisite())
	checks = append(checks, s.checkBusinessAgentCatalog())
	checks = append(checks, s.checkFileContains(
		"agent-protocol-v4", "Agent Protocol Business Agent closed loop", true,
		consolePath(s.configDir, "src/lib/architecture/agentProtocolCatalog.ts"),
		"BUSINESS_AGENT_CLOSED_LOOP",
	))
	checks = append(checks, s.checkConfigFile(
		"trade-api-domains", "Trade API domains registry (9)", true,
		filepath.Join(s.configDir, "trade-api-domains.yaml"),
		"id: research",
	))
	checks = append(checks, s.checkTradeDomainCount())
	checks = append(checks, s.checkConfigFile(
		"daily-brief-spec", "Business Agent brief schedule", true,
		filepath.Join(s.configDir, "business-agent-brief-schedule.yaml"),
		"pre-market",
	))
	checks = append(checks, s.checkConfigFile(
		"cursor-mcp-trade", "Cursor MCP trade bridge", true,
		filepath.Join(s.configDir, "cursor-mcp-trade.json"),
		"bifrost-trade-api",
	))
	checks = append(checks, s.checkMcpTradeScript())
	checks = append(checks, s.checkFileContains(
		"trade-agent-api", "Trade Agent catalog API", true,
		platformAPIPath(s.configDir, "internal/server/server.go"),
		"/trade-agent/catalog",
	))
	checks = append(checks, s.checkFileContains(
		"mcp-trade-readonly", "mcp-trade-api available in contract", true,
		consolePath(s.configDir, "src/lib/standards/mcpContractCatalog.ts"),
		"mcp-trade-api",
	))
	checks = append(checks, s.checkFileContains(
		"business-agent-boundary", "Business Agent read-only boundary", true,
		consolePath(s.configDir, "src/lib/architecture/dualFlywheelVisionCatalog.ts"),
		"Business Agent read-only",
	))
	checks = append(checks, s.checkStgSmoke(ctx))
	checks = append(checks, s.checkSpineMilestoneV2("vision-v4-business-agent"))
	return checks
}

func (s *Service) checkV3Prerequisite() GateCheck {
	check := GateCheck{
		ID: "v3-prerequisite", Label: "Vision V3 signed (prerequisite)", Required: true,
		Reachability: probe.ReachFail, Detail: "V3 signoff not recorded",
	}
	signoff, err := s.v3store.LoadSignoff()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if signoff == nil || signoff.Result != "SIGNED" {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("V3 SIGNED by %s", signoff.SignedBy)
	return check
}

func (s *Service) checkBusinessAgentCatalog() GateCheck {
	check := GateCheck{
		ID: "business-agent-catalog", Label: "businessAgentLoopCatalog.ts", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := consolePath(s.configDir, "src/lib/architecture/businessAgentLoopCatalog.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	body := string(data)
	if !strings.Contains(body, "BUSINESS_AGENT_LOOP_STEPS") || !strings.Contains(body, "TRADE_API_DOMAINS") {
		check.Detail = "missing BUSINESS_AGENT_LOOP_STEPS or TRADE_API_DOMAINS"
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("%d loop steps, %d domains", strings.Count(body, "order:"), tradeDomainCount)
	return check
}

func (s *Service) checkTradeDomainCount() GateCheck {
	check := GateCheck{
		ID: "trade-agent-domains", Label: "Trade Agent domain catalog", Required: true,
		Reachability: probe.ReachFail, Detail: "expected 9 domains",
	}
	n := len(tradeagent.Domains())
	if n == tradeDomainCount {
		check.Reachability = probe.ReachOK
		check.Detail = fmt.Sprintf("%d domains in tradeagent catalog", n)
		return check
	}
	check.Detail = fmt.Sprintf("got %d want %d", n, tradeDomainCount)
	return check
}

func (s *Service) checkMcpTradeScript() GateCheck {
	check := GateCheck{
		ID: "mcp-trade-script", Label: "mcp-server-trade script", Required: true,
		Reachability: probe.ReachFail, Detail: "mcp/trade/src/index.ts not found",
	}
	candidates := []string{}
	if s.configDir != "" {
		candidates = append(candidates, filepath.Join(s.configDir, "..", "mcp", "trade", "src", "index.ts"))
	}
	candidates = append(candidates, filepath.Join("mcp", "trade", "src", "index.ts"))
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "mcp-server-trade") && strings.Contains(string(data), "read") {
			check.Reachability = probe.ReachOK
			check.Detail = filepath.Base(abs) + " (" + tradeagent.ServerName + ")"
			return check
		}
	}
	return check
}
