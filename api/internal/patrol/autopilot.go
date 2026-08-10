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

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
)

// autopilotDispatcher queries checklist signals and fixes red items directly
// through platform-api routes. It treats both full_auto and semi_auto items
// identically (act-then-report), skipping observe/manual items.
type autopilotDispatcher struct {
	client   *http.Client
	base     string
	token    string
	throttle *RestartThrottle
}

type fixResult struct {
	ItemID  string
	Action  string
	Status  int
	Ok      bool
	Detail  string
	Skipped bool
}

// safe boundary: items whose fix would violate hard-coded guardrails.
var unsafeItemIDs = map[string]string{
	"ib-feed": "D10: IB feed actuation blocked",
}

func (a *autopilotDispatcher) Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, _ string, progress progressFn) dispatchOutcome {
	timeout := skill.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var b strings.Builder
	fmt.Fprintf(&b, "## Bifrost Ops Autopilot\n")
	fmt.Fprintf(&b, "trigger: %s · trust: %s\n\n", trigger, skill.TrustLevel)
	emitProgress(progress, b.String())

	// Phase 1: Fetch checklist signals via GET /api/v1/checklist/signals
	signals, err := a.fetchSignals(ctx)
	if err != nil {
		fmt.Fprintf(&b, "### Checklist Signals\nERROR: %s\n", err)
		return dispatchOutcome{
			Result:   ResultFailure,
			Status:   StatusCompleted,
			Evidence: b.String(),
			Error:    "failed to fetch checklist signals: " + err.Error(),
		}
	}
	signals = a.mergeBackupSignal(ctx, signals)
	fmt.Fprintf(&b, "### Checklist Signals\nfetched %d signal(s)\n\n", len(signals))
	emitProgress(progress, b.String())

	// Phase 2: Classify red items
	var reds []checklist.ItemSignal
	var okN int
	for _, sig := range signals {
		if sig.Signal == checklist.SignalFail || sig.Signal == checklist.SignalDegraded {
			reds = append(reds, sig)
		} else {
			okN++
		}
	}
	if len(reds) == 0 {
		fmt.Fprintf(&b, "### Verdict\n**ALL_OK** · %d items ok · 0 red\n", okN)
		return dispatchOutcome{
			Result:   ResultSuccess,
			Status:   StatusCompleted,
			Evidence: b.String(),
		}
	}
	fmt.Fprintf(&b, "red items: %d · ok: %d\n\n", len(reds), okN)
	emitProgress(progress, b.String())

	// Phase 3: Fix each red item
	var results []fixResult
	var selfRestartItems []checklist.ItemSignal // platform-api/console execute last
	for _, sig := range reds {
		if isSelfRestart(sig.ItemID) {
			selfRestartItems = append(selfRestartItems, sig)
			continue
		}
		r := a.fixItem(ctx, sig, &b, progress)
		results = append(results, r)
	}

	// Phase 4: Write consolidated report BEFORE self-restart
	fixedN, failedN, skippedN := tallyResults(results)
	b.WriteString("### Remediation Summary (pre-self-restart)\n")
	fmt.Fprintf(&b, "fixed: %d · failed: %d · skipped: %d\n\n", fixedN, failedN, skippedN)
	emitProgress(progress, b.String())

	// Phase 5: Self-restart items (platform-api, platform-console) execute LAST
	for _, sig := range selfRestartItems {
		r := a.fixItem(ctx, sig, &b, progress)
		results = append(results, r)
	}

	// Final tally
	fixedN, failedN, skippedN = tallyResults(results)
	overallResult := ResultSuccess
	errMsg := ""
	if failedN > 0 {
		errMsg = fmt.Sprintf("%d fix(es) failed", failedN)
	}

	fmt.Fprintf(&b, "### Verdict\n**AUTOPILOT_DONE** · %d fixed · %d failed · %d skipped · %d ok\n",
		fixedN, failedN, skippedN, okN)
	emitProgress(progress, b.String())

	return dispatchOutcome{
		Result:   overallResult,
		Status:   StatusCompleted,
		Evidence: b.String(),
		Error:    errMsg,
	}
}

