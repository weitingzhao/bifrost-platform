package satellite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	defaultHTTPTimeout = 12 * time.Second
	maxJSONBodyBytes   = 256 * 1024
)

var ErrUnknownEnvironment = errors.New("unknown environment")

type Service struct {
	cfg    *config.Config
	client *http.Client
}

func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:    cfg,
		client: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

func (s *Service) BusDeep(ctx context.Context, envID string) (BusDeepResponse, error) {
	if s.cfg == nil {
		return BusDeepResponse{}, errors.New("config unavailable")
	}
	env, ok := s.cfg.GetEnvironment(envID)
	if !ok || env == nil {
		return BusDeepResponse{}, fmt.Errorf("%w: %s", ErrUnknownEnvironment, envID)
	}
	return s.busDeepByEnvironment(ctx, *env), nil
}

func (s *Service) busDeepByEnvironment(ctx context.Context, env config.Environment) BusDeepResponse {
	now := time.Now().UTC()
	base := strings.TrimRight(env.NginxBase, "/")
	resp := BusDeepResponse{
		Environment: env.ID,
		Label:       env.Label,
		GeneratedAt: now,
		Detail:      "Deep bus semantics from trade monitor/ops APIs",
		Monitor: MonitorDeep{
			Reachability: probe.ReachUnknown,
			Detail:       "monitor payload unavailable",
			Health: MonitorHealthDeep{
				Reachability: probe.ReachUnknown,
			},
			Daemon: MonitorDaemonDeep{
				Reachability: probe.ReachUnknown,
			},
			Socket: MonitorSocketDeep{
				PolygonWs:         SocketComponentDeep{Reachability: probe.ReachUnknown, Detail: "not reported"},
				IBIngestor:        SocketComponentDeep{Reachability: probe.ReachUnknown, Detail: "not reported"},
				IBAccountAgent:    SocketComponentDeep{Reachability: probe.ReachUnknown, Detail: "not reported"},
				IBOperator:        SocketComponentDeep{Reachability: probe.ReachUnknown, Detail: "not reported"},
				PlatformIBGateway: SocketComponentDeep{Reachability: probe.ReachUnknown, Detail: "not reported"},
			},
			AccountSync: MonitorAccountSyncDeep{
				Reachability: probe.ReachUnknown,
			},
		},
		Ops: OpsDeep{
			Reachability: probe.ReachUnknown,
			Detail:       "ops payload unavailable",
		},
		Ingest: IngestDeep{
			Reachability: probe.ReachUnknown,
			Detail:       "ingest payload unavailable",
			Services:     []IngestServiceDeep{},
		},
		Reachability: probe.ReachUnknown,
	}

	if env.EffectiveProbeMode() == "bridge" {
		bridgeResp := s.busDeepFromBridge(ctx, env)
		bridgeResp.GeneratedAt = now
		return bridgeResp
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		resp.Monitor = s.fetchMonitorDeep(ctx, env, base+"/api/monitor/status")
	}()
	go func() {
		defer wg.Done()
		resp.Ops = s.fetchOpsDeep(ctx, env, base+"/api/ops/health")
	}()
	go func() {
		defer wg.Done()
		resp.Ingest = s.fetchIngestDeep(ctx, env, base+"/api/ops/ops/market-ingest/services")
	}()
	wg.Wait()

	resp.Reachability = aggregateReach(resp.Monitor.Reachability, resp.Ops.Reachability, resp.Ingest.Reachability)

	// Pull fallback only when this env explicitly sets trade_bridge_url (avoid global bridge URL on K3s envs).
	if resp.Reachability == probe.ReachFail && strings.TrimSpace(env.TradeBridgeURL) != "" {
		bridgeResp := s.busDeepFromBridge(ctx, env)
		bridgeResp.GeneratedAt = now
		bridgeResp.Detail = "Pull probe failed; served from satellite-probe-bridge fallback"
		return bridgeResp
	}

	return resp
}

