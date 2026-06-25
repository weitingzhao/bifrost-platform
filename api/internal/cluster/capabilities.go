package cluster

import (
	"sort"
	"strings"
)

// NodeCapabilityView is a human-readable node capability derived from Kubernetes labels.
type NodeCapabilityView struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// CapabilityCatalogEntry documents a governance capability for Ops Console.
type CapabilityCatalogEntry struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Scope       string `json:"scope"` // node | cluster
	LabelHint   string `json:"label_hint,omitempty"`
	RequiredFor string `json:"required_for,omitempty"`
}

type capabilityDef struct {
	id          string
	label       string
	category    string
	labelHint   string
	requiredFor string
	match       func(labels map[string]string) (ok bool, detail string)
}

// capabilityDefs is the authoritative node-capability catalog (label → capability).
var capabilityDefs = []capabilityDef{
	{
		id: "nfs-client", label: "NFS client", category: "storage",
		labelHint: "storage.nfs/client=true",
		requiredFor: "NFS PVC mounts (postgres · gitea · cold archive)",
		match: func(labels map[string]string) (bool, string) {
			if labels["storage.nfs/client"] == "true" {
				return true, "storage.nfs/client=true — nfs-common on host; can mount NAS exports"
			}
			return false, ""
		},
	},
	{
		id: "control-plane", label: "Control plane", category: "infra",
		labelHint: "node-role.kubernetes.io/control-plane",
		requiredFor: "K3s server · etcd · control-plane pinned workloads",
		match: func(labels map[string]string) (bool, string) {
			if _, ok := labels["node-role.kubernetes.io/control-plane"]; ok {
				return true, "node-role.kubernetes.io/control-plane — K3s server member"
			}
			return false, ""
		},
	},
	{
		id: "gpu-nvidia", label: "NVIDIA GPU", category: "compute",
		labelHint: "accelerator=nvidia or nvidia.com/gpu.present=true",
		requiredFor: "ai namespace GPU workloads (Ollama)",
		match: func(labels map[string]string) (bool, string) {
			if labels["accelerator"] == "nvidia" {
				return true, "accelerator=nvidia"
			}
			if labels["nvidia.com/gpu.present"] == "true" {
				return true, "nvidia.com/gpu.present=true"
			}
			return false, ""
		},
	},
	{
		id: "gpu-pool", label: "GPU pool", category: "compute",
		labelHint: "workload=gpu",
		requiredFor: "workload=gpu nodeSelector (ai · MinIO warehouse host)",
		match: func(labels map[string]string) (bool, string) {
			if labels["workload"] == "gpu" {
				return true, "workload=gpu"
			}
			return false, ""
		},
	},
	{
		id: "warehouse", label: "Data warehouse", category: "storage",
		labelHint: "node-role=warehouse",
		requiredFor: "MinIO local object store on gpu-server",
		match: func(labels map[string]string) (bool, string) {
			if labels["node-role"] == "warehouse" {
				return true, "node-role=warehouse"
			}
			return false, ""
		},
	},
	{
		id: "elastic-compute", label: "Elastic compute", category: "placement",
		labelHint: "bifrost.io/workload-pool=compute",
		requiredFor: "On-demand WOL compute pool (gpu-server)",
		match: func(labels map[string]string) (bool, string) {
			if labels["bifrost.io/workload-pool"] == "compute" {
				return true, "bifrost.io/workload-pool=compute"
			}
			return false, ""
		},
	},
	{
		id: "general-pool", label: "General runtime pool", category: "placement",
		labelHint: "bifrost.io/workload-pool=general",
		requiredFor: "amd64 general runtime — stg · dev · platform · CI offload",
		match: func(labels map[string]string) (bool, string) {
			if labels["bifrost.io/workload-pool"] == "general" {
				return true, "bifrost.io/workload-pool=general — amd64 general workload pool"
			}
			return false, ""
		},
	},
	{
		id: "prod-pool", label: "Prod pool", category: "placement",
		labelHint: "bifrost.io/workload-pool=prod|prod-pool",
		requiredFor: "Production runtime binding (mini-pc-a / ubt-k3s-02)",
		match: func(labels map[string]string) (bool, string) {
			pool := labels["bifrost.io/workload-pool"]
			if pool == "prod" || pool == "prod-pool" {
				return true, "bifrost.io/workload-pool=" + pool
			}
			return false, ""
		},
	},
	{
		id: "wol", label: "Wake-on-LAN", category: "infra",
		labelHint: "bifrost.io/wol=enabled",
		requiredFor: "Ops Console power-off / wake for elastic nodes",
		match: func(labels map[string]string) (bool, string) {
			if labels["bifrost.io/wol"] == "enabled" {
				return true, "bifrost.io/wol=enabled"
			}
			return false, ""
		},
	},
	{
		id: "bootstrap-server", label: "Bootstrap server", category: "infra",
		labelHint: "bifrost.io/bootstrap=first-server",
		requiredFor: "First K3s control-plane (cluster bootstrap anchor)",
		match: func(labels map[string]string) (bool, string) {
			if labels["bifrost.io/bootstrap"] == "first-server" {
				return true, "bifrost.io/bootstrap=first-server"
			}
			return false, ""
		},
	},
	{
		id: "postgres-role", label: "PostgreSQL host", category: "storage",
		labelHint: "node-role=postgres",
		requiredFor: "Dedicated data namespace PostgreSQL (planned)",
		match: func(labels map[string]string) (bool, string) {
			if labels["node-role"] == "postgres" {
				return true, "node-role=postgres"
			}
			return false, ""
		},
	},
}