func (a *autopilotDispatcher) fixItem(ctx context.Context, sig checklist.ItemSignal, b *strings.Builder, progress progressFn) fixResult {
	meta, ok := checklist.ItemByID(sig.ItemID)
	if !ok {
		fmt.Fprintf(b, "### %s\nskipped: unknown catalog item\n\n", sig.ItemID)
		emitProgress(progress, b.String())
		return fixResult{ItemID: sig.ItemID, Skipped: true, Detail: "unknown catalog item"}
	}

	// Safe boundary check
	if reason, blocked := unsafeItemIDs[meta.ID]; blocked {
		fmt.Fprintf(b, "### %s (%s)\nBLOCKED: %s\n\n", meta.ID, meta.Label, reason)
		emitProgress(progress, b.String())
		return fixResult{ItemID: meta.ID, Skipped: true, Detail: reason}
	}

	// Only fix full_auto and semi_auto
	if meta.FixCapability != checklist.FixFullAuto && meta.FixCapability != checklist.FixSemiAuto {
		fmt.Fprintf(b, "### %s (%s)\nskipped: capability=%s (observe/manual)\n\n", meta.ID, meta.Label, meta.FixCapability)
		emitProgress(progress, b.String())
		return fixResult{ItemID: meta.ID, Skipped: true, Detail: "observe/manual — skip"}
	}

	// Throttle check (restarts only — backup/WAL repair must be retryable)
	if shouldThrottleFix(meta.ID) && a.throttle != nil && !a.throttle.CanRestart(meta.ID) {
		fmt.Fprintf(b, "### %s (%s)\nTHROTTLED: restarted within 24h\n\n", meta.ID, meta.Label)
		emitProgress(progress, b.String())
		return fixResult{ItemID: meta.ID, Skipped: true, Detail: "throttled: 24h cooldown"}
	}

	fmt.Fprintf(b, "### %s (%s)\nsignal=%s · cap=%s\n", meta.ID, meta.Label, sig.Signal, meta.FixCapability)
	emitProgress(progress, b.String())

	action, status, err := a.executeFixRoute(ctx, meta, sig)
	if action == "observe-only" {
		detail := "observe-only"
		if err != nil {
			detail = err.Error()
		}
		fmt.Fprintf(b, "OBSERVE-ONLY: skipped — %s\n\n", detail)
		emitProgress(progress, b.String())
		return fixResult{ItemID: meta.ID, Action: action, Skipped: true, Detail: detail}
	}
	if err != nil {
		fmt.Fprintf(b, "FIX ERROR: %s\n\n", err)
		emitProgress(progress, b.String())
		return fixResult{ItemID: meta.ID, Action: action, Ok: false, Detail: err.Error()}
	}

	ok = status >= 200 && status < 300
	if ok && shouldThrottleFix(meta.ID) && a.throttle != nil {
		a.throttle.RecordRestart(meta.ID)
	}

	marker := "✗"
	if ok {
		marker = "✓"
	}
	fmt.Fprintf(b, "%s HTTP %d %s\n\n", action, status, marker)
	emitProgress(progress, b.String())
	return fixResult{ItemID: meta.ID, Action: action, Status: status, Ok: ok, Detail: fmt.Sprintf("HTTP %d", status)}
}

