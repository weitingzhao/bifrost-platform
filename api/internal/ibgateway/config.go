package ibgateway

import (
	"fmt"
	"os"
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
	RedisPlatformPass string
	Kubeconfig        string
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
	return Config{RedisPlatformPass: pass, Kubeconfig: kc}, nil
}
