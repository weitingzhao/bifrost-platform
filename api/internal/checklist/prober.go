package checklist

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The checklist prober fills the catalog from the platform's own read API, in
// process, on a timer (TD-253). Before it the signals came only from an LLM
// runner that stopped reporting on 09-08, so the patrol autopilot kept acting
// on observations weeks old. It probes and records; it never dispatches — what
// to do about a red item stays with the autopilot and its PATROL_MODE.
//
// Each rule reads one GET route the Console already shows. A route that cannot
// be read makes its items unknown, never ok.

const proberSource = "checklist-prober"

// ProberWanted reports whether CHECKLIST_PROBER=on. Off by default: only the
// PROD workers overlay turns it on, so maintenance signals have one producer.
func ProberWanted() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CHECKLIST_PROBER")), "on")
}

// ProberInterval reads CHECKLIST_PROBER_INTERVAL (Go duration), default 10m,
// floor 1m.
func ProberInterval() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(os.Getenv("CHECKLIST_PROBER_INTERVAL")))
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	if d < time.Minute {
		return time.Minute
	}
	return d
}

// Prober reads the platform API and maps responses to checklist signals.
type Prober struct {
	Base       string // the platform's own API, e.g. http://127.0.0.1:8780
	APIHealth  string // platform-api /health as seen from this pod
	ConsoleURL string
	Env        string // matrix environment the Trade items describe
	Client     *http.Client
}

