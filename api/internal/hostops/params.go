package hostops

import "strings"

// Closed parameter names from the reviewed action catalog. Complex values
// are registered object/plan IDs, never an inline shell/path/URL. Adapters
// must resolve those IDs from installed trust before claiming capability.
var parameterNames = map[string][]string{
	"host_status":                  {"check"},
	"host_logs":                    {"service", "since", "lines"},
	"host_storage_status":          {"root_id"},
	"host_network_probe":           {"endpoint_id", "kind"},
	"host_integrity_check":         {"profile"},
	"host_inventory_plan":          {"operation"},
	"platform_state_check":         {"scope", "check"},
	"host_service_restart":         {"service"},
	"host_service_state":           {"service", "state"},
	"host_container_restart":       {"project", "service"},
	"host_compose_reconcile":       {"project", "bundle"},
	"host_image_stage":             {"artifact"},
	"host_monitoring_install":      {"bundle"},
	"host_monitoring_remove":       {"install_id"},
	"host_monitoring_drill":        {"exporter", "duration_s"},
	"host_agentd_update":           {"bundle", "environment"},
	"host_toolchain_install":       {"bundle"},
	"host_validation_run":          {"profile", "bundle"},
	"host_temp_cleanup":            {"plan"},
	"host_lifecycle_collect":       {"plan"},
	"host_netplan_apply":           {"plan"},
	"host_new_node_join":           {"runbook", "cluster_id"},
	"host_new_node_fixed_ip":       {"plan"},
	"host_git_endpoint_set":        {"repo_set", "mapping"},
	"host_disk_provision":          {"disk_id", "layout"},
	"host_data_delete":             {"object_ids", "plan"},
	"host_network_security_change": {"plan"},
	"host_node_power":              {"operation", "plan"},
	"host_node_retire":             {"step", "plan"},
	"host_access_change":           {"account", "plan"},
	"host_backup_verify":           {"backup_id", "plan"},
	"host_execution_resolve":       {"execution_id", "evidence_hash", "outcome"},
}

func validParams[T string | []string](action string, params map[string]T) bool {
	names, ok := parameterNames[action]
	if !ok || len(names) != len(params) {
		return false
	}
	for _, k := range names {
		if _, ok := params[k]; !ok {
			return false
		}
	}
	return true
}

func nonemptyParams(params map[string]string) bool {
	for _, v := range params {
		if strings.TrimSpace(v) == "" || strings.ContainsRune(v, 0) {
			return false
		}
	}
	return true
}
