package checklist

// FixCapability mirrors console dailyOpsChecklistCatalog FixCapability.
type FixCapability string

const (
	FixFullAuto FixCapability = "full_auto"
	FixSemiAuto FixCapability = "semi_auto"
	FixManual   FixCapability = "manual"
	FixObserve  FixCapability = "observe"
)

// ItemMeta is the server-side subset needed for auto-dispatch gates.
type ItemMeta struct {
	ID            string
	Label         string
	FixScope      string
	FixCapability FixCapability
}

// CatalogItems mirrors console DAILY_OPS_CHECKLIST items (dispatch-relevant fields).
// Keep in sync with console/src/lib/control-room/dailyOpsChecklistCatalog.ts.
var CatalogItems = []ItemMeta{
	{ID: "cluster-api", Label: "Cluster API reachable", FixScope: "cluster_issues_full_auto", FixCapability: FixSemiAuto},
	{ID: "nodes-ready", Label: "All nodes Ready", FixScope: "cluster_issues_full_auto", FixCapability: FixSemiAuto},
	{ID: "failing-pods", Label: "No failing pods", FixScope: "cluster_issues_full_auto", FixCapability: FixFullAuto},
	{ID: "platform-api", Label: "platform-api health", FixScope: "platform-self-health-recover", FixCapability: FixFullAuto},
	{ID: "platform-console", Label: "Console reachable", FixScope: "platform-self-health-recover", FixCapability: FixFullAuto},
	{ID: "argo-apps", Label: "GitOps Argo apps synced", FixScope: "platform-self-health-recover", FixCapability: FixSemiAuto},
	{ID: "runners-ha", Label: "Agent runners (HA)", FixScope: "operator-plane-remediate", FixCapability: FixSemiAuto},
	{ID: "git-bridge", Label: "Git bridge healthy + clean", FixScope: "git-dirty-remediate", FixCapability: FixSemiAuto},
	{ID: "mac-probe-bridge", Label: "Mac seat · probe-bridge", FixScope: "", FixCapability: FixManual},
	{ID: "postgres", Label: "PostgreSQL reachable", FixScope: "cluster_issues_full_auto", FixCapability: FixSemiAuto},
	{ID: "db-backup-fresh", Label: "CNPG backup < 48h", FixScope: "data-layer-backup", FixCapability: FixSemiAuto},
	{ID: "redis", Label: "Redis reachable", FixScope: "cluster_issues_full_auto", FixCapability: FixFullAuto},
	{ID: "nginx-edge", Label: "Nginx / SPA edge", FixScope: "cluster_issues_full_auto", FixCapability: FixFullAuto},
	{ID: "trade-apis", Label: "Trade APIs (4 pods / path aliases)", FixScope: "cluster_issues_full_auto", FixCapability: FixFullAuto},
	{ID: "deliver-pipeline", Label: "STG deliver pipeline", FixScope: "deliver-stg-recover", FixCapability: FixFullAuto},
	{ID: "stg-smoke", Label: "STG smoke targets", FixScope: "deliver-stg-recover", FixCapability: FixSemiAuto},
	{ID: "massive-polygon", Label: "Massive / Polygon feed", FixScope: "cluster_issues_full_auto", FixCapability: FixSemiAuto},
	{ID: "ib-feed", Label: "IB data feed", FixScope: "", FixCapability: FixObserve},
	{ID: "hermes-tooling", Label: "Hermes AI tooling", FixScope: "operator-plane-remediate", FixCapability: FixSemiAuto},
}

func ItemByID(id string) (ItemMeta, bool) {
	for _, item := range CatalogItems {
		if item.ID == id {
			return item, true
		}
	}
	return ItemMeta{}, false
}