// executeFixRoute picks the right platform-api route for the item.
func (a *autopilotDispatcher) executeFixRoute(ctx context.Context, meta checklist.ItemMeta, sig checklist.ItemSignal) (action string, status int, err error) {
	switch meta.ID {
	case "failing-pods":
		return a.fixFailingPods(ctx, sig)
	case "platform-api":
		return a.selfRestart(ctx, "platform-api")
	case "platform-console":
		return a.selfRestart(ctx, "platform-console")
	case "git-bridge":
		if isInCluster() {
			return a.rolloutRestart(ctx, resolvePlatformNamespace(), "git-bridge")
		}
		return a.restartDevSession(ctx, "git-bridge")
	case "nodes-ready":
		return a.fixNodesReady(ctx, sig)
	case "argo-apps":
		return a.fixArgoApps(ctx, sig)
	case "redis":
		return a.rolloutRestart(ctx, resolveTradeNamespace(), "redis")
	case "nginx-edge":
		return a.rolloutRestart(ctx, resolveTradeNamespace(), "nginx")
	case "trade-apis":
		return a.fixTradeAPIs(ctx, sig)
	case "cluster-api":
		return "observe-only", 0, fmt.Errorf("cluster-api fix requires manual investigation")
	case "postgres":
		return "observe-only", 0, fmt.Errorf("postgres is CNPG-managed — rollout-restart is not valid; escalate to operator")
	case "db-backup-fresh":
		return a.repairCnpgWalStore(ctx)
	case "deliver-pipeline":
		return "observe-only", 0, fmt.Errorf("deliver-pipeline fix not automatable in Wave 1")
	case "stg-smoke":
		return "observe-only", 0, fmt.Errorf("stg-smoke fix requires investigation")
	case "massive-polygon":
		return a.rolloutRestart(ctx, resolveMarketDataNamespace(), "polygon-worker-stocks")
	case "runners-ha":
		return "observe-only", 0, fmt.Errorf("runners-ha fix requires manual runner restart")
	case "hermes-tooling":
		return "observe-only", 0, fmt.Errorf("hermes-tooling fix requires manual investigation")
	default:
		return "skip", 0, fmt.Errorf("no autopilot fix mapped for %s", meta.ID)
	}
}

// selfRestart restarts platform-api or platform-console. In-cluster mode uses
// K8s rollout-restart; local mode uses the bdev dev-sessions restart endpoint.
// Self-restart is inherently fire-and-forget: the API process may terminate
// before the HTTP response arrives. We accept a best-effort status.
func (a *autopilotDispatcher) selfRestart(ctx context.Context, component string) (string, int, error) {
	if isInCluster() {
		return a.rolloutRestart(ctx, resolvePlatformNamespace(), component)
	}
	return a.restartDevSession(ctx, component)
}

// isInCluster returns true when the process runs inside a Kubernetes pod.
func isInCluster() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

func viewerEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPS_VIEWER_ENV"))) {
	case "stg":
		return "stg"
	case "prod":
		return "prod"
	default:
		return "dev"
	}
}

// resolveTradeNamespace maps viewer seat → Trade stack NS (overlays/dev|stg|prod).
func resolveTradeNamespace() string {
	switch viewerEnv() {
	case "stg":
		return "bifrost-stg"
	case "prod":
		return "bifrost-prod"
	default:
		return "bifrost-dev"
	}
}

// resolvePlatformNamespace maps viewer seat → platform-api/console NS.
// There is no bifrost-platform-dev in-cluster overlay; local/dev uses bdev.
func resolvePlatformNamespace() string {
	if viewerEnv() == "prod" {
		return "bifrost-platform-prod"
	}
	return "bifrost-platform-stg"
}

// resolveMarketDataNamespace maps viewer seat → plugin-market-data overlay NS.
func resolveMarketDataNamespace() string {
	switch viewerEnv() {
	case "stg":
		return "plugin-market-data-stg"
	case "prod":
		return "plugin-market-data-prod"
	default:
		return "plugin-market-data"
	}
}

