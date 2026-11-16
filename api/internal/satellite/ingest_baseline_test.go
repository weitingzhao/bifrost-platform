package satellite

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"strings"
)

func originalIngestProjection(services []map[string]any) IngestDeep {
	out := make([]IngestServiceDeep, 0, len(services))
	reaches := make([]probe.Reachability, 0, len(services))
	for _, item := range services {
		id, _ := item["id"].(string)
		active, _ := item["process_active"].(string)
		runtimeStatus, _ := item["runtime_status"].(string)
		displayActive, _ := item["display_active"].(string)
		runtimeKind, _ := item["runtime_kind"].(string)
		redisControlEnv, _ := item["redis_control_env"].(string)
		runtimeExternallyManaged, _ := item["runtime_externally_managed"].(bool)
		platformGatewayManaged, _ := item["platform_gateway_managed"].(bool)
		reach := originalRuntimeReach(runtimeStatus)
		if reach == probe.ReachUnknown {
			reach = originalProcessReach(active)
			if runtimeExternallyManaged && reach == probe.ReachFail {
				reach = probe.ReachDegraded
			}
			if platformGatewayManaged && reach == probe.ReachFail {
				reach = probe.ReachDegraded
			}
		}
		detail := strings.TrimSpace(displayActive)
		if detail == "" {
			detail = strings.TrimSpace(active)
		}
		if detail == "" {
			detail = "process_active unknown"
		}
		out = append(out, IngestServiceDeep{
			ID:                       id,
			ProcessActive:            active,
			RuntimeStatus:            runtimeStatus,
			DisplayActive:            displayActive,
			RuntimeKind:              runtimeKind,
			RedisControlEnv:          redisControlEnv,
			RuntimeExternallyManaged: runtimeExternallyManaged,
			PlatformGatewayManaged:   platformGatewayManaged,
			Reachability:             reach,
			Detail:                   detail,
		})
		reaches = append(reaches, reach)
	}
	return IngestDeep{
		Services:     out,
		Reachability: aggregateReach(reaches...),
		Detail:       "Parsed market ingest services payload",
	}
}

func originalRuntimeReach(status string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "policy-off", "managed":
		return probe.ReachOK
	case "degraded":
		return probe.ReachDegraded
	case "inactive":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}

func originalProcessReach(active string) probe.Reachability {
	switch strings.ToLower(strings.TrimSpace(active)) {
	case "active":
		return probe.ReachOK
	case "activating":
		return probe.ReachDegraded
	case "inactive", "failed", "deactivating":
		return probe.ReachFail
	default:
		return probe.ReachUnknown
	}
}
