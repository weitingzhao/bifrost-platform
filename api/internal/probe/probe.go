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

var tradeAPIEndpoints = []HTTPEndpoint{
	{ID: "nginx-spa", Category: "trade_frontend", Path: "/"},
	{ID: "api-monitor", Category: "trade_api", Path: "/api/monitor/status"},
	{ID: "api-massive", Category: "trade_api", Path: "/api/massive/research/massive/health"},
	{ID: "api-docs", Category: "trade_api", Path: "/api/docs/research/docs/health"},
	{ID: "api-ops", Category: "trade_api", Path: "/api/ops/health"},
	{ID: "api-trading", Category: "trade_api", Path: "/api/trading/health"},
	{ID: "api-strategy", Category: "trade_api", Path: "/api/strategy/health"},
	{ID: "api-portfolio", Category: "trade_api", Path: "/api/portfolio/health"},
	{ID: "api-market", Category: "trade_api", Path: "/api/market/health"},
	{ID: "api-research", Category: "trade_api", Path: "/api/research/health"},
}

var policyBlockedTargets = []Target{
	{
		ID:                 "ib-operator-rpc",
		Category:           "trade_write",
		Reachability:       ReachUnknown,
		Auth:               AuthBlocked,
		AuthorizationLevel: "forbidden",
		Detail:             "Platform must not access ib:operator:cmd (R-DV3 / trade write path)",
	},
	{
		ID:                 "daemon-control-write",
		Category:           "trade_write",
		Reachability:       ReachUnknown,
		Auth:               AuthBlocked,
		AuthorizationLevel: "forbidden",
		Detail:             "Platform L0 probe does not invoke daemon_control write endpoints",
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
	base := strings.TrimRight(env.NginxBase, "/")
	targets := make([]Target, 0, len(tradeAPIEndpoints)+4+len(policyBlockedTargets))

	for _, ep := range tradeAPIEndpoints {
		url := base + ep.Path
		targets = append(targets, p.probeHTTP(ctx, ep.ID, ep.Category, url, "", env))
	}

	targets = append(targets, p.probeTCP(ctx, "postgres", "datastore",
		fmt.Sprintf("%s:%d", env.Postgres.Host, env.Postgres.Port)))
	targets = append(targets, p.probeTCP(ctx, "redis", "datastore",
		fmt.Sprintf("%s:%d", env.Redis.Host, env.Redis.Port)))

	capURL := base + "/api/ops/ops/auth/capabilities"
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
		})
	} else {
		targets = append(targets, p.probeCapabilities(ctx, capURL, token, env))
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
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	reach, detail := classifyHTTP(resp.StatusCode)
	return Target{
		ID: id, Category: category, Reachability: reach,
		Auth: AuthSkipped, AuthorizationLevel: "L0",
		Detail: detail, URL: url,
	}
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
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != 200 {
		auth := AuthInvalid
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