// NewProberFromEnv builds a Prober for the in-cluster workers pod.
func NewProberFromEnv() *Prober {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("PLATFORM_API_URL")), "/")
	if base == "" {
		base = "http://127.0.0.1:8780"
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("OPS_VIEWER_ENV")))
	if env == "" {
		env = "dev"
	}
	return &Prober{
		Base:       base,
		APIHealth:  envOr("CHECKLIST_PROBER_API_HEALTH_URL", "http://platform-api:8780/health"),
		ConsoleURL: envOr("CHECKLIST_PROBER_CONSOLE_URL", "http://platform-console/"),
		Env:        env,
		Client:     &http.Client{Timeout: 45 * time.Second},
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// StartProber probes now and then every interval until ctx ends.
func (h *Handler) StartProber(ctx context.Context, p *Prober, interval time.Duration) {
	slog.Info("checklist prober on", "interval", interval.String(), "base", p.Base, "env", p.Env)
	safego.Go("checklist.prober", func() {
		// The prober starts before the listener it reads; a first run against a
		// closed port reports unknown for everything it asked.
		p.waitReady(ctx, 2*time.Minute)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			// A panic in one run is contained and the next tick runs again.
			safego.Do("checklist.prober.run", func() { h.probeAndMerge(ctx, p) })
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
}

func (h *Handler) probeAndMerge(ctx context.Context, p *Prober) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	sigs := p.Probe(ctx)
	runID := "prober-" + time.Now().UTC().Format("20060102T150405Z")
	if _, err := h.store.Merge(MergeRequest{RunID: runID, Source: proberSource, Signals: sigs}); err != nil {
		slog.Warn("checklist prober merge failed", "err", err)
		maintainer.Failure(maintainer.PlatformID(maintainer.LoopChecklistProber))
		return
	}
	bad := []string{}
	for _, s := range sigs {
		if s.Signal == SignalFail || s.Signal == SignalDegraded {
			bad = append(bad, s.ItemID)
		}
	}
	slog.Info("checklist prober run", "run_id", runID, "items", len(sigs), "red", strings.Join(bad, ","))
	maintainer.Success(maintainer.PlatformID(maintainer.LoopChecklistProber))
}

// Probe returns one signal per catalog item it covers. The data-husbandry items
// (market-batch-sla, flex-tokens-secret, research-batch-sla) are left out: GET
// /checklist/signals overlays them live on every read.
func (p *Prober) Probe(ctx context.Context) []ItemSignal {
	out := []ItemSignal{}
	add := func(id, signal, detail string) {
		out = append(out, ItemSignal{ItemID: id, Signal: signal, Detail: truncate(detail, 240), Env: p.Env, Source: proberSource})
	}

	// cluster-api · nodes-ready · failing-pods
	var cl struct {
		APIReachability string `json:"api_reachability"`
		Detail          string `json:"detail"`
		NodesReady      int    `json:"nodes_ready"`
		NodesTotal      int    `json:"nodes_total"`
		FailingPods     int    `json:"failing_pods"`
		FailingDetails  []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Reason    string `json:"reason"`
		} `json:"failing_pod_details"`
	}
	if err := p.getJSON(ctx, "/api/v1/cluster", &cl); err != nil {
		for _, id := range []string{"cluster-api", "nodes-ready", "failing-pods"} {
			add(id, SignalUnknown, "GET /api/v1/cluster: "+err.Error())
		}
	} else {
		add("cluster-api", okIf(cl.APIReachability == "ok", SignalFail), "api_reachability="+cl.APIReachability)
		nodes := fmt.Sprintf("%d/%d nodes ready", cl.NodesReady, cl.NodesTotal)
		if cl.Detail != "" {
			nodes += " · " + cl.Detail
		}
		add("nodes-ready", okIf(cl.NodesTotal > 0 && cl.NodesReady == cl.NodesTotal, SignalDegraded), nodes)
		pods := fmt.Sprintf("%d failing pod(s)", cl.FailingPods)
		for i, d := range cl.FailingDetails {
			if i == 3 {
				pods += " …"
				break
			}
			pods += fmt.Sprintf(" · %s/%s %s", d.Namespace, d.Name, d.Reason)
		}
		add("failing-pods", okIf(cl.FailingPods == 0, SignalDegraded), pods)
	}

	// platform-api · platform-console: plain HTTP 200.
	sig, detail := p.httpOK(ctx, p.APIHealth)
	add("platform-api", sig, detail)
	sig, detail = p.httpOK(ctx, p.ConsoleURL)
	add("platform-console", sig, detail)

	// argo-apps
	var argo struct {
		Reachability string `json:"reachability"`
		Detail       string `json:"detail"`
		Apps         []struct {
			Name   string `json:"name"`
			Sync   string `json:"sync_status"`
			Health string `json:"health_status"`
		} `json:"apps"`
	}
	if err := p.getJSON(ctx, "/api/v1/gitops/apps", &argo); err != nil {
		add("argo-apps", SignalUnknown, "GET /api/v1/gitops/apps: "+err.Error())
	} else if argo.Reachability != "ok" || len(argo.Apps) == 0 {
		add("argo-apps", SignalUnknown, "argocd "+argo.Reachability+": "+argo.Detail)
	} else {
		// Progressing is a rollout in flight, not a fault: Argo turns a rollout
		// that misses its deadline into Degraded on its own.
		off, moving := []string{}, []string{}
		for _, a := range argo.Apps {
			switch {
			case a.Sync == "Synced" && a.Health == "Healthy":
			case a.Sync == "Synced" && a.Health == "Progressing":
				moving = append(moving, a.Name)
			default:
				off = append(off, a.Name+" "+a.Sync+"/"+a.Health)
			}
		}
		switch {
		case len(off) > 0:
			add("argo-apps", SignalDegraded, strings.Join(off, " · "))
		case len(moving) > 0:
			add("argo-apps", SignalOK, fmt.Sprintf("%d app(s) Synced; progressing: %s", len(argo.Apps), strings.Join(moving, ", ")))
		default:
			add("argo-apps", SignalOK, fmt.Sprintf("%d app(s) Synced/Healthy", len(argo.Apps)))
		}
	}

	// runners-ha · git-bridge · mac-probe-bridge
	var br struct {
		Runners []struct {
			URL    string `json:"url"`
			Role   string `json:"role"`
			Status string `json:"status"`
		} `json:"runners"`
		GitBridge struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"git_bridge"`
		ProbeBridge struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"satellite_probe_bridge"`
	}
	if err := p.getJSON(ctx, "/api/v1/agent/bridge", &br); err != nil {
		for _, id := range []string{"runners-ha", "git-bridge", "mac-probe-bridge"} {
			add(id, SignalUnknown, "GET /api/v1/agent/bridge: "+err.Error())
		}
	} else {
		okN, parts := 0, []string{}
		for _, r := range br.Runners {
			if r.Status == "ok" {
				okN++
			}
			parts = append(parts, r.Role+" "+r.Status)
		}
		switch {
		case len(br.Runners) == 0:
			add("runners-ha", SignalUnknown, "no runners configured")
		case okN == len(br.Runners):
			add("runners-ha", SignalOK, strings.Join(parts, " · "))
		case okN > 0:
			add("runners-ha", SignalDegraded, strings.Join(parts, " · "))
		default:
			add("runners-ha", SignalFail, strings.Join(parts, " · "))
		}
		add("git-bridge", bridgeSignal(br.GitBridge.Status), bridgeDetail(br.GitBridge.Status, "git_bridge", br.GitBridge.Error))
		add("mac-probe-bridge", bridgeSignal(br.ProbeBridge.Status), bridgeDetail(br.ProbeBridge.Status, "probe_bridge", br.ProbeBridge.Error))
	}

	// db-backup-fresh
	var bk struct {
		Signal string `json:"signal"`
		Detail string `json:"detail"`
	}
	if err := p.getJSON(ctx, "/api/v1/cluster/postgres/backup-status", &bk); err != nil {
		add("db-backup-fresh", SignalUnknown, "GET backup-status: "+err.Error())
	} else {
		add("db-backup-fresh", normalizeSignal(bk.Signal), bk.Detail)
	}

	// postgres · redis · nginx-edge · trade-apis — the connectivity matrix row
	// for this environment.
	var mx struct {
		Matrices []struct {
			Environment string `json:"environment"`
			Targets     []struct {
				ID           string `json:"id"`
				Reachability string `json:"reachability"`
				Detail       string `json:"detail"`
			} `json:"targets"`
		} `json:"matrices"`
	}
	matrixItems := []string{"postgres", "redis", "nginx-edge", "trade-apis"}
	if err := p.getJSON(ctx, "/api/v1/matrix", &mx); err != nil {
		for _, id := range matrixItems {
			add(id, SignalUnknown, "GET /api/v1/matrix: "+err.Error())
		}
	} else {
		found := false
		for _, m := range mx.Matrices {
			if m.Environment != p.Env {
				continue
			}
			found = true
			byID := map[string]string{}
			detail := map[string]string{}
			apis, apiBad := 0, []string{}
			for _, t := range m.Targets {
				byID[t.ID] = t.Reachability
				detail[t.ID] = t.Detail
				if strings.HasPrefix(t.ID, "api-") {
					apis++
					if t.Reachability != "ok" {
						apiBad = append(apiBad, t.ID+" "+t.Reachability)
					}
				}
			}
			for _, pair := range [][2]string{{"postgres", "postgres"}, {"redis", "redis"}, {"nginx-edge", "nginx-spa"}} {
				r, ok := byID[pair[1]]
				if !ok {
					add(pair[0], SignalUnknown, pair[1]+" not in the "+p.Env+" matrix")
					continue
				}
				add(pair[0], reachSignal(r), pair[1]+" "+r+": "+detail[pair[1]])
			}
			switch {
			case apis == 0:
				add("trade-apis", SignalUnknown, "no api-* targets in the "+p.Env+" matrix")
			case len(apiBad) == 0:
				add("trade-apis", SignalOK, fmt.Sprintf("%d/%d api targets ok", apis, apis))
			default:
				sort.Strings(apiBad)
				add("trade-apis", SignalDegraded, strings.Join(apiBad, " · "))
			}
		}
		if !found {
			for _, id := range matrixItems {
				add(id, SignalUnknown, "no "+p.Env+" matrix")
			}
		}
	}

	// deliver-pipeline · stg-smoke
	var dp struct {
		Reachability string `json:"reachability"`
		Detail       string `json:"detail"`
	}
	if err := p.getJSON(ctx, "/api/v1/delivery/pipelines", &dp); err != nil {
		add("deliver-pipeline", SignalUnknown, "GET /api/v1/delivery/pipelines: "+err.Error())
	} else {
		add("deliver-pipeline", reachSignal(dp.Reachability), dp.Detail)
	}
	var sm struct {
		Reachability string `json:"reachability"`
		Detail       string `json:"detail"`
	}
	if err := p.getJSON(ctx, "/api/v1/delivery/stg/smoke", &sm); err != nil {
		add("stg-smoke", SignalUnknown, "GET /api/v1/delivery/stg/smoke: "+err.Error())
	} else {
		add("stg-smoke", reachSignal(sm.Reachability), sm.Detail)
	}

	// massive-polygon — the market-data plugin's workers and freshness.
	var md struct {
		Reachability string `json:"reachability"`
		Summary      string `json:"summary"`
	}
	if err := p.getJSON(ctx, "/api/v1/plugins/market-data/status", &md); err != nil {
		add("massive-polygon", SignalUnknown, "GET market-data status: "+err.Error())
	} else {
		add("massive-polygon", reachSignal(md.Reachability), md.Summary)
	}

	// ib-feed is observe-only under D10: say so rather than guess.
	add("ib-feed", SignalUnknown, "observe-only (D10); not probed")

	// hermes-tooling
	var hr struct {
		Ready    bool     `json:"ready"`
		Blockers []string `json:"blockers"`
	}
	if err := p.getJSON(ctx, "/api/v1/agent/hermes/readiness", &hr); err != nil {
		add("hermes-tooling", SignalUnknown, "GET hermes readiness: "+err.Error())
	} else if hr.Ready {
		add("hermes-tooling", SignalOK, "ready")
	} else {
		add("hermes-tooling", SignalDegraded, "not ready: "+strings.Join(hr.Blockers, " · "))
	}

	return out
}

// waitReady polls Base/health until it answers 200 or max passes.
func (p *Prober) waitReady(ctx context.Context, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if sig, _ := p.httpOK(ctx, p.Base+"/health"); sig == SignalOK {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *Prober) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Base+path, nil)
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (p *Prober) httpOK(ctx context.Context, url string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SignalUnknown, err.Error()
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return SignalFail, url + ": " + err.Error()
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SignalFail, fmt.Sprintf("%s: HTTP %d", url, resp.StatusCode)
	}
	return SignalOK, url + ": HTTP 200"
}

