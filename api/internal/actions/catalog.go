package actions

import (
	"context"
	"sort"
	"strings"
)

// Action is one named write the platform can perform.
// Tier is the resting level returned by GET /api/v1/actions.
// Classify replaces it when the level depends on params.
type Action struct {
	ID          string
	Tier        Tier
	Description string
	Params      []Param
	Method      string
	Pattern     string
	Classify    func(ctx context.Context, params map[string]any) Tier
	// Passthrough keeps unrecognized body keys in params_hash (plugin heal, schedule).
	Passthrough bool
}

// TierOf returns the level for these params.
func (a Action) TierOf(ctx context.Context, params map[string]any) Tier {
	if a.Classify != nil {
		if t := a.Classify(ctx, params); t.Valid() {
			return t
		}
	}
	return a.Tier
}

// View is the GET /api/v1/actions element.
type View struct {
	ID          string  `json:"id"`
	Tier        Tier    `json:"tier"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
}

func (a Action) View() View {
	params := a.Params
	if params == nil {
		params = []Param{}
	}
	return View{ID: a.ID, Tier: a.Tier, Description: a.Description, Params: params}
}

// Catalog is the action directory in stable id order.
func Catalog() []Action {
	out := append([]Action(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ByID returns the action and whether it exists.
func ByID(id string) (Action, bool) {
	id = strings.TrimSpace(id)
	for _, a := range catalog {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

const (
	prodPipelineNote = "bifrost-deliver-prod, bifrost-deliver-platform-prod, and bifrost-deliver-research are tier C because they roll out or gitops-sync a production workload. Image-build, CI, and staging deliver pipelines are tier B (call them directly)."
	prodAppNote      = "Argo applications whose name is prod, starts with prod-, ends with -prod, or contains -prod- are tier C; others are tier B."
	restartNote      = "Namespaces data, bifrost-prod and bifrost-platform-prod are tier C; other namespaces are tier B."
)

var catalog = []Action{
	{
		ID: "start_pipeline_run", Tier: TierB,
		Description: "Start a Tekton pipeline run. " + prodPipelineNote,
		Method:      "POST", Pattern: "/api/v1/delivery/pipelines/{name}/runs",
		Params: []Param{
			{Name: "name", Type: "string", Required: true, In: "path"},
			{Name: "revision", Type: "string", In: "body"},
			{Name: "tag", Type: "string", In: "body"},
			{Name: "who", Type: "string", In: "body"},
			{Name: "params", Type: "object", In: "body"},
		},
		Classify: func(_ context.Context, params map[string]any) Tier {
			if ProdPipeline(str(params["name"])) {
				return TierC
			}
			return TierB
		},
	},
	{
		ID: "release_window_hold", Tier: TierB,
		Description: "Hold or renew the release window. The same who renews; a different who is refused while the window is live. Tier B.",
		Method:      "PUT", Pattern: "/api/v1/delivery/release-window",
		Params: []Param{
			{Name: "what", Type: "string", Required: true, In: "body"},
			{Name: "who", Type: "string", Required: true, In: "body"},
			{Name: "reason", Type: "string", In: "body"},
			{Name: "ttl_minutes", Type: "integer", In: "body"},
			{Name: "env", Type: "string", In: "body"},
		},
	},
	{
		ID: "release_window_release", Tier: TierB,
		Description: "Release the window. The holder calls it directly. force=1 is tier C and clears a window held by someone else.",
		Method:      "DELETE", Pattern: "/api/v1/delivery/release-window",
		Params: []Param{
			{Name: "who", Type: "string", Required: true, In: "body"},
			{Name: "force", Type: "boolean", In: "query"},
		},
		Classify: func(_ context.Context, params map[string]any) Tier {
			if b, ok := asBool(params["force"]); ok && b {
				return TierC
			}
			return TierB
		},
	},
	{
		ID: "sync_mirrors", Tier: TierB,
		Description: "Ask Gitea to fetch allow-listed repositories and wait until the named commits are present. Tier B.",
		Method:      "POST", Pattern: "/api/v1/delivery/mirrors/sync",
		Params: []Param{
			{Name: "repos", Type: "string[]", Required: true, In: "body"},
			{Name: "commits", Type: "object", In: "body"},
		},
	},
	{
		ID: "delete_pipeline_run", Tier: TierB,
		Description: "Delete a terminal Tekton PipelineRun. Tier B; call the endpoint directly.",
		Method:      "DELETE", Pattern: "/api/v1/delivery/runs/{id}",
		Params: []Param{
			{Name: "id", Type: "string", Required: true, In: "path"},
			{Name: "ns", Type: "string", In: "query"},
		},
	},
	{
		ID: "gitops_sync_app", Tier: TierB,
		Description: "Sync an Argo CD application to HEAD. " + prodAppNote,
		Method:      "POST", Pattern: "/api/v1/gitops/apps/{name}/sync",
		Params:   []Param{{Name: "name", Type: "string", Required: true, In: "path"}},
		Classify: classifyApp,
	},
	{
		ID: "gitops_rollback_app", Tier: TierB,
		Description: "Roll an Argo CD application back to a previous revision. " + prodAppNote,
		Method:      "POST", Pattern: "/api/v1/gitops/apps/{name}/rollback",
		Params: []Param{
			{Name: "name", Type: "string", Required: true, In: "path"},
			{Name: "revision", Type: "string", In: "body"},
		},
		Classify: classifyApp,
	},
	{
		ID: "rollout_restart_deployment", Tier: TierB,
		Description: "Rollout-restart a Deployment. " + restartNote,
		Method:      "POST", Pattern: "/api/v1/cluster/workloads/rollout-restart",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "body"},
			{Name: "kind", Type: "string", Required: true, In: "body"},
			{Name: "name", Type: "string", Required: true, In: "body"},
		},
		Classify: func(_ context.Context, params map[string]any) Tier {
			return RestartTier(str(params["namespace"]))
		},
	},
	{
		ID: "scale_deployment", Tier: TierC,
		Description: "Scale a Deployment. Tier C. Increasing replicas of the Deployment named daemon is tier X (D10) and cannot be requested; the direct endpoint still runs the TD-222 scale-up gate.",
		Method:      "POST", Pattern: "/api/v1/cluster/workloads/scale",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "body"},
			{Name: "kind", Type: "string", Required: true, In: "body"},
			{Name: "name", Type: "string", Required: true, In: "body"},
			{Name: "replicas", Type: "integer", Required: true, In: "body"},
		},
		Classify: classifyScale,
	},
	{
		ID: "delete_pod", Tier: TierB,
		Description: "Delete a Pod. Tier B; call the endpoint directly.",
		Method:      "DELETE", Pattern: "/api/v1/cluster/workloads/pods/{namespace}/{name}",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "path"},
			{Name: "name", Type: "string", Required: true, In: "path"},
		},
	},
	{
		ID: "cordon_node", Tier: TierC,
		Description: "Cordon a node so nothing new is scheduled on it. Tier C.",
		Method:      "POST", Pattern: "/api/v1/cluster/nodes/{name}/cordon",
		Params: []Param{{Name: "name", Type: "string", Required: true, In: "path"}},
	},
	{
		ID: "uncordon_node", Tier: TierC,
		Description: "Uncordon a node. Tier C.",
		Method:      "POST", Pattern: "/api/v1/cluster/nodes/{name}/uncordon",
		Params: []Param{{Name: "name", Type: "string", Required: true, In: "path"}},
	},
	{
		ID: "drain_node", Tier: TierD,
		Description: "Drain workloads off a node. Tier D.",
		Method:      "POST", Pattern: "/api/v1/cluster/nodes/{name}/drain",
		Params: []Param{
			{Name: "name", Type: "string", Required: true, In: "path"},
			{Name: "force", Type: "boolean", In: "body"},
			{Name: "delete_local_data", Type: "boolean", In: "body"},
			{Name: "grace_period_seconds", Type: "integer", In: "body"},
		},
	},
	{
		ID: "poweroff_compute_node", Tier: TierD,
		Description: "Drain a compute node and power it off. Tier D.",
		Method:      "POST", Pattern: "/api/v1/cluster/nodes/{name}/poweroff",
		Params: []Param{{Name: "name", Type: "string", Required: true, In: "path"}},
	},
	{
		ID: "wake_compute_node", Tier: TierB,
		Description: "Wake a compute node (Wake-on-LAN). Tier B; call the endpoint directly.",
		Method:      "POST", Pattern: "/api/v1/cluster/nodes/{name}/wake",
		Params: []Param{{Name: "name", Type: "string", Required: true, In: "path"}},
	},
	{
		ID: "trigger_cnpg_backup", Tier: TierC,
		Description: "Create an on-demand CNPG backup. Tier C.",
		Method:      "POST", Pattern: "/api/v1/cluster/postgres/backup",
		Params: []Param{},
	},
	{
		ID: "repair_cnpg_wal_store", Tier: TierD,
		Description: "Repair the CNPG WAL object store and trigger a backup. Tier D. Does not delete Backup CRs.",
		Method:      "POST", Pattern: "/api/v1/cluster/postgres/wal-store/repair",
		Params: []Param{},
	},
	{
		ID: "trigger_data_clone", Tier: TierC,
		Description: "Clone bifrost_prod into a non-prod database. Tier C.",
		Method:      "POST", Pattern: "/api/v1/cluster/data-clone",
		Params: []Param{
			{Name: "source", Type: "string", Required: true, In: "body"},
			{Name: "targets", Type: "string[]", Required: true, In: "body"},
			{Name: "mode", Type: "string", In: "body"},
			{Name: "tables", Type: "string[]", In: "body"},
			{Name: "confirmation_token", Type: "string", Required: true, In: "body"},
			{Name: "confirm", Type: "boolean", Required: true, In: "body"},
		},
	},
	{
		ID: "market_data_heal", Tier: TierB,
		Description: "Execute Market Data doctor prescriptions at POST /api/v1/plugins/market-data/api/market/doctor/heal. Tier B; call that path directly. The wildcard plugin proxy is exempt from the catalog route ratchet.",
		Passthrough: true,
		Params: []Param{
			{Name: "dry_run", Type: "boolean", In: "body"},
			{Name: "finding_ids", Type: "string[]", In: "body"},
		},
	},
	{
		ID: "ib_reconnect", Tier: TierB,
		Description: "Reconnect the IB gateway (soft reconnect_all, D-IB-Heal L1). Tier B; call the endpoint directly.",
		Method:      "POST", Pattern: "/api/v1/plugins/ib-gateway/control/{action}",
		Params: []Param{},
	},
	{
		ID: "ib_mode", Tier: TierC,
		Description: "Switch the IB gateway mode. Tier C.",
		Method:      "POST", Pattern: "/api/v1/plugins/ib-gateway/control/{action}",
		Params: []Param{{Name: "mode", Type: "string", Required: true, In: "body"}},
	},
	{
		ID: "ib_maintenance", Tier: TierC,
		Description: "Set IB gateway maintenance. Tier C.",
		Method:      "POST", Pattern: "/api/v1/plugins/ib-gateway/control/{action}",
		Passthrough: true,
		Params: []Param{
			{Name: "account_id", Type: "string", In: "body"},
			{Name: "enabled", Type: "boolean", In: "body"},
		},
	},
	{
		ID: "ib_self_heal", Tier: TierB,
		Description: "Toggle IB gateway self-heal. Tier B. PROD already runs this loop; call the endpoint directly.",
		Method:      "POST", Pattern: "/api/v1/plugins/ib-gateway/control/{action}",
		Passthrough: true,
		Params:      []Param{{Name: "enabled", Type: "boolean", In: "body"}},
	},
	{
		ID: "unifi_firewall_apply", Tier: TierD,
		Description: "Apply the UniFi firewall policy. Tier D.",
		Method:      "POST", Pattern: "/api/v1/network/firewall/apply",
		Params: []Param{{Name: "include_default_deny", Type: "boolean", In: "body"}},
	},
	{
		ID: "sweep_failed_backups", Tier: TierC,
		Description: "Delete expired failed CNPG Backup CRs. Tier C.",
		Method:      "POST", Pattern: "/api/v1/cluster/postgres/backups/sweep-failed",
		Params: []Param{},
	},
	{
		ID: "ensure_metrics_server", Tier: TierD,
		Description: "Install the metrics-server add-on. Tier D.",
		Method:      "POST", Pattern: "/api/v1/cluster/addons/metrics-server/ensure",
		Params: []Param{},
	},
	{
		ID: "ensure_kube_prometheus_stack", Tier: TierD,
		Description: "Install the kube-prometheus-stack add-on. Tier D.",
		Method:      "POST", Pattern: "/api/v1/cluster/addons/kube-prometheus-stack/ensure",
		Params: []Param{},
	},
	{
		ID: "sync_kubeconfig", Tier: TierD,
		Description: "Copy the host kubeconfig onto the platform path. Tier D.",
		Method:      "POST", Pattern: "/api/v1/cluster/sync-kubeconfig",
		Params: []Param{},
	},
	{
		ID: "ensure_kubeconfig_secret", Tier: TierD,
		Description: "Create or update the platform kubeconfig Secret in the given namespaces. Tier D. The response does not include Secret contents.",
		Method:      "POST", Pattern: "/api/v1/cluster/kubeconfig-secret/ensure",
		Params: []Param{
			{Name: "namespaces", Type: "string[]", In: "body"},
			{Name: "sync_first", Type: "boolean", In: "body"},
		},
	},
	{
		ID: "update_data_clone_schedule", Tier: TierC,
		Description: "Update the data-clone schedule. Tier C.",
		Method:      "PUT", Pattern: "/api/v1/cluster/data-clone/schedule",
		Passthrough: true,
		Params: []Param{
			{Name: "enabled", Type: "boolean", In: "body"},
			{Name: "interval", Type: "string", In: "body"},
			{Name: "source", Type: "string", In: "body"},
			{Name: "targets", Type: "string[]", In: "body"},
			{Name: "mode", Type: "string", In: "body"},
			{Name: "tables", Type: "string[]", In: "body"},
		},
	},
	{
		ID: "market_data_delete", Tier: TierC,
		Description: "Delete a Market Data plugin API resource. Tier C. path is the suffix after /plugins/market-data/api/.",
		Method:      "DELETE", Pattern: "/api/v1/plugins/market-data/api/*",
		Params: []Param{{Name: "path", Type: "string", Required: true, In: "path"}},
	},
	{
		ID: "rolling_reboot", Tier: TierD,
		Description: "Record approval for a weekend node rolling reboot. Tier D. The platform does not reboot nodes; after approval, run scripts/k3s/rolling-reboot.sh --execute --approval <id>.",
		Params:      []Param{},
	},
	{
		ID: "plan_manifest", Tier: TierB,
		Description: "Render a git path and return a plan id. Tier B. The plan runs as the fixed apply pipeline in plan mode.",
		Method:      "POST", Pattern: "/api/v1/actuation/manifests/plan",
		Params: []Param{
			{Name: "repo", Type: "string", Required: true, In: "body"},
			{Name: "path", Type: "string", Required: true, In: "body"},
			{Name: "commit", Type: "string", Required: true, In: "body"},
		},
		Classify: classifyPlanManifest,
	},
	{
		ID: "apply_manifest", Tier: TierC,
		Description: "Apply a completed manifest plan. Tier is the highest namespace in the plan. A rendered daemon Deployment is tier X and cannot be requested. Does not prune.",
		Method:      "POST", Pattern: "/api/v1/actuation/manifests/apply",
		Params: []Param{
			{Name: "plan_id", Type: "string", Required: true, In: "body"},
		},
		Classify: classifyApply,
	},
	{
		ID: "create_job_from_cronjob", Tier: TierB,
		Description: "Create one Job from a CronJob, the same shape as kubectl create job --from. Denied namespaces are refused. PROD business namespaces and data are tier C.",
		Method:      "POST", Pattern: "/api/v1/actuation/jobs/from-cronjob",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "body"},
			{Name: "cronjob", Type: "string", Required: true, In: "body"},
		},
		Classify: classifyJob,
	},
	{
		ID: "delete_finished_jobs", Tier: TierB,
		Description: "Delete Jobs that are Complete or Failed, and finished probe Pods. Tier B. Provide names or label_selector.",
		Method:      "POST", Pattern: "/api/v1/actuation/jobs/delete-finished",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "body"},
			{Name: "names", Type: "string[]", In: "body"},
			{Name: "label_selector", Type: "string", In: "body"},
		},
	},
	{
		ID: "run_probe_pod", Tier: TierB,
		Description: "Run a short-lived probe Job from an allow-listed image. The data namespace is tier C. env_from is limited to the namespaces in the actuation policy.",
		Method:      "POST", Pattern: "/api/v1/actuation/probes",
		Params: []Param{
			{Name: "namespace", Type: "string", Required: true, In: "body"},
			{Name: "image", Type: "string", Required: true, In: "body"},
			{Name: "command", Type: "string[]", Required: true, In: "body"},
			{Name: "args", Type: "string[]", In: "body"},
			{Name: "env_from", Type: "string", In: "body"},
			{Name: "timeout_seconds", Type: "integer", In: "body"},
		},
		Classify: classifyProbe,
	},
	{
		ID: "owner_run_command", Tier: TierD,
		Description: "Record an exact shell command for the Owner to run. Tier D. The platform does not execute it; after approval, run scripts/owner/owner-run.sh <id>.",
		Params: []Param{
			{Name: "command", Type: "string", Required: true, In: "body"},
			{Name: "reason", Type: "string", Required: true, In: "body"},
		},
	},
}

func classifyApp(_ context.Context, params map[string]any) Tier {
	if ProdApp(str(params["name"])) {
		return TierC
	}
	return TierB
}
