//bifrost:owner foundation

package cluster

import "testing"

func TestNodeCapabilitiesNFSSClient(t *testing.T) {
	caps := nodeCapabilities(map[string]string{
		"storage.nfs/client": "true",
	})
	if len(caps) != 1 || caps[0].ID != "nfs-client" {
		t.Fatalf("got %+v want nfs-client", caps)
	}
}

func TestNodeCapabilitiesGPU(t *testing.T) {
	caps := nodeCapabilities(map[string]string{
		"workload":                  "gpu",
		"nvidia.com/gpu.present":    "true",
		"accelerator":               "nvidia",
	})
	if len(caps) != 2 {
		t.Fatalf("got %d capabilities want 2 (gpu-pool + gpu-nvidia): %+v", len(caps), caps)
	}
}

func TestNodeCapabilitiesGeneralPool(t *testing.T) {
	caps := nodeCapabilities(map[string]string{
		"bifrost.io/workload-pool": "general",
		"storage.nfs/client":       "true",
	})
	if len(caps) != 2 {
		t.Fatalf("got %d capabilities want 2 (general-pool + nfs-client): %+v", len(caps), caps)
	}
}

func TestNodeCapabilitiesEmpty(t *testing.T) {
	if caps := nodeCapabilities(nil); caps != nil {
		t.Fatalf("expected nil, got %+v", caps)
	}
}
