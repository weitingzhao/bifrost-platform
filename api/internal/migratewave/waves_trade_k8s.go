package migratewave

// tradeK8sNativeWaves — sync with console/src/lib/architecture/tradeK8sNativeCatalog.ts

var tradeK8sNativeWaves = []Wave{
	{ID: "w0-dev-mock", Code: "W0", SpineIndex: 0, Label: "Dev IB Mock Gateway — zero live TWS client_id on bifrost-dev"},
	{ID: "w1-ingress", Code: "W1", SpineIndex: 1, Label: "Traefik Ingress replaces in-cluster nginx gateway"},
	{ID: "w2-ops-k8s-executor", Code: "W2", SpineIndex: 2, Label: "api-ops executor_mode kubernetes — restore celery-worker Deployment"},
	{ID: "w3-manifest-refactor", Code: "W3", SpineIndex: 3, Label: "Kustomize API component + single image; fix prod config mount alias"},
	{ID: "w4-ib-lease-lib", Code: "W4", SpineIndex: 4, Label: "K8s Lease leader election module in bifrost-trade-socket"},
	{ID: "w5-ib-statefulset", Code: "W5", SpineIndex: 5, Label: "IB socket StatefulSet + ServiceAccount/RBAC for Lease"},
	{ID: "w6-ib-gateway-merge", Code: "W6", SpineIndex: 6, Label: "Merge ingestor+listener+worker_market → ib-market-gateway (3 gateways total)"},
	{ID: "w7-probes-init", Code: "W7", SpineIndex: 7, Label: "Probes + initContainers — socket/worker wait CNPG/Redis ready"},
	{ID: "w8-daemon-lease", Code: "W8", SpineIndex: 8, Label: "Daemon Deployment + Lease — single active auto-trade (R-DV3)"},
	{ID: "w9-network-policy", Code: "W9", SpineIndex: 9, Label: "NetworkPolicy — env isolation + IB LAN egress allowlist"},
	{ID: "w10-observability", Code: "W10", SpineIndex: 10, Label: "IB data-line budget ConfigMap + Celery/Flower metrics"},
	{ID: "w11-signoff", Code: "W11", SpineIndex: 11, Label: "STG Tier A/B + deliver-prod gate — Compose→K3s native sign-off"},
}
