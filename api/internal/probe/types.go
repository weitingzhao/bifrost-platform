package probe

import "time"

type Reachability string

const (
	ReachOK       Reachability = "ok"
	ReachDegraded Reachability = "degraded"
	ReachFail     Reachability = "fail"
	ReachUnknown  Reachability = "unknown"
)

type AuthStatus string

const (
	AuthOK       AuthStatus = "ok"
	AuthMissing  AuthStatus = "missing"
	AuthInvalid  AuthStatus = "invalid"
	AuthSkipped  AuthStatus = "skipped"
	AuthBlocked  AuthStatus = "blocked"
)

type Principal struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

type Target struct {
	ID                 string       `json:"id"`
	Category           string       `json:"category"`
	Reachability       Reachability `json:"reachability"`
	Auth               AuthStatus   `json:"auth"`
	AuthorizationLevel string       `json:"authorization_level"`
	Detail             string       `json:"detail"`
	URL                string       `json:"url,omitempty"`
}

type MatrixResponse struct {
	Environment string    `json:"environment"`
	Label       string    `json:"label"`
	GeneratedAt time.Time `json:"generated_at"`
	Principal   Principal `json:"principal"`
	Targets     []Target  `json:"targets"`
}

type HTTPEndpoint struct {
	ID       string
	Category string
	Path     string
}

// PluginHealth is what /metrics needs from a plugin, and the least a plugin can
// answer. The four plugins report wildly different shapes — market-data has
// deployments and worker pools, ib-gateway has connection slots, research has
// almost nothing — so the exposition takes the two fields they all share and
// lets each contribute whatever numbers are worth alerting on.
//
// It exists because nothing watched the plugins: on 2026-09-07 the IB gateway
// could not reach TWS for seventeen hours while its pod stayed 1/1 Running, so
// every generic Kubernetes alert stayed quiet and no Bifrost rule covered it.
type PluginHealth struct {
	Name         string
	Reachable    bool
	Reachability Reachability
	// Gauges are emitted as bifrost_plugin_<key>{plugin="<Name>", ...labels}.
	// Key must already be a valid metric name suffix.
	Gauges []Gauge
}

type Gauge struct {
	Key    string
	Labels map[string]string
	Value  float64
	Help   string
}