// clusterCapabilityCatalog documents cluster-scoped capabilities probed via the API.
var clusterCapabilityCatalog = []CapabilityCatalogEntry{
	{
		ID: "storage-class-nfs-hot", Label: "StorageClass nfs-hot", Category: "storage", Scope: "cluster",
		RequiredFor: "Hot NAS PVCs (postgres · redis · gitea)",
	},
	{
		ID: "storage-class-nfs-cold", Label: "StorageClass nfs-cold", Category: "storage", Scope: "cluster",
		RequiredFor: "Cold NAS PVCs (archives · backups)",
	},
	{
		ID: "storage-class-local-path", Label: "StorageClass local-path", Category: "storage", Scope: "cluster",
		RequiredFor: "K3s default ephemeral PV (legacy — migrate to nfs-hot)",
	},
	{
		ID: "nfs-provisioner-hot", Label: "NFS provisioner (hot)", Category: "storage", Scope: "cluster",
		RequiredFor: "Dynamic nfs-hot PVC provisioning",
	},
	{
		ID: "nfs-provisioner-cold", Label: "NFS provisioner (cold)", Category: "storage", Scope: "cluster",
		RequiredFor: "Dynamic nfs-cold PVC provisioning",
	},
	{
		ID: "metrics-server", Label: "metrics-server", Category: "infra", Scope: "cluster",
		RequiredFor: "Cluster CPU/MEM usage in Ops Console",
	},
}

func NodeCapabilityCatalog() []CapabilityCatalogEntry {
	out := make([]CapabilityCatalogEntry, 0, len(capabilityDefs)+len(clusterCapabilityCatalog))
	for _, def := range capabilityDefs {
		out = append(out, CapabilityCatalogEntry{
			ID:          def.id,
			Label:       def.label,
			Category:    def.category,
			Scope:       "node",
			LabelHint:   def.labelHint,
			RequiredFor: def.requiredFor,
		})
	}
	out = append(out, clusterCapabilityCatalog...)
	return out
}

func nodeCapabilities(labels map[string]string) []NodeCapabilityView {
	if len(labels) == 0 {
		return nil
	}
	out := make([]NodeCapabilityView, 0, len(capabilityDefs))
	for _, def := range capabilityDefs {
		ok, detail := def.match(labels)
		if !ok {
			continue
		}
		out = append(out, NodeCapabilityView{
			ID:       def.id,
			Label:    def.label,
			Category: def.category,
			Detail:   detail,
		})
	}
	return out
}

func capabilityIDs(labels map[string]string) []string {
	caps := nodeCapabilities(labels)
	if len(caps) == 0 {
		return nil
	}
	ids := make([]string, len(caps))
	for i, c := range caps {
		ids[i] = c.ID
	}
	sort.Strings(ids)
	return ids
}

func HasCapability(labels map[string]string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, c := range nodeCapabilities(labels) {
		if c.ID == id {
			return true
		}
	}
	return false
}

func catalogEntryForNodeCapability(id string) (CapabilityCatalogEntry, bool) {
	for _, def := range capabilityDefs {
		if def.id == id {
			return CapabilityCatalogEntry{
				ID:          def.id,
				Label:       def.label,
				Category:    def.category,
				Scope:       "node",
				LabelHint:   def.labelHint,
				RequiredFor: def.requiredFor,
			}, true
		}
	}
	return CapabilityCatalogEntry{}, false
}
