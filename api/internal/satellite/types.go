package satellite

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type BusDeepResponse struct {
	Environment  string             `json:"environment"`
	Label        string             `json:"label"`
	GeneratedAt  time.Time          `json:"generated_at"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Monitor      MonitorDeep        `json:"monitor"`
	Ops          OpsDeep            `json:"ops"`
	Ingest       IngestDeep         `json:"ingest"`
}

type MonitorDeep struct {
	Reachability probe.Reachability     `json:"reachability"`
	Detail       string                 `json:"detail"`
	Health       MonitorHealthDeep      `json:"health"`
	Daemon       MonitorDaemonDeep      `json:"daemon"`
	Socket       MonitorSocketDeep      `json:"socket"`
	Celery       MonitorCeleryDeep      `json:"celery"`
	AccountSync  MonitorAccountSyncDeep `json:"account_sync"`
}

type MonitorHealthDeep struct {
	SelfCheck    string             `json:"self_check,omitempty"`
	BlockReasons []string           `json:"block_reasons,omitempty"`
	StatusLamp   string             `json:"status_lamp,omitempty"`
	Reachability probe.Reachability `json:"reachability"`
}

type MonitorDaemonDeep struct {
	SelfCheck       string             `json:"self_check,omitempty"`
	Lamp            string             `json:"lamp,omitempty"`
	BlockReasons    []string           `json:"block_reasons,omitempty"`
	Trading         map[string]any     `json:"trading,omitempty"`
	Heartbeat       map[string]any     `json:"heartbeat,omitempty"`
	Reachability    probe.Reachability `json:"reachability"`
	AutoStatus      map[string]any     `json:"auto_status,omitempty"`
	TradingFSMState map[string]any     `json:"trading_fsm_state,omitempty"`
}

type MonitorSocketDeep struct {
	Massive           SocketComponentDeep `json:"massive"`
	IBIngestor        SocketComponentDeep `json:"ib_ingestor"`
	IBAccountAgent    SocketComponentDeep `json:"ib_account_agent"`
	IBOperator        SocketComponentDeep `json:"ib_operator"`
	PlatformIBGateway SocketComponentDeep `json:"platform_ib_gateway"`
}

type SocketComponentDeep struct {
	Reachability probe.Reachability `json:"reachability"`
	Lamp         string             `json:"lamp,omitempty"`
	SelfCheck    string             `json:"self_check,omitempty"`
	Detail       string             `json:"detail"`
	Raw          map[string]any     `json:"raw,omitempty"`
}

type MonitorCeleryDeep struct {
	BrokerConnected   bool               `json:"broker_connected"`
	Workers           []string           `json:"workers"`
	WorkerIBConnected bool               `json:"worker_ib_connected"`
	WorkerIBClientID  any                `json:"worker_ib_client_id,omitempty"`
	WorkerLastUpdated any                `json:"worker_last_updated_ts,omitempty"`
	Reachability      probe.Reachability `json:"reachability"`
}

type MonitorAccountSyncDeep struct {
	DaemonAlive  bool               `json:"daemon_alive"`
	StreamLag    any                `json:"stream_lag,omitempty"`
	Heartbeat    map[string]any     `json:"heartbeat,omitempty"`
	Reachability probe.Reachability `json:"reachability"`
}

type OpsDeep struct {
	Status       string             `json:"status,omitempty"`
	Service      string             `json:"service,omitempty"`
	ExecutorMode string             `json:"executor_mode,omitempty"`
	K8sReachable *bool              `json:"k8s_reachable,omitempty"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Raw          map[string]any     `json:"raw,omitempty"`
}

type IngestDeep struct {
	Services     []IngestServiceDeep `json:"services"`
	Reachability probe.Reachability  `json:"reachability"`
	Detail       string              `json:"detail"`
}

type IngestServiceDeep struct {
	ID                       string             `json:"id"`
	ProcessActive            string             `json:"process_active,omitempty"`
	RuntimeKind              string             `json:"runtime_kind,omitempty"`
	RedisControlEnv          string             `json:"redis_control_env,omitempty"`
	RuntimeExternallyManaged bool               `json:"runtime_externally_managed,omitempty"`
	PlatformGatewayManaged   bool               `json:"platform_gateway_managed,omitempty"`
	Reachability             probe.Reachability `json:"reachability"`
	Detail                   string             `json:"detail"`
}