type bridgeProbePayload struct {
	OK         bool            `json:"ok"`
	StatusCode int             `json:"status_code,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type bridgeBusSnapshot struct {
	Source         string             `json:"source"`
	TradeNginxBase string             `json:"trade_nginx_base"`
	GeneratedAt    string             `json:"generated_at"`
	Monitor        bridgeProbePayload `json:"monitor"`
	MarketIngest   bridgeProbePayload `json:"market_ingest"`
}

func (s *Service) busDeepFromBridge(ctx context.Context, env config.Environment) BusDeepResponse {
	bridgeURL := env.EffectiveTradeBridgeURL()
	resp := BusDeepResponse{
		Environment: env.ID,
		Label:       env.Label,
		Detail:      "Deep bus semantics via satellite-probe-bridge",
		Monitor: MonitorDeep{
			Reachability: probe.ReachFail,
			Detail:       "bridge monitor payload unavailable",
			Health:       MonitorHealthDeep{Reachability: probe.ReachFail},
			Daemon:       MonitorDaemonDeep{Reachability: probe.ReachFail},
			Socket: MonitorSocketDeep{
				PolygonWs:         SocketComponentDeep{Reachability: probe.ReachFail, Detail: "bridge unavailable"},
				IBIngestor:        SocketComponentDeep{Reachability: probe.ReachFail, Detail: "bridge unavailable"},
				IBAccountAgent:    SocketComponentDeep{Reachability: probe.ReachFail, Detail: "bridge unavailable"},
				IBOperator:        SocketComponentDeep{Reachability: probe.ReachFail, Detail: "bridge unavailable"},
				PlatformIBGateway: SocketComponentDeep{Reachability: probe.ReachFail, Detail: "bridge unavailable"},
			},
			AccountSync: MonitorAccountSyncDeep{Reachability: probe.ReachFail},
		},
		Ops: OpsDeep{
			Reachability: probe.ReachUnknown,
			Detail:       "ops not probed via bridge",
		},
		Ingest: IngestDeep{
			Reachability: probe.ReachFail,
			Detail:       "bridge ingest payload unavailable",
			Services:     []IngestServiceDeep{},
		},
		Reachability: probe.ReachFail,
	}

	if bridgeURL == "" {
		resp.Detail = "satellite-probe-bridge not configured (set trade_bridge_url or SATELLITE_PROBE_BRIDGE_URL)"
		resp.Monitor.Detail = resp.Detail
		resp.Ingest.Detail = resp.Detail
		return resp
	}

	var snap bridgeBusSnapshot
	if err := s.fetchJSONPlain(ctx, bridgeURL+"/bus-snapshot", &snap); err != nil {
		msg := "satellite-probe-bridge unreachable: " + err.Error()
		resp.Detail = msg
		resp.Monitor.Detail = msg
		resp.Ingest.Detail = msg
		return resp
	}

	if snap.Monitor.OK && len(snap.Monitor.Body) > 0 {
		resp.Monitor = s.parseMonitorDeepJSON(snap.Monitor.Body)
	} else if snap.Monitor.Error != "" {
		resp.Monitor = monitorDeepFromBridgeError(snap.Monitor.Error)
	}

	if snap.MarketIngest.OK && len(snap.MarketIngest.Body) > 0 {
		resp.Ingest = s.parseIngestDeepJSON(snap.MarketIngest.Body)
	} else if snap.MarketIngest.Error != "" {
		resp.Ingest = IngestDeep{
			Reachability: probe.ReachFail,
			Detail:       snap.MarketIngest.Error,
			Services:     []IngestServiceDeep{},
		}
	}

	if snap.TradeNginxBase != "" {
		resp.Detail = fmt.Sprintf("Bridge probe of %s (%s)", snap.TradeNginxBase, snap.Source)
	}

	resp.Reachability = aggregateReach(resp.Monitor.Reachability, resp.Ingest.Reachability)
	return resp
}

func monitorDeepFromBridgeError(msg string) MonitorDeep {
	return MonitorDeep{
		Reachability: probe.ReachFail,
		Detail:       msg,
		Health:       MonitorHealthDeep{Reachability: probe.ReachFail},
		Daemon:       MonitorDaemonDeep{Reachability: probe.ReachFail},
		Socket: MonitorSocketDeep{
			PolygonWs:         SocketComponentDeep{Reachability: probe.ReachFail, Detail: msg},
			IBIngestor:        SocketComponentDeep{Reachability: probe.ReachFail, Detail: msg},
			IBAccountAgent:    SocketComponentDeep{Reachability: probe.ReachFail, Detail: msg},
			IBOperator:        SocketComponentDeep{Reachability: probe.ReachFail, Detail: msg},
			PlatformIBGateway: SocketComponentDeep{Reachability: probe.ReachFail, Detail: msg},
		},
		AccountSync: MonitorAccountSyncDeep{Reachability: probe.ReachFail},
	}
}

func (s *Service) parseMonitorDeepJSON(body []byte) MonitorDeep {
	var raw monitorStatusRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return monitorDeepFromBridgeError("invalid monitor JSON: " + err.Error())
	}
	return buildMonitorDeepFromRaw(raw)
}

func (s *Service) parseIngestDeepJSON(body []byte) IngestDeep {
	var raw struct {
		OK       bool             `json:"ok"`
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return IngestDeep{
			Reachability: probe.ReachFail,
			Detail:       "invalid ingest JSON: " + err.Error(),
			Services:     []IngestServiceDeep{},
		}
	}
	return buildIngestDeepFromRaw(raw.Services)
}

func (s *Service) fetchJSONPlain(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBodyBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		trimmed := strings.TrimSpace(string(body))
		if len(trimmed) > 256 {
			trimmed = trimmed[:256]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, trimmed)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

type monitorStatusRaw struct {
	Health struct {
		SelfCheck    string   `json:"self_check"`
		BlockReasons []string `json:"block_reasons"`
		StatusLamp   string   `json:"status_lamp"`
	} `json:"health"`
	Daemon struct {
		SelfCheck    string         `json:"self_check"`
		Lamp         string         `json:"lamp"`
		BlockReasons []string       `json:"block_reasons"`
		Trading      map[string]any `json:"trading"`
		Heartbeat    map[string]any `json:"heartbeat"`
	} `json:"daemon"`
	Socket map[string]any `json:"socket"`
	AccountSyncDaemon *struct {
		Heartbeat map[string]any `json:"heartbeat"`
	} `json:"account_sync_daemon"`
}

func monitorDetailFromRaw(raw monitorStatusRaw, reach probe.Reachability) string {
	if reach == probe.ReachOK {
		return "Parsed monitor schema v9 subset"
	}
	parts := []string{}
	if lamp := strings.TrimSpace(raw.Health.StatusLamp); lamp != "" {
		parts = append(parts, "health lamp="+lamp)
	}
	if len(raw.Health.BlockReasons) > 0 {
		parts = append(parts, "blocked: "+strings.Join(raw.Health.BlockReasons, ", "))
	}
	if raw.Daemon.Heartbeat != nil {
		if alive, ok := raw.Daemon.Heartbeat["daemon_alive"].(bool); ok && !alive {
			parts = append(parts, "daemon_alive=false")
		}
	}
	nullSockets := []string{}
	for _, key := range []string{"ib_ingestor", "ib_account_agent", "ib_operator", "platform_ib_gateway"} {
		if raw.Socket[key] == nil {
			nullSockets = append(nullSockets, key)
		}
	}
	// Prefer polygon_ws; briefly accept legacy monitor JSON key ``massive``.
	if raw.Socket["polygon_ws"] == nil && raw.Socket["massive"] == nil {
		nullSockets = append(nullSockets, "polygon_ws")
	}
	if len(nullSockets) > 0 {
		parts = append(parts, "socket null: "+strings.Join(nullSockets, ", "))
	}
	if len(parts) == 0 {
		return "monitor unhealthy"
	}
	return strings.Join(parts, " · ")
}

func buildMonitorDeepFromRaw(raw monitorStatusRaw) MonitorDeep {
	healthReach := aggregateReach(reachFromLamp(raw.Health.StatusLamp), reachFromSelfCheck(raw.Health.SelfCheck))
	daemonReach := aggregateReach(
		reachFromLamp(raw.Daemon.Lamp),
		reachFromSelfCheck(raw.Daemon.SelfCheck),
	)

	accountSync := MonitorAccountSyncDeep{
		Reachability: probe.ReachUnknown,
	}
	if raw.AccountSyncDaemon != nil && raw.AccountSyncDaemon.Heartbeat != nil {
		hb := raw.AccountSyncDaemon.Heartbeat
		alive, _ := hb["daemon_alive"].(bool)
		accountSync = MonitorAccountSyncDeep{
			DaemonAlive:  alive,
			StreamLag:    hb["stream_lag"],
			Heartbeat:    hb,
			Reachability: probe.ReachFail,
		}
		if alive {
			accountSync.Reachability = probe.ReachOK
		}
	}

	polygonRaw := raw.Socket["polygon_ws"]
	if polygonRaw == nil {
		polygonRaw = raw.Socket["massive"] // brief accept of legacy monitor JSON
	}
	socket := MonitorSocketDeep{
		PolygonWs:         socketComponentDeep(polygonRaw),
		IBIngestor:        socketComponentDeep(raw.Socket["ib_ingestor"]),
		IBAccountAgent:    socketComponentDeep(raw.Socket["ib_account_agent"]),
		IBOperator:        socketComponentDeep(raw.Socket["ib_operator"]),
		PlatformIBGateway: socketComponentDeep(raw.Socket["platform_ib_gateway"]),
	}
	socketReach := aggregateReach(
		socket.PolygonWs.Reachability,
		socket.IBIngestor.Reachability,
		socket.IBAccountAgent.Reachability,
		socket.IBOperator.Reachability,
		socket.PlatformIBGateway.Reachability,
	)

	reach := aggregateReach(healthReach, daemonReach, socketReach, accountSync.Reachability)
	return MonitorDeep{
		Reachability: reach,
		Detail:       monitorDetailFromRaw(raw, reach),
		Health: MonitorHealthDeep{
			SelfCheck:    raw.Health.SelfCheck,
			BlockReasons: raw.Health.BlockReasons,
			StatusLamp:   raw.Health.StatusLamp,
			Reachability: healthReach,
		},
		Daemon: MonitorDaemonDeep{
			SelfCheck:    raw.Daemon.SelfCheck,
			Lamp:         raw.Daemon.Lamp,
			BlockReasons: raw.Daemon.BlockReasons,
			Trading:      raw.Daemon.Trading,
			Heartbeat:    raw.Daemon.Heartbeat,
			Reachability: daemonReach,
			AutoStatus:   mapFromAny(raw.Daemon.Trading["auto_status"]),
		},
		Socket: socket,
		AccountSync: accountSync,
	}
}

func (s *Service) fetchMonitorDeep(ctx context.Context, env config.Environment, url string) MonitorDeep {
	var raw monitorStatusRaw
	if err := s.fetchJSON(ctx, env, url, &raw); err != nil {
		return monitorDeepFromBridgeError(err.Error())
	}
	return buildMonitorDeepFromRaw(raw)
}

func buildIngestDeepFromRaw(services []map[string]any) IngestDeep {
	out := make([]IngestServiceDeep, 0, len(services))
	reaches := make([]probe.Reachability, 0, len(services))
	for _, item := range services {
		id, _ := item["id"].(string)
		if id == "massive_ws" {
			id = "polygon_ws"
		}
		active, _ := item["process_active"].(string)
		runtimeStatus, _ := item["runtime_status"].(string)
		displayActive, _ := item["display_active"].(string)
		runtimeKind, _ := item["runtime_kind"].(string)
		redisControlEnv, _ := item["redis_control_env"].(string)
		runtimeExternallyManaged, _ := item["runtime_externally_managed"].(bool)
		platformGatewayManaged, _ := item["platform_gateway_managed"].(bool)
		reach := reachFromRuntimeStatus(runtimeStatus)
		if reach == probe.ReachUnknown {
			reach = reachFromProcessActive(active)
			if runtimeExternallyManaged && reach == probe.ReachFail {
				reach = probe.ReachDegraded
			}
			if platformGatewayManaged && reach == probe.ReachFail {
				reach = probe.ReachDegraded
			}
		}
		detail := strings.TrimSpace(displayActive)
		if detail == "" {
			detail = strings.TrimSpace(active)
		}
		if detail == "" {
			detail = "process_active unknown"
		}
		out = append(out, IngestServiceDeep{
			ID:                       id,
			ProcessActive:            active,
			RuntimeStatus:            runtimeStatus,
			DisplayActive:            displayActive,
			RuntimeKind:              runtimeKind,
			RedisControlEnv:          redisControlEnv,
			RuntimeExternallyManaged: runtimeExternallyManaged,
			PlatformGatewayManaged:   platformGatewayManaged,
			Reachability:             reach,
			Detail:                   detail,
		})
		reaches = append(reaches, reach)
	}
	return IngestDeep{
		Services:     out,
		Reachability: aggregateReach(reaches...),
		Detail:       "Parsed market ingest services payload",
	}
}

func (s *Service) fetchOpsDeep(ctx context.Context, env config.Environment, url string) OpsDeep {
	raw := map[string]any{}
	if err := s.fetchJSON(ctx, env, url, &raw); err != nil {
		return OpsDeep{
			Reachability: probe.ReachFail,
			Detail:       err.Error(),
		}
	}
	status, _ := raw["status"].(string)
	executorMode, _ := raw["executor_mode"].(string)
	service, _ := raw["service"].(string)
	var k8sReachable *bool
	if v, ok := raw["k8s_reachable"].(bool); ok {
		k8sReachable = &v
	}
	reach := probe.ReachUnknown
	if strings.EqualFold(status, "ok") {
		reach = probe.ReachOK
	}
	if k8sReachable != nil && !*k8sReachable {
		reach = aggregateReach(reach, probe.ReachDegraded)
	}
	return OpsDeep{
		Status:       status,
		Service:      service,
		ExecutorMode: executorMode,
		K8sReachable: k8sReachable,
		Reachability: reach,
		Detail:       "Parsed ops health payload",
		Raw:          raw,
	}
}

func (s *Service) fetchIngestDeep(ctx context.Context, env config.Environment, url string) IngestDeep {
	var raw struct {
		OK       bool             `json:"ok"`
		Services []map[string]any `json:"services"`
	}
	if err := s.fetchJSON(ctx, env, url, &raw); err != nil {
		return IngestDeep{
			Reachability: probe.ReachFail,
			Detail:       err.Error(),
			Services:     []IngestServiceDeep{},
		}
	}
	return buildIngestDeepFromRaw(raw.Services)
}

func (s *Service) fetchJSON(ctx context.Context, env config.Environment, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	env.ApplyIngressHost(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBodyBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		trimmed := strings.TrimSpace(string(body))
		if len(trimmed) > 256 {
			trimmed = trimmed[:256]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, trimmed)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func socketComponentDeep(v any) SocketComponentDeep {
	m := mapFromAny(v)
	if m == nil {
		return SocketComponentDeep{
			Reachability: probe.ReachUnknown,
			Detail:       "not reported",
		}
	}

	if wsMode, _ := m["ws_mode"].(string); strings.EqualFold(strings.TrimSpace(wsMode), "rest_only") {
		return SocketComponentDeep{
			Reachability: probe.ReachOK,
			Detail:       "policy-off · REST-only (ws not required)",
			Raw:          m,
		}
	}

	transport, _ := m["transport"].(string)
	healthSource, _ := m["health_source"].(string)
	isPlatformGateway := strings.EqualFold(strings.TrimSpace(transport), "platform_gateway") ||
		strings.EqualFold(strings.TrimSpace(healthSource), "platform_ib_gateway")

	if isPlatformGateway {
		connected, _ := m["connected"].(bool)
		serviceAlive, _ := m["service_alive"].(bool)
		if connected || serviceAlive {
			lamp, _ := m["lamp"].(string)
			reach := probe.ReachOK
			if reachFromLamp(lamp) == probe.ReachDegraded {
				reach = probe.ReachDegraded
			}
			return SocketComponentDeep{
				Reachability: reach,
				Lamp:         lamp,
				Detail:       socketComponentDetail(m, reach),
				Raw:          m,
			}
		}
	}

	if components, ok := m["components"].(map[string]any); ok && len(components) > 0 {
		lamp, _ := m["lamp"].(string)
		return socketPlatformGatewayAggregateDeep(m, lamp, components)
	}

	lamp, _ := m["lamp"].(string)
	selfCheck, _ := m["self_check"].(string)
	reaches := []probe.Reachability{
		reachFromLamp(lamp),
		reachFromSelfCheck(selfCheck),
	}
	if connected, ok := m["connected"].(bool); ok {
		if connected {
			reaches = append(reaches, probe.ReachOK)
		} else {
			reaches = append(reaches, probe.ReachFail)
		}
	}
	reach := aggregateReach(reaches...)
	return SocketComponentDeep{
		Reachability: reach,
		Lamp:         lamp,
		SelfCheck:    selfCheck,
		Detail:       socketComponentDetail(m, reach),
		Raw:          m,
	}
}

func socketPlatformGatewayAggregateDeep(m map[string]any, lamp string, components map[string]any) SocketComponentDeep {
	liveCount, total := 0, 0
	for _, block := range components {
		bm := mapFromAny(block)
		if bm == nil {
			continue
		}
		total++
		if c, hasConnected := bm["connected"].(bool); hasConnected && c {
			liveCount++
			continue
		}
		if a, hasAlive := bm["service_alive"].(bool); hasAlive && a {
			liveCount++
		}
	}

	var reach probe.Reachability
	detail := socketComponentDetail(m, reach)
	if title, _ := m["title"].(string); strings.TrimSpace(title) != "" {
		detail = strings.TrimSpace(title)
	}
	switch {
	case total > 0 && liveCount == total:
		reach = probe.ReachOK
		if detail == "" || detail == string(reach) {
			detail = "Platform IB Gateway healthy @ redis-ib"
		}
	case liveCount > 0:
		reach = probe.ReachDegraded
		if detail == "" || detail == string(reach) {
			detail = fmt.Sprintf("%d/%d components live · check TWS slots", liveCount, total)
		}
	default:
		reach = probe.ReachFail
		if detail == "" || detail == string(reach) {
			detail = "Platform IB Gateway unreachable @ redis-ib"
		}
	}

	return SocketComponentDeep{
		Reachability: reach,
		Lamp:         lamp,
		Detail:       detail,
		Raw:          m,
	}
}

func socketComponentDetail(m map[string]any, reach probe.Reachability) string {
	if m == nil {
		return "not reported"
	}
	parts := []string{}
	if status, _ := m["status"].(string); strings.TrimSpace(status) != "" {
		parts = append(parts, status)
	}
	if transport, _ := m["transport"].(string); strings.TrimSpace(transport) != "" {
		parts = append(parts, "transport="+transport)
	}
	if len(parts) == 0 {
		return string(reach)
	}
	return strings.Join(parts, " · ")
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func reachFromLamp(lamp string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(lamp)) {
	case "green":
		return probe.ReachOK
	case "yellow":
		return probe.ReachDegraded
	case "red":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}

func reachFromSelfCheck(selfCheck string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(selfCheck)) {
	case "ok":
		return probe.ReachOK
	case "degraded":
		return probe.ReachDegraded
	case "blocked", "fail", "failed":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}

func reachFromRuntimeStatus(status string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "policy-off", "managed":
		return probe.ReachOK
	case "degraded":
		return probe.ReachDegraded
	case "inactive":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}

func reachFromProcessActive(active string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(active)) {
	case "active":
		return probe.ReachOK
	case "activating":
		return probe.ReachDegraded
	case "inactive", "failed", "deactivating":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}

func aggregateReach(values ...probe.Reachability) probe.Reachability {
	hasOK := false
	hasUnknown := false
	for _, v := range values {
		switch v {
		case probe.ReachFail:
			return probe.ReachFail
		case probe.ReachDegraded:
			return probe.ReachDegraded
		case probe.ReachOK:
			hasOK = true
		case probe.ReachUnknown:
			hasUnknown = true
		}
	}
	if hasUnknown && hasOK {
		return probe.ReachDegraded
	}
	if hasOK {
		return probe.ReachOK
	}
	if hasUnknown {
		return probe.ReachUnknown
	}
	return probe.ReachUnknown
}
