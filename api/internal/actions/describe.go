package actions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Description is what a notification or an approval row shows instead of the
// raw params: the environment, one English line, and a few key params.
type Description struct {
	Env       string
	Summary   string
	KeyParams map[string]string
}

const (
	summaryMax  = 140
	keyParamMax = 4
	keyValueMax = 80
)

// hiddenParams never appear in key params: the full command stays in the
// record, tokens and signatures stay out of every summary.
var hiddenParams = map[string]bool{
	"command": true, "reason": true, "runner": true,
	"confirmation_token": true, "confirm": true,
	"policy_yaml": true, "policy_sig": true, "sig": true, "text": true,
}

// notifySafeKeys is the only parameter keys a notification may show for an
// action. Declared parameters are not safe by default: an action with no
// entry shows nothing. run_probe_pod args and command are omitted because
// they can carry a secret. Passthrough keys are absent from every list.
var notifySafeKeys = map[string][]string{
	"start_pipeline_run":         {"name", "revision", "tag", "who"},
	"release_window_hold":        {"what", "who", "ttl_minutes", "env"},
	"release_window_release":     {"who", "force"},
	"release_freeze":             {"who"},
	"sync_mirrors":               {"repos"},
	"delete_pipeline_run":        {"id", "ns"},
	"gitops_sync_app":            {"name"},
	"gitops_rollback_app":        {"name", "revision"},
	"rollout_restart_deployment": {"namespace", "kind", "name"},
	"scale_deployment":           {"namespace", "kind", "name", "replicas"},
	"delete_pod":                 {"namespace", "name"},
	"cordon_node":                {"name"},
	"uncordon_node":              {"name"},
	"drain_node":                 {"name", "force", "delete_local_data", "grace_period_seconds"},
	"poweroff_compute_node":      {"name"},
	"wake_compute_node":          {"name"},
	"trigger_data_clone":         {"source", "targets", "mode"},
	"market_data_heal":           {"dry_run", "finding_ids"},
	"ib_mode":                    {"mode"},
	"ib_maintenance":             {"account_id", "enabled"},
	"ib_self_heal":               {"enabled"},
	"unifi_firewall_apply":       {"include_default_deny"},
	"ensure_kubeconfig_secret":   {"namespaces", "sync_first"},
	"update_data_clone_schedule": {"enabled", "interval", "source", "targets", "mode"},
	"market_data_delete":         {"path"},
	"plan_manifest":              {"repo", "path", "commit"},
	"apply_manifest":             {"plan_id"},
	"create_job_from_cronjob":    {"namespace", "cronjob"},
	"delete_finished_jobs":       {"namespace", "names", "label_selector"},
	"run_probe_pod":              {"namespace", "image", "timeout_seconds", "env_from"},
}

// Describe summarizes normalized params. approvalReason is the request's
// reason, used when the action has nothing better to say.
func (a Action) Describe(ctx context.Context, params map[string]any, approvalReason string) Description {
	d := Description{KeyParams: keyParams(a, params)}
	p := func(k string) string { return str(params[k]) }
	switch a.ID {
	case "start_pipeline_run":
		d.Env = "cicd"
		if ProdPipeline(p("name")) {
			d.Env = "prod"
		}
		ref := shortRef(p("revision"))
		if ref == "" {
			ref = p("tag")
		}
		if ref == "" {
			ref = "main"
		}
		d.Summary = p("name") + " @ " + ref
	case "gitops_sync_app", "gitops_rollback_app":
		d.Env = "cluster"
		if ProdApp(p("name")) {
			d.Env = "prod"
		}
		if a.ID == "gitops_sync_app" {
			d.Summary = "Sync Argo app " + p("name") + " to HEAD"
		} else {
			d.Summary = "Roll back Argo app " + p("name") + " to " + orDefault(shortRef(p("revision")), "the previous revision")
		}
	case "release_window_release":
		d.Env = "cicd"
		d.Summary = "Force-release the release window (requested by " + p("who") + ")"
	case "rollout_restart_deployment":
		d.Env = p("namespace")
		d.Summary = fmt.Sprintf("Restart %s %s/%s", orDefault(p("kind"), "Deployment"), p("namespace"), p("name"))
	case "scale_deployment":
		d.Env = p("namespace")
		d.Summary = fmt.Sprintf("Scale %s %s/%s to %s", orDefault(p("kind"), "Deployment"), p("namespace"), p("name"), scalar(params["replicas"]))
	case "cordon_node", "uncordon_node", "drain_node", "poweroff_compute_node", "wake_compute_node":
		d.Env = "node"
		verb := map[string]string{
			"cordon_node": "Cordon", "uncordon_node": "Uncordon", "drain_node": "Drain",
			"poweroff_compute_node": "Drain and power off", "wake_compute_node": "Wake",
		}[a.ID]
		d.Summary = verb + " node " + p("name")
		if b, ok := asBool(params["force"]); ok && b {
			d.Summary += " (force)"
		}
	case "trigger_cnpg_backup":
		d.Env, d.Summary = "data", "On-demand CNPG backup"
	case "repair_cnpg_wal_store":
		d.Env, d.Summary = "data", "Repair the CNPG WAL object store, then back up"
	case "sweep_failed_backups":
		d.Env, d.Summary = "data", "Delete expired failed CNPG Backup CRs"
	case "trigger_data_clone":
		targets := strings.Join(stringsOf(params["targets"]), ",")
		d.Env = targets
		d.Summary = fmt.Sprintf("Clone %s into %s (%s)", p("source"), targets, orDefault(p("mode"), "default mode"))
	case "update_data_clone_schedule":
		d.Env, d.Summary = "data", "Update the data-clone schedule"
	case "ib_mode":
		d.Env, d.Summary = "ib-gateway", "Switch IB gateway mode to "+p("mode")
	case "ib_maintenance":
		d.Env, d.Summary = "ib-gateway", "Set IB gateway maintenance"
	case "unifi_firewall_apply":
		d.Env, d.Summary = "network", "Apply the UniFi firewall policy"
	case "ensure_metrics_server":
		d.Env, d.Summary = "cluster", "Install the metrics-server add-on"
	case "ensure_kube_prometheus_stack":
		d.Env, d.Summary = "cluster", "Install the kube-prometheus-stack add-on"
	case "sync_kubeconfig":
		d.Env, d.Summary = "cluster", "Copy the host kubeconfig onto the platform path"
	case "ensure_kubeconfig_secret":
		ns := strings.Join(stringsOf(params["namespaces"]), ",")
		d.Env = orDefault(ns, "cluster")
		d.Summary = "Create or update the platform kubeconfig Secret"
	case "market_data_delete":
		d.Env, d.Summary = "market-data", "DELETE /plugins/market-data/api/"+strings.Trim(p("path"), "/")
	case "rolling_reboot":
		d.Env, d.Summary = "nodes", "Weekend rolling reboot of the k3s nodes"
	case "apply_manifest":
		d.Env, d.Summary = describeApply(ctx, p("plan_id"))
	case "create_job_from_cronjob":
		d.Env = p("namespace")
		d.Summary = "Create a Job from CronJob " + p("namespace") + "/" + p("cronjob")
	case "run_probe_pod":
		d.Env = p("namespace")
		d.Summary = "Run a probe Job from " + p("image")
	case "owner_run_command":
		d.Env = "host"
		d.Summary = firstLine(p("reason"))
	}
	if d.Summary == "" {
		d.Summary = firstLine(approvalReason)
	}
	if d.Summary == "" {
		d.Summary = a.ID
	}
	d.Summary = clip(d.Summary, summaryMax)
	return d
}