func (a *autopilotDispatcher) fixFailingPods(ctx context.Context, sig checklist.ItemSignal) (string, int, error) {
	raw, err := a.get(ctx, "/api/v1/cluster/")
	if err != nil {
		return "get_cluster_summary", 0, err
	}
	pods := extractTerminalPods(raw)
	if len(pods) == 0 {
		return "delete_pod(none)", 200, nil
	}
	var lastStatus int
	deleted := 0
	for _, p := range pods {
		if !isSafeToDelete(p) {
			continue
		}
		route := fmt.Sprintf("/api/v1/cluster/workloads/pods/%s/%s", p.Namespace, p.Name)
		status, delErr := a.doRequest(ctx, http.MethodDelete, route, nil)
		if delErr != nil {
			return "delete_pod", 0, delErr
		}
		lastStatus = status
		if status >= 200 && status < 300 {
			deleted++
		}
	}
	if deleted > 0 {
		return fmt.Sprintf("delete_pod(%d)", deleted), lastStatus, nil
	}
	return "delete_pod(0)", 200, nil
}

func (a *autopilotDispatcher) rolloutRestart(ctx context.Context, namespace, name string) (string, int, error) {
	body := map[string]string{
		"namespace": namespace,
		"kind":      "Deployment",
		"name":      name,
	}
	raw, _ := json.Marshal(body)
	status, err := a.doRequest(ctx, http.MethodPost, "/api/v1/cluster/workloads/rollout-restart", raw)
	action := fmt.Sprintf("rollout_restart(%s/%s)", namespace, name)
	return action, status, err
}

func (a *autopilotDispatcher) triggerCnpgBackup(ctx context.Context) (string, int, error) {
	status, err := a.doRequest(ctx, http.MethodPost, "/api/v1/cluster/postgres/backup", []byte(`{}`))
	return "trigger_cnpg_backup", status, err
}

func (a *autopilotDispatcher) repairCnpgWalStore(ctx context.Context) (string, int, error) {
	status, err := a.doRequest(ctx, http.MethodPost, "/api/v1/cluster/postgres/wal-store/repair", []byte(`{}`))
	return "repair_cnpg_wal_store", status, err
}

func shouldThrottleFix(itemID string) bool {
	return itemID != "db-backup-fresh"
}

func (a *autopilotDispatcher) restartDevSession(ctx context.Context, name string) (string, int, error) {
	body := map[string]string{"action": "restart"}
	raw, _ := json.Marshal(body)
	route := fmt.Sprintf("/api/v1/dev-sessions/%s/control", name)
	status, err := a.doRequest(ctx, http.MethodPost, route, raw)
	return fmt.Sprintf("restart_dev_session(%s)", name), status, err
}

func (a *autopilotDispatcher) fixNodesReady(ctx context.Context, sig checklist.ItemSignal) (string, int, error) {
	nodeName := extractNodeName(sig.Detail)
	if nodeName == "" {
		return "uncordon_node", 0, fmt.Errorf("cannot extract node name from detail: %s", truncateRunes(sig.Detail, 120))
	}
	route := fmt.Sprintf("/api/v1/cluster/nodes/%s/uncordon", nodeName)
	status, err := a.doRequest(ctx, http.MethodPost, route, nil)
	return fmt.Sprintf("uncordon_node(%s)", nodeName), status, err
}

func (a *autopilotDispatcher) fixArgoApps(ctx context.Context, sig checklist.ItemSignal) (string, int, error) {
	appName := extractArgoApp(sig.Detail)
	if appName == "" {
		return "gitops_sync_app", 0, fmt.Errorf("cannot extract app name from detail: %s", truncateRunes(sig.Detail, 120))
	}
	route := fmt.Sprintf("/api/v1/gitops/apps/%s/sync", appName)
	status, err := a.doRequest(ctx, http.MethodPost, route, nil)
	return fmt.Sprintf("gitops_sync_app(%s)", appName), status, err
}

func (a *autopilotDispatcher) fixTradeAPIs(ctx context.Context, _ checklist.ItemSignal) (string, int, error) {
	// Trade APIs are 9 domains behind nginx; restart the nginx edge as first pass
	return a.rolloutRestart(ctx, resolveTradeNamespace(), "nginx")
}