func (p *Prober) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func okIf(cond bool, otherwise string) string {
	if cond {
		return SignalOK
	}
	return otherwise
}

// reachSignal maps a Console reachability word to a checklist signal.
func reachSignal(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "ok":
		return SignalOK
	case "degraded", "warn", "partial":
		return SignalDegraded
	case "fail", "failed", "unreachable", "unavailable", "error", "down":
		return SignalFail
	default:
		return SignalUnknown
	}
}

// localOnlyBridgeDetail is the checklist text when a workstation bridge is not
// configured. Git Bridge and the Mac probe bridge are local tools, not a
// cluster feature, so an unset URL is unknown rather than a failure.
const localOnlyBridgeDetail = "local-only (dev workstation)"

// bridgeSignal: a bridge that is not configured for this seat is unknown, not
// a failure.
func bridgeSignal(status string) string {
	if strings.EqualFold(strings.TrimSpace(status), "not_configured") {
		return SignalUnknown
	}
	return reachSignal(status)
}

func bridgeDetail(status, head, err string) string {
	if strings.EqualFold(strings.TrimSpace(status), "not_configured") {
		return localOnlyBridgeDetail
	}
	return joinDetail(head+" "+status, err)
}

func joinDetail(head, err string) string {
	if strings.TrimSpace(err) == "" {
		return head
	}
	return head + ": " + err
}
