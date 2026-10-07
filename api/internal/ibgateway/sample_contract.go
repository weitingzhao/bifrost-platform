package ibgateway

import (
	"context"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const tickKeyPrefix = "ib:ingester:tick:"

// sampleContract is the contract whose tick stands for "market data flows".
// It comes from the gateway's own configuration, so it follows the gateway's
// subscriptions instead of a symbol compiled into the platform (TD-231):
// OPS_IB_SAMPLE_CONTRACT, else ib-gateway-config data.sample_contract, else the
// first of gateway.yaml watchlist_symbols as a stock contract. Empty when none
// is configured; the feed check then reports the missing sample.
func (s *Service) sampleContract(ctx context.Context) string {
	if c := strings.TrimSpace(os.Getenv("OPS_IB_SAMPLE_CONTRACT")); c != "" {
		return c
	}
	if s.cluster == nil {
		return ""
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return ""
	}
	cm, err := clientset.CoreV1().ConfigMaps(dataNamespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return sampleContractFromConfig(cm.Data)
}

func sampleContractFromConfig(data map[string]string) string {
	if c := strings.TrimSpace(data["sample_contract"]); c != "" {
		return c
	}
	var gw struct {
		WatchlistSymbols []string `yaml:"watchlist_symbols"`
	}
	if yaml.Unmarshal([]byte(data["gateway.yaml"]), &gw) != nil {
		return ""
	}
	for _, sym := range gw.WatchlistSymbols {
		if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
			return sym + "|STK|||"
		}
	}
	return ""
}
