package patrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultAgentAPIBase = "https://api.cursor.com"

type cursorCreateRequest struct {
	Prompt cursorPrompt `json:"prompt"`
	Name   string       `json:"name,omitempty"`
	Model  *cursorModel `json:"model,omitempty"`
	Repos  []cursorRepo `json:"repos,omitempty"`
}

type cursorPrompt struct {
	Text string `json:"text"`
}

type cursorModel struct {
	ID string `json:"id"`
}

type cursorRepo struct {
	URL         string `json:"url"`
	StartingRef string `json:"startingRef,omitempty"`
}

type cursorCreateResponse struct {
	Agent struct {
		ID          string `json:"id"`
		LatestRunID string `json:"latestRunId"`
	} `json:"agent"`
	Run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"run"`
}

type cursorRunResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result string `json:"result"`
}

// progressFn publishes incremental evidence while a run is in flight (UI polls running rows).
type progressFn func(evidence string)

func emitProgress(fn progressFn, evidence string) {
	if fn == nil || strings.TrimSpace(evidence) == "" {
		return
	}
	fn(evidence)
}

type dispatcher interface {
	Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, prompt string, progress progressFn) dispatchOutcome
}

type stubDispatcher struct{}

func (stubDispatcher) Dispatch(_ context.Context, skill PatrolSkill, trigger Trigger, _ string, _ progressFn) dispatchOutcome {
	return dispatchOutcome{
		Result:   ResultSuccess,
		Status:   StatusCompleted,
		Evidence: fmt.Sprintf("stub: patrol skill %s (%s) trigger=%s via PATROL_DISPATCH=stub", skill.ID, skill.TrustLevel, trigger),
	}
}

type cursorDispatcher struct {
	client *http.Client
}

func (d *cursorDispatcher) Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, prompt string, progress progressFn) dispatchOutcome {
	key := strings.TrimSpace(os.Getenv("CURSOR_API_KEY"))
	if key == "" {
		return dispatchOutcome{
			Result: ResultSkipped,
			Status: StatusCompleted,
			Error:  "CURSOR_API_KEY not configured; patrol dispatch skipped",
		}
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("CURSOR_AGENT_API_BASE")), "/")
	if base == "" {
		base = defaultAgentAPIBase
	}
	timeout := skill.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := cursorCreateRequest{
		Prompt: cursorPrompt{Text: prompt},
		Name:   "patrol-" + skill.ID,
	}
	if model := strings.TrimSpace(os.Getenv("PATROL_CURSOR_MODEL")); model != "" {
		body.Model = &cursorModel{ID: model}
	}
	if repo := strings.TrimSpace(os.Getenv("PATROL_CURSOR_REPO")); repo != "" {
		ref := strings.TrimSpace(os.Getenv("PATROL_CURSOR_REF"))
		body.Repos = []cursorRepo{{URL: repo, StartingRef: ref}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/agents", bytes.NewReader(raw))
	if err != nil {
		return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := d.client.Do(req)
	if err != nil {
		return dispatchOutcome{
			Result: ResultFailure,
			Status: StatusCompleted,
			Error:  "cursor agent API request failed: " + err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dispatchOutcome{
			Result: ResultFailure,
			Status: StatusCompleted,
			Error:  fmt.Sprintf("cursor agent API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		}
	}
	var created cursorCreateResponse
	if err := json.Unmarshal(respBody, &created); err != nil {
		return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: "decode create agent: " + err.Error()}
	}
	agentID := created.Agent.ID
	runID := created.Run.ID
	if runID == "" {
		runID = created.Agent.LatestRunID
	}
	if agentID == "" {
		return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: "cursor agent API returned empty agent id"}
	}
	emitProgress(progress, fmt.Sprintf("cursor agent %s run %s — polling (Cloud Agent; Platform MCP is stdio/LAN and usually unreachable from Cursor Cloud)\n", agentID, runID))
	if runID == "" {
		return dispatchOutcome{
			Result:   ResultSuccess,
			Status:   StatusStarted,
			Evidence: fmt.Sprintf("cursor agent %s started (trigger=%s); run id missing from create response", agentID, trigger),
		}
	}
	outcome := d.pollRun(ctx, base, key, agentID, runID)
	if outcome.Evidence == "" && outcome.Error == "" {
		outcome.Evidence = fmt.Sprintf("cursor agent %s run %s", agentID, runID)
	}
	return outcome
}

func (d *cursorDispatcher) pollRun(ctx context.Context, base, key, agentID, runID string) dispatchOutcome {
	url := fmt.Sprintf("%s/v1/agents/%s/runs/%s", base, agentID, runID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: err.Error()}
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := d.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return dispatchOutcome{
					Result: ResultFailure,
					Status: StatusStarted,
					Error:  "timed out waiting for cursor agent run",
				}
			}
			return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: err.Error()}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var run cursorRunResponse
			if err := json.Unmarshal(body, &run); err == nil {
				switch strings.ToUpper(run.Status) {
				case "FINISHED":
					evidence := strings.TrimSpace(run.Result)
					result, errMsg := classifyPatrolEvidence(evidence)
					return dispatchOutcome{
						Result:   result,
						Status:   StatusCompleted,
						Evidence: evidence,
						Error:    errMsg,
					}
				case "ERROR", "EXPIRED":
					msg := strings.TrimSpace(run.Result)
					if msg == "" {
						msg = "cursor agent run " + strings.ToLower(run.Status)
					}
					return dispatchOutcome{Result: ResultFailure, Status: StatusCompleted, Error: msg}
				case "CANCELLED":
					return dispatchOutcome{Result: ResultSkipped, Status: StatusCompleted, Error: "cursor agent run cancelled"}
				}
			}
		}
		select {
		case <-ctx.Done():
			return dispatchOutcome{
				Result: ResultFailure,
				Status: StatusStarted,
				Error:  "timed out waiting for cursor agent run",
			}
		case <-ticker.C:
		}
	}
}

// classifyPatrolEvidence maps a finished Cursor agent report to patrol work outcome.
// FINISHED only means the agent stopped writing — HTTP_FAIL / 扫描失败 is a failed scan.
func classifyPatrolEvidence(evidence string) (RunResult, string) {
	if strings.TrimSpace(evidence) == "" {
		return ResultSuccess, ""
	}
	upper := strings.ToUpper(evidence)
	lower := strings.ToLower(evidence)
	if strings.Contains(evidence, "工具层不可用") ||
		strings.Contains(lower, "tool layer unavailable") ||
		strings.Contains(evidence, "GetMcpTools") {
		return ResultFailure, "patrol verdict HTTP_FAIL (tool layer unavailable)"
	}
	if strings.Contains(upper, "HTTP_FAIL") && (strings.Contains(upper, "UNAVAILABLE") || strings.Contains(lower, "mcp")) {
		return ResultFailure, "patrol verdict HTTP_FAIL (tool layer unavailable)"
	}
	if strings.Contains(evidence, "扫描失败") ||
		strings.Contains(lower, "drift scan failed") ||
		strings.Contains(lower, "scan failed") {
		return ResultFailure, "patrol report marked scan failed"
	}
	return ResultSuccess, ""
}

func resolveDispatcher() dispatcher {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PATROL_DISPATCH")))
	switch mode {
	case "stub", "test":
		return stubDispatcher{}
	case "cursor":
		// Force Cursor Cloud for every skill (LAN Platform MCP usually unreachable).
		return &cursorDispatcher{client: &http.Client{}}
	case "local":
		return newLocalDispatcher()
	default:
		// live / unset: L0 read skills probe platform-api in-process (same GET routes as MCP).
		// L1+ still uses Cursor Cloud when a key is configured.
		return hybridDispatcher{
			local: newLocalDispatcher(),
			cloud: &cursorDispatcher{client: &http.Client{}},
		}
	}
}

type hybridDispatcher struct {
	local dispatcher
	cloud dispatcher
}

func (h hybridDispatcher) Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, prompt string, progress progressFn) dispatchOutcome {
	// L0 read-only: always local.
	// L1 with confirm actuation (deterministic cleanup): local.
	// L1 with escalate / L2: Cursor Cloud for AI judgment.
	if skill.TrustLevel == TrustL0 {
		return h.local.Dispatch(ctx, skill, trigger, prompt, progress)
	}
	if skill.TrustLevel == TrustL1 && skill.CronActuation == CronActuationConfirm {
		return h.local.Dispatch(ctx, skill, trigger, prompt, progress)
	}
	return h.cloud.Dispatch(ctx, skill, trigger, prompt, progress)
}

func buildPrompt(skill PatrolSkill, trigger Trigger, now time.Time) string {
	var b strings.Builder
	b.WriteString(skill.PromptTemplate)
	b.WriteString("\n\n---\nPatrol context\n")
	fmt.Fprintf(&b, "skill_id: %s\n", skill.ID)
	fmt.Fprintf(&b, "trust_level: %s\n", skill.TrustLevel)
	fmt.Fprintf(&b, "scope: %s\n", skill.Scope)
	fmt.Fprintf(&b, "trigger: %s\n", trigger)
	fmt.Fprintf(&b, "now_utc: %s\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "mcp_tools: %s\n", strings.Join(skill.MCPTools, ", "))
	if trigger == TriggerManual && skill.TrustLevel == TrustL1 {
		b.WriteString("owner_confirm: true (manual trigger)\n")
	}
	b.WriteString("Do not place live trading orders. D10 freeze.\n")
	return b.String()
}
