package ibgateway

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type SlotStatus struct {
	Slot       string             `json:"slot"`
	AccountID  string             `json:"account_id"`
	Status     string             `json:"status"`
	ClientID   int                `json:"client_id,omitempty"`
	Connected  bool               `json:"connected"`
	Reach      probe.Reachability `json:"reachability"`
	Detail     string             `json:"detail,omitempty"`
}

type DeploymentStatus struct {
	Namespace    string             `json:"namespace"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Mode         string             `json:"mode"`
	Reach        probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail,omitempty"`
}

type StatusResponse struct {
	Reachable      bool               `json:"reachable"`
	Reachability   probe.Reachability `json:"reachability"`
	Summary        string             `json:"summary"`
	Mode           string             `json:"mode"`
	Deployment     DeploymentStatus   `json:"deployment"`
	RedisReach     probe.Reachability `json:"redis_reachability"`
	Slots          []SlotStatus       `json:"slots"`
	IngestorHealth  map[string]string `json:"ingestor_health,omitempty"`
	AccountHealth   map[string]string `json:"account_health,omitempty"`
	OperatorHealth  map[string]string `json:"operator_health,omitempty"`
	SampleTick      string            `json:"sample_tick_nvda,omitempty"`
	AccountSnapshot string            `json:"account_snapshot,omitempty"`
	ConsumerGroup   string            `json:"operator_consumer_group,omitempty"`
	Cutover        *CutoverStatus     `json:"cutover,omitempty"`
	Autonomy       string             `json:"autonomy"`
	Error          string             `json:"error,omitempty"`
	Hint           string             `json:"hint,omitempty"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

type ControlRequest struct {
	AccountID string `json:"account_id,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

type ControlResponse struct {
	OK              bool      `json:"ok"`
	Action          string    `json:"action"`
	Target          string    `json:"target"`
	Autonomy        string    `json:"autonomy"`
	Message         string    `json:"message"`
	ActionTaken     string    `json:"action_taken,omitempty"`
	SoftReconnectAt time.Time `json:"soft_reconnect_at,omitempty"`
	GeneratedAt     time.Time `json:"generated_at"`
}

type SelfHealStatusResponse struct {
	Reach           probe.Reachability `json:"reachability"`
	LastAction      string             `json:"last_action,omitempty"`
	LastActionTS    float64            `json:"last_action_ts,omitempty"`
	StaleStreak     int                `json:"stale_streak,omitempty"`
	CooldownUntil   float64            `json:"cooldown_until,omitempty"`
	Reason          string             `json:"reason,omitempty"`
	Enabled         bool               `json:"enabled"`
	RolloutRecommended bool           `json:"rollout_recommended"`
	SnapshotAgeSec  float64            `json:"snapshot_age_sec,omitempty"`
	AutoRepairEnabled bool             `json:"auto_repair_enabled"`
	GeneratedAt     time.Time          `json:"generated_at"`
	Error           string             `json:"error,omitempty"`
}

type TradeCutoverEnv struct {
	Namespace           string             `json:"namespace"`
	LegacyIbReplicas    int                `json:"legacy_ib_replicas"`
	RedisIbExternalName bool               `json:"redis_ib_external_name_ok"`
	Reach               probe.Reachability `json:"reachability"`
	Detail              string             `json:"detail,omitempty"`
}

type CutoverStatus struct {
	LegacySocketRetired bool               `json:"legacy_socket_retired"`
	Reach               probe.Reachability `json:"reachability"`
	Environments        []TradeCutoverEnv  `json:"environments"`
}