// --- HTTP helpers ---

func (a *autopilotDispatcher) get(ctx context.Context, route string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+route, nil)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, 128<<10))
}

func (a *autopilotDispatcher) doRequest(ctx context.Context, method, route string, body []byte) (int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+route, reader)
	if err != nil {
		return 0, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// --- Signal parsing helpers ---

func (a *autopilotDispatcher) fetchSignals(ctx context.Context) ([]checklist.ItemSignal, error) {
	raw, err := a.get(ctx, "/api/v1/checklist/signals")
	if err != nil {
		return nil, err
	}
	var resp checklist.SignalsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse signals: %w", err)
	}
	return resp.Signals, nil
}

type backupStatusDTO struct {
	Fresh  bool   `json:"fresh"`
	Signal string `json:"signal"`
	Detail string `json:"detail"`
}

// mergeBackupSignal overlays live CNPG backup freshness so Autopilot can act
// even when the checklist store has not yet recorded db-backup-fresh.
func (a *autopilotDispatcher) mergeBackupSignal(ctx context.Context, signals []checklist.ItemSignal) []checklist.ItemSignal {
	raw, err := a.get(ctx, "/api/v1/cluster/postgres/backup-status")
	if err != nil {
		return signals
	}
	var status backupStatusDTO
	if err := json.Unmarshal(raw, &status); err != nil {
		return signals
	}
	sig := strings.TrimSpace(status.Signal)
	if sig == "" && status.Detail == "" && !status.Fresh {
		return signals
	}
	if sig == "" {
		if status.Fresh {
			sig = checklist.SignalOK
		} else {
			sig = checklist.SignalFail
		}
	}
	item := checklist.ItemSignal{
		ItemID: "db-backup-fresh",
		Signal: sig,
		Detail: status.Detail,
	}
	out := make([]checklist.ItemSignal, 0, len(signals)+1)
	replaced := false
	for _, s := range signals {
		if s.ItemID == "db-backup-fresh" {
			out = append(out, item)
			replaced = true
			continue
		}
		out = append(out, s)
	}
	if !replaced {
		out = append(out, item)
	}
	return out
}

func isSelfRestart(itemID string) bool {
	return itemID == "platform-api" || itemID == "platform-console"
}

func tallyResults(results []fixResult) (fixed, failed, skipped int) {
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Ok:
			fixed++
		default:
			failed++
		}
	}
	return
}

// extractNodeName tries to pull a Kubernetes node name from signal detail text.
// Typical detail: "node k3s-worker-02 NotReady" or "k3s-worker-02: NotReady".
func extractNodeName(detail string) string {
	detail = strings.TrimSpace(detail)
	lower := strings.ToLower(detail)
	if idx := strings.Index(lower, "node "); idx >= 0 {
		rest := detail[idx+5:]
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	if idx := strings.Index(detail, ":"); idx > 0 {
		candidate := strings.TrimSpace(detail[:idx])
		if !strings.Contains(candidate, " ") && len(candidate) > 2 {
			return candidate
		}
	}
	fields := strings.Fields(detail)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// extractArgoApp extracts an Argo CD app name from signal detail text.
// Typical detail: "app bifrost-dev OutOfSync" or "bifrost-dev: Degraded".
func extractArgoApp(detail string) string {
	detail = strings.TrimSpace(detail)
	lower := strings.ToLower(detail)
	if idx := strings.Index(lower, "app "); idx >= 0 {
		rest := detail[idx+4:]
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	if idx := strings.Index(detail, ":"); idx > 0 {
		candidate := strings.TrimSpace(detail[:idx])
		if !strings.Contains(candidate, " ") && len(candidate) > 2 {
			return candidate
		}
	}
	fields := strings.Fields(detail)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}
