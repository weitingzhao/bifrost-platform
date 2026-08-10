package patrol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
)

const maxProbeSnippetRunes = 900

// localDispatcher runs patrol skills by calling platform-api routes directly.
// L0 skills: read-only GET probes.
// L1 + cron_actuation=confirm: read probes + deterministic terminal-pod cleanup.
type localDispatcher struct {
	client *http.Client
	base   string
	token  string
}

func newLocalDispatcher() *localDispatcher {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("PLATFORM_API_URL")), "/")
	if base == "" {
		base = "http://127.0.0.1:8780"
	}
	return &localDispatcher{
		client: &http.Client{},
		base:   base,
		token:  strings.TrimSpace(os.Getenv("PLATFORM_OPERATOR_TOKEN")),
	}
}

func (d *localDispatcher) Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, _ string, progress progressFn) dispatchOutcome {
	// Delegate ops-autopilot to dedicated autopilot dispatcher.
	if skill.ID == "ops-autopilot" {
		ap := d.autopilot()
		return ap.Dispatch(ctx, skill, trigger, "", progress)
	}

	timeout := skill.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	routes := catalogByName()
	var b strings.Builder
	fmt.Fprintf(&b, "## Bifrost Patrol — %s\n", skill.Name)
	b.WriteString("runtime: platform-api local probe (same GET routes as Platform MCP)\n")
	fmt.Fprintf(&b, "trigger: %s · trust: %s\n\n", trigger, skill.TrustLevel)
	emitProgress(progress, b.String())

	canActuate := skill.TrustLevel == TrustL1 && skill.CronActuation == CronActuationConfirm
	hasDeletePod := toolListContains(skill.MCPTools, "delete_pod")

	// Phase 1: Read probes
	okN, failN := 0, 0
	var hints []string
	var findings []string
	var clusterRaw []byte // retain for Phase 2 pod extraction
	for _, name := range skill.MCPTools {
		tool, exists := routes[name]
		if !exists {
			failN++
			fmt.Fprintf(&b, "### %s\nskipped: not in MCP catalog\n\n", name)
			emitProgress(progress, b.String())
			continue
		}
		if !isReadTool(tool.Level, tool.Method) {
			continue // write tools handled in Phase 2
		}
		if strings.Contains(tool.Route, "{") {
			fmt.Fprintf(&b, "### %s\nskipped: parameterized route %s\n\n", name, tool.Route)
			emitProgress(progress, b.String())
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(tool.Method))
		if method == "" {
			method = http.MethodGet
		}
		fmt.Fprintf(&b, "### %s\n%s %s …\n", name, method, tool.Route)
		emitProgress(progress, b.String())

		status, snippet, raw, hint, notes, err := d.callRaw(ctx, method, tool.Route)
		if err != nil {
			failN++
			fmt.Fprintf(&b, "ERROR: %s\n\n", err)
			emitProgress(progress, b.String())
			continue
		}
		if name == "get_cluster_summary" {
			clusterRaw = raw
		}
		if status < 200 || status >= 300 {
			failN++
		} else {
			okN++
		}
		if hint != "" {
			hints = append(hints, hint)
		}
		findings = append(findings, notes...)
		if hint != "" {
			fmt.Fprintf(&b, "HTTP %d · signal %s\n%s\n\n", status, hint, snippet)
		} else {
			fmt.Fprintf(&b, "HTTP %d\n%s\n\n", status, snippet)
		}
		emitProgress(progress, b.String())
	}

	// Phase 2: Chain cleanup — delete terminal pods
	var cleaned, skippedClean int
	if canActuate && hasDeletePod && len(clusterRaw) > 0 {
		pods := extractTerminalPods(clusterRaw)
		if len(pods) > 0 {
			b.WriteString("### Chain Cleanup (L1 auto)\n")
			fmt.Fprintf(&b, "terminal pods found: %d\n", len(pods))
			emitProgress(progress, b.String())
			for _, p := range pods {
				if !isSafeToDelete(p) {
					skippedClean++
					fmt.Fprintf(&b, "- SKIP %s/%s — guardrail: %s\n", p.Namespace, p.Name, p.Phase)
					emitProgress(progress, b.String())
					continue
				}
				route := fmt.Sprintf("/api/v1/cluster/workloads/pods/%s/%s", p.Namespace, p.Name)
				fmt.Fprintf(&b, "- DELETE %s/%s (phase=%s) … ", p.Namespace, p.Name, p.Phase)
				emitProgress(progress, b.String())
				status, err := d.delete(ctx, route)
				if err != nil {
					fmt.Fprintf(&b, "ERROR %s\n", err)
				} else if status >= 200 && status < 300 {
					cleaned++
					fmt.Fprintf(&b, "HTTP %d ✓\n", status)
				} else {
					fmt.Fprintf(&b, "HTTP %d ✗\n", status)
				}
				emitProgress(progress, b.String())
			}
			fmt.Fprintf(&b, "cleanup: %d deleted · %d skipped\n\n", cleaned, skippedClean)
			emitProgress(progress, b.String())
		}
	}

	// Phase 3: Verdict
	verdict := "NOMINAL"
	result := ResultSuccess
	errMsg := ""
	switch {
	case okN == 0:
		verdict = "HTTP_FAIL"
		result = ResultFailure
		errMsg = "patrol local probe: no read tools succeeded"
	case failN > 0:
		verdict = "HTTP_FAIL"
		result = ResultFailure
		errMsg = fmt.Sprintf("patrol local probe: %d tool call(s) failed", failN)
	default:
		verdict = rollupProbeHints(hints)
	}
	if len(findings) > 0 {
		b.WriteString("### Compare\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "### Verdict\n**%s** · %d tools ok · %d failed", verdict, okN, failN)
	if cleaned > 0 {
		fmt.Fprintf(&b, " · %d pods cleaned", cleaned)
	}
	b.WriteByte('\n')
	if failN == 0 && verdict != "NOMINAL" && verdict != "" {
		fmt.Fprintf(&b, "scan executed; fleet signal is %s (Result=success means probes ran, not that the fleet is clean)\n", verdict)
	}
	emitProgress(progress, b.String())
	return dispatchOutcome{
		Result:   result,
		Status:   StatusCompleted,
		Evidence: b.String(),
		Error:    errMsg,
	}
}

// callRaw performs an HTTP request and returns both parsed hint/notes and raw body.
func (d *localDispatcher) autopilot() *autopilotDispatcher {
	throttle, _ := NewRestartThrottle("", nil)
	return &autopilotDispatcher{
		client:   d.client,
		base:     d.base,
		token:    d.token,
		throttle: throttle,
	}
}

func (d *localDispatcher) callRaw(ctx context.Context, method, route string) (int, string, []byte, string, []string, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base+route, nil)
	if err != nil {
		return 0, "", nil, "", nil, err
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, "", nil, "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	hint, notes := classifyProbeBody(raw)
	snippet := truncateRunes(strings.TrimSpace(string(raw)), maxProbeSnippetRunes)
	return resp.StatusCode, snippet, raw, hint, notes, nil
}

func (d *localDispatcher) delete(ctx context.Context, route string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, d.base+route, nil)
	if err != nil {
		return 0, err
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// --- Terminal pod extraction and guardrails ---

type stalePod struct {
	Namespace string
	Name      string
	Phase     string
	Reason    string
	Node      string
}

func extractTerminalPods(clusterJSON []byte) []stalePod {
	var summary struct {
		FailingPods    int `json:"failing_pods"`
		FailingDetails []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Phase     string `json:"phase"`
			Reason    string `json:"reason"`
			Node      string `json:"node"`
		} `json:"failing_pod_details"`
	}
	if err := json.Unmarshal(clusterJSON, &summary); err != nil {
		return nil
	}
	var out []stalePod
	for _, p := range summary.FailingDetails {
		phase := strings.ToLower(strings.TrimSpace(p.Phase))
		reason := strings.ToLower(p.Reason)
		if phase == "succeeded" || phase == "failed" || strings.Contains(reason, "evict") {
			out = append(out, stalePod{
				Namespace: p.Namespace,
				Name:      p.Name,
				Phase:     p.Phase,
				Reason:    p.Reason,
				Node:      p.Node,
			})
		}
	}
	return out
}

var protectedPodPrefixes = []string{
	"daemon", "gs-trading", "bifrost-daemon",
}

func isSafeToDelete(p stalePod) bool {
	phase := strings.ToLower(strings.TrimSpace(p.Phase))
	reason := strings.ToLower(p.Reason)
	switch phase {
	case "succeeded", "failed":
		// ok
	default:
		if !strings.Contains(reason, "evict") {
			return false
		}
	}
	nameLower := strings.ToLower(p.Name)
	for _, prefix := range protectedPodPrefixes {
		if strings.HasPrefix(nameLower, prefix) {
			return false
		}
	}
	return true
}

func toolListContains(tools []string, name string) bool {
	for _, t := range tools {
		if t == name {
			return true
		}
	}
	return false
}

func catalogByName() map[string]mcp.ToolView {
	out := map[string]mcp.ToolView{}
	for _, t := range mcp.Catalog() {
		out[t.Name] = t
	}
	return out
}

func missionSignalHint(sig string) string {
	switch strings.ToLower(strings.TrimSpace(sig)) {
	case "ok", "nominal":
		return "NOMINAL"
	case "degraded", "probe_drift":
		return "PROBE_DRIFT"
	case "fail", "failed", "data_layer":
		return "DATA_LAYER"
	case "http_fail", "unknown":
		if strings.EqualFold(sig, "unknown") {
			return "UNKNOWN"
		}
		return "HTTP_FAIL"
	default:
		return strings.ToUpper(strings.TrimSpace(sig))
	}
}

// classifyProbeBody extracts a patrol taxonomy hint plus human compare notes.
func classifyProbeBody(body []byte) (hint string, notes []string) {
	var probe struct {
		PayloadOverall string `json:"payload_overall"`
		Summary        struct {
			Overall string `json:"overall"`
		} `json:"summary"`
		PayloadVerification struct {
			Summary struct {
				Overall string `json:"overall"`
			} `json:"summary"`
		} `json:"payload_verification"`
		Reachability    string `json:"reachability"`
		APIReachability string `json:"api_reachability"`
		FailingPods     int    `json:"failing_pods"`
		FailingDetails  []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Reason    string `json:"reason"`
		} `json:"failing_pod_details"`
		Matrices []struct {
			Environment string `json:"environment"`
			Targets     []struct {
				ID            string `json:"id"`
				Reachability  string `json:"reachability"`
			} `json:"targets"`
		} `json:"matrices"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", nil
	}

	if s := strings.TrimSpace(probe.PayloadVerification.Summary.Overall); s != "" {
		hint = strings.ToUpper(s)
		notes = append(notes, "mission/payload: "+hint)
	} else if s := strings.TrimSpace(probe.Summary.Overall); s != "" {
		hint = strings.ToUpper(s)
		notes = append(notes, "payload summary: "+hint)
	} else if s := strings.TrimSpace(probe.PayloadOverall); s != "" {
		hint = missionSignalHint(s)
		notes = append(notes, fmt.Sprintf("mission payload_overall=%s → %s", s, hint))
	}

	if len(probe.Matrices) > 0 {
		failN := 0
		var failedIDs []string
		for _, m := range probe.Matrices {
			for _, t := range m.Targets {
				r := strings.ToLower(strings.TrimSpace(t.Reachability))
				if r != "" && r != "ok" && r != "skipped" {
					failN++
					if len(failedIDs) < 6 {
						label := t.ID
						if m.Environment != "" {
							label = m.Environment + "/" + t.ID
						}
						failedIDs = append(failedIDs, label+"="+t.Reachability)
					}
				}
			}
		}
		if failN > 0 {
			hint = rollupProbeHints([]string{hint, "PROBE_DRIFT"})
			notes = append(notes, fmt.Sprintf("matrix: %d non-ok target(s) (%s)", failN, strings.Join(failedIDs, ", ")))
		} else {
			notes = append(notes, "matrix: all sampled targets ok")
		}
	}

	if probe.APIReachability != "" || probe.Reachability != "" || probe.FailingPods > 0 {
		api := strings.ToLower(strings.TrimSpace(probe.APIReachability))
		reach := strings.ToLower(strings.TrimSpace(probe.Reachability))
		switch {
		case api == "fail" || reach == "fail":
			hint = rollupProbeHints([]string{hint, "DATA_LAYER"})
		case reach == "degraded" || probe.FailingPods > 0:
			hint = rollupProbeHints([]string{hint, "PROBE_DRIFT"})
		case reach == "ok" && api == "ok":
			hint = rollupProbeHints([]string{hint, "NOMINAL"})
		}
		podNote := ""
		if len(probe.FailingDetails) > 0 {
			d := probe.FailingDetails[0]
			podNote = fmt.Sprintf(" · %s/%s %s", d.Namespace, d.Name, d.Reason)
		}
		notes = append(notes, fmt.Sprintf("cluster: api=%s reach=%s failing_pods=%d%s",
			nz(probe.APIReachability, "—"), nz(probe.Reachability, "—"), probe.FailingPods, podNote))
	}

	return hint, notes
}

func nz(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func rollupProbeHints(hints []string) string {
	rank := map[string]int{
		"NOMINAL":     0,
		"UNKNOWN":     1,
		"PROBE_DRIFT": 2,
		"DATA_LAYER":  3,
		"HTTP_FAIL":   4,
	}
	best, bestRank := "NOMINAL", -1
	for _, h := range hints {
		u := strings.ToUpper(strings.TrimSpace(h))
		r, ok := rank[u]
		if !ok {
			continue
		}
		if r > bestRank {
			best, bestRank = u, r
		}
	}
	return best
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
