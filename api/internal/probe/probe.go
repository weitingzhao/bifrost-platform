package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

const (
	defaultHTTPTimeout = 8 * time.Second
	tcpTimeout         = 4 * time.Second
)

// The SPA itself; the Trade API routes come from tradeGatewayRoutes (trade_routes.go).
var frontendEndpoint = HTTPEndpoint{ID: "nginx-spa", Category: "trade_frontend", Path: "/", Process: "frontend"}

var policyBlockedTargets = []Target{
	{
		ID:                 "ib-operator-rpc",
		Category:           "trade_write",
		Reachability:       ReachUnknown,
		Auth:               AuthBlocked,
		AuthorizationLevel: "forbidden",
		Detail:             "Agent write to the IB operator command stream (platform-api reconnect_all is D-IB-Heal L1)",
	},
	{
		ID:                 "daemon-control-write",
		Category:           "trade_write",
		Reachability:       ReachUnknown,
		Auth:               AuthBlocked,
		AuthorizationLevel: "forbidden",
		Detail:             "Platform L0 probe does not invoke Monitor POST /control/* (Redis daemon control)",
	},
}

type Prober struct {
	Client *http.Client
}

func NewProber() *Prober {
	return &Prober{
		Client: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

func (p *Prober) ProbeEnvironment(ctx context.Context, env config.Environment) MatrixResponse {
	return p.ProbeEnvironmentWithDatastore(ctx, env, nil)
}

func (p *Prober) ProbeEnvironmentWithDatastore(ctx context.Context, env config.Environment, ds *DatastoreSnapshot) MatrixResponse {
	base := strings.TrimRight(env.NginxBase, "/")
	targets := make([]Target, 0, len(tradeGatewayRoutes)+5+len(policyBlockedTargets))

	spa := p.probeHTTP(ctx, frontendEndpoint.ID, frontendEndpoint.Category, base+frontendEndpoint.Path, "", env)
	spa.Process = frontendEndpoint.Process
	targets = append(targets, spa)
	for _, r := range tradeGatewayRoutes {
		targets = append(targets, p.probeTradeRoute(ctx, r, base, env))
	}

	targets = append(targets, p.probePostgres(ctx, env.ID, postgresCfgAddr(env), ds))
	targets = append(targets, p.probeRedis(ctx, env.ID, redisCfgAddr(env), ds))

	// The ops router's capabilities on api-monitor, through its own prefix (TD-55):
	// /api/monitor/ops/auth/capabilities.
	capURL := base + TradeGatewayPath("ops", "/ops/auth/capabilities")
	token := env.OpsToken()
	if token == "" {
		targets = append(targets, Target{
			ID:                 "ops-capabilities",
			Category:           "trade_auth",
			Reachability:       ReachUnknown,
			Auth:               AuthSkipped,
			AuthorizationLevel: "L0",
			Detail:             "No ops token configured (" + env.OpsTokenEnv + " empty)",
			URL:                capURL,
			Process:            "api-monitor",
		})
	} else {
		capT := p.probeCapabilities(ctx, capURL, token, env)
		capT.Process = "api-monitor"
		targets = append(targets, capT)
	}

	targets = append(targets, policyBlockedTargets...)

	return MatrixResponse{
		Environment: env.ID,
		Label:       env.Label,
		GeneratedAt: time.Now().UTC(),
		Principal: Principal{
			Name:  "platform-probe",
			Level: "L0",
		},
		Targets: targets,
	}
}

func (p *Prober) probeHTTP(ctx context.Context, id, category, url, bearer string, env config.Environment) Target {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Target{
			ID: id, Category: category, Reachability: ReachFail,
			Auth: AuthSkipped, AuthorizationLevel: "L0",
			Detail: "request error: " + err.Error(), URL: url,
		}
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	env.ApplyIngressHost(req)

	resp, err := p.Client.Do(req)
	if err != nil {
		return Target{
			ID: id, Category: category, Reachability: ReachFail,
			Auth: AuthSkipped, AuthorizationLevel: "L0",
			Detail: err.Error(), URL: url,
		}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	reach, detail := classifyHTTP(resp.StatusCode)
	return Target{
		ID: id, Category: category, Reachability: reach,
		Auth: AuthSkipped, AuthorizationLevel: "L0",
		Detail: detail, URL: url,
	}
}

// probeTradeRoute asks the gateway for one prefix's probe path and checks that the answer came from
// the process the route names (TradeGatewayRoute.CheckAnswer): a 200 from the SPA fallback or from
// another router is a failure, not a green row.
func (p *Prober) probeTradeRoute(ctx context.Context, r TradeGatewayRoute, base string, env config.Environment) Target {
	url := base + r.GatewayPath()
	t := Target{
		ID: r.TargetID(), Category: "trade_api", Auth: AuthSkipped, AuthorizationLevel: "L0",
		URL: url, Process: r.Process,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Reachability, t.Detail = ReachFail, "request error: "+err.Error()
		return t
	}
	env.ApplyIngressHost(req)
	resp, err := p.Client.Do(req)
	if err != nil {
		t.Reachability, t.Detail = ReachFail, err.Error()
		return t
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	t.Reachability, t.Detail = classifyHTTP(resp.StatusCode)
	if resp.StatusCode == http.StatusOK {
		ok, detail := r.CheckAnswer(resp.Header.Get("Content-Type"), body)
		t.Detail = detail
		if !ok {
			t.Reachability = ReachFail
		}
	}
	return t
}

func classifyHTTP(code int) (Reachability, string) {
	switch {
	case code == 200:
		return ReachOK, fmt.Sprintf("HTTP %d", code)
	case code == 503:
		return ReachDegraded, fmt.Sprintf("HTTP %d (service starting or degraded)", code)
	case code >= 400:
		return ReachFail, fmt.Sprintf("HTTP %d", code)
	default:
		return ReachUnknown, fmt.Sprintf("HTTP %d", code)
	}
}

func (p *Prober) probeTCP(ctx context.Context, id, category, addr string) Target {
	dialer := net.Dialer{Timeout: tcpTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Target{
			ID: id, Category: category, Reachability: ReachFail,
			Auth: AuthSkipped, AuthorizationLevel: "L0",
			Detail: "TCP dial failed: " + err.Error(), URL: "tcp://" + addr,
		}
	}
	_ = conn.Close()
	return Target{
		ID: id, Category: category, Reachability: ReachOK,
		Auth: AuthSkipped, AuthorizationLevel: "L0",
		Detail: "TCP open", URL: "tcp://" + addr,
	}
}

func (p *Prober) probeCapabilities(ctx context.Context, url, token string, env config.Environment) Target {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Target{
			ID: "ops-capabilities", Category: "trade_auth",
			Reachability: ReachFail, Auth: AuthInvalid,
			AuthorizationLevel: "L0", Detail: err.Error(), URL: url,
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	env.ApplyIngressHost(req)

	resp, err := p.Client.Do(req)
	if err != nil {
		return Target{
			ID: "ops-capabilities", Category: "trade_auth",
			Reachability: ReachFail, Auth: AuthInvalid,
			AuthorizationLevel: "L0", Detail: err.Error(), URL: url,
		}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != 200 {
		var auth AuthStatus
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			auth = AuthInvalid
		} else {
			auth = AuthMissing
		}
		return Target{
			ID: "ops-capabilities", Category: "trade_auth",
			Reachability: ReachFail, Auth: auth,
			AuthorizationLevel: "L0",
			Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), URL: url,
		}
	}

	var payload struct {
		Identity *struct {
			Authenticated bool   `json:"authenticated"`
			Role          string `json:"role"`
			Name          string `json:"name"`
		} `json:"identity"`
		Capabilities *struct {
			CanOperate bool `json:"can_operate"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Target{
			ID: "ops-capabilities", Category: "trade_auth",
			Reachability: ReachDegraded, Auth: AuthInvalid,
			AuthorizationLevel: "L0", Detail: "invalid JSON response", URL: url,
		}
	}

	auth := AuthInvalid
	detail := "capabilities response missing identity"
	level := "L0"
	if payload.Identity != nil {
		if payload.Identity.Authenticated {
			auth = AuthOK
			detail = fmt.Sprintf("role=%s name=%s", payload.Identity.Role, payload.Identity.Name)
			if payload.Capabilities != nil && payload.Capabilities.CanOperate {
				level = "L1-capable"
				detail += " can_operate=true"
			}
		} else if payload.Identity.Name == "invalid-token" {
			auth = AuthInvalid
			detail = "invalid token"
		} else {
			auth = AuthMissing
			detail = "not authenticated"
		}
	}

	reach := ReachOK
	return Target{
		ID: "ops-capabilities", Category: "trade_auth",
		Reachability: reach, Auth: auth,
		AuthorizationLevel: level, Detail: detail, URL: url,
	}
}