func describeApply(ctx context.Context, planID string) (string, string) {
	actMu.RLock()
	look := planLookup
	actMu.RUnlock()
	if look == nil {
		return "", "Apply plan " + planID
	}
	sum, err := look(ctx, planID)
	if err != nil {
		return "", "Apply plan " + planID
	}
	env := highestNamespace(sum.Namespaces)
	where := sum.Path
	if where == "" {
		where = "plan " + planID
	}
	return env, fmt.Sprintf("%s · %d objects", where, sum.Objects)
}

// highestNamespace is the namespace with the highest manifest tier in the
// policy; the first one when there is no policy.
func highestNamespace(namespaces []string) string {
	if len(namespaces) == 0 {
		return ""
	}
	p := currentPolicy()
	if p == nil {
		return namespaces[0]
	}
	rank := map[string]int{"B": 1, "C": 2, "D": 3, "X": 4}
	best, bestRank := namespaces[0], -1
	for _, ns := range namespaces {
		tier, err := p.ManifestTier([]string{ns}, false)
		r := rank[tier]
		if err != nil {
			r = rank["X"]
		}
		if r > bestRank {
			best, bestRank = ns, r
		}
	}
	return best
}

// keyParams lists only notifySafeKeys for the action. Keys that arrived
// through Passthrough stay on the record for execution and the params hash,
// and are never shown in a summary or a notification.
func keyParams(a Action, params map[string]any) map[string]string {
	out := map[string]string{}
	names := append([]string(nil), notifySafeKeys[a.ID]...)
	for _, name := range names {
		if len(out) >= keyParamMax {
			break
		}
		if hiddenParams[name] {
			continue
		}
		if _, dup := out[name]; dup {
			continue
		}
		v, ok := params[name]
		if !ok {
			continue
		}
		s := scalar(v)
		if s == "" || s == "false" {
			continue
		}
		out[name] = clip(s, keyValueMax)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FilterNotifyParams keeps only the keys that are safe to show for action.
// An unknown action or a key that is not on its list is dropped, including
// values an older version stored. Callers redact and clip after this.
func FilterNotifyParams(action string, kp map[string]string) map[string]string {
	if len(kp) == 0 {
		return nil
	}
	allow := map[string]bool{}
	for _, name := range notifySafeKeys[action] {
		if hiddenParams[name] {
			continue
		}
		allow[name] = true
	}
	out := map[string]string{}
	for k, v := range kp {
		if allow[k] {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any, []string:
		return strings.Join(stringsOf(t), ",")
	}
	return ""
}

func stringsOf(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, el := range t {
			if s := scalar(el); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func shortRef(ref string) string {
	if len(ref) == 40 && strings.Trim(strings.ToLower(ref), "0123456789abcdef") == "" {
		return ref[:7]
	}
	return ref
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - 1
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
