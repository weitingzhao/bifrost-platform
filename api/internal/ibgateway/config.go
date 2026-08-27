package ibgateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	dataNamespace      = "data"
	gatewayDeployName  = "ib-gateway"
	redisDeployName    = "redis-ib"
	configMapName      = "ib-gateway-config"
	redisServiceHost   = "redis-ib.data.svc.cluster.local:6379"
	consumerGroupName  = "ib-gateway"
)

var tradeCutoverNamespaces = []string{"bifrost-dev", "bifrost-stg", "bifrost-prod"}

var legacyIBStatefulSets = []string{"ib-market-gateway", "ib-account-agent", "ib-operator"}

type Config struct {
	RedisPlatformPass       string
	Kubeconfig              string
	AutoRepairEnabled       bool
	AutoRolloutCooldownSec  int
	SnapshotStaleSec        float64
	SnapshotStaleMaxRollout int
}

func ConfigFromEnv() (Config, error) {
	pass := os.Getenv("REDIS_IB_PLATFORM_PASS")
	if pass == "" {
		return Config{}, fmt.Errorf("REDIS_IB_PLATFORM_PASS required (redis-ib ACL user platform)")
	}
	kc := os.Getenv("PLATFORM_KUBECONFIG")
	if kc == "" {
		home, _ := os.UserHomeDir()
		kc = home + "/.kube/bifrost-k3s.yaml"
	}
	autoRepair := strings.EqualFold(os.Getenv("OPS_IB_AUTOREPAIR_ENABLED"), "true") ||
		os.Getenv("OPS_IB_AUTOREPAIR_ENABLED") == "1"
	cooldown := 900
	if v := strings.TrimSpace(os.Getenv("OPS_IB_AUTO_ROLLOUT_COOLDOWN_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cooldown = n
		}
	}
	staleSec := 90.0
	if v := strings.TrimSpace(os.Getenv("OPS_IB_SNAPSHOT_STALE_SEC")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			staleSec = f
		}
	}
	maxRollout := 3
	if v := strings.TrimSpace(os.Getenv("OPS_IB_SNAPSHOT_STALE_MAX_BEFORE_ROLLOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxRollout = n
		}
	}
	return Config{
		RedisPlatformPass:       pass,
		Kubeconfig:              kc,
		AutoRepairEnabled:       autoRepair,
		AutoRolloutCooldownSec:  cooldown,
		SnapshotStaleSec:        staleSec,
		SnapshotStaleMaxRollout: maxRollout,
	}, nil
}
