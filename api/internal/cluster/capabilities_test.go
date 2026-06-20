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

func TestNodeCapabilitiesEmpty(t *testing.T) {
	if caps := nodeCapabilities(nil); caps != nil {
		t.Fatalf("expected nil, got %+v", caps)
	}
}

func TestHasCapability(t *testing.T) {
	labels := map[string]string{"storage.nfs/client": "true"}
	if !HasCapability(labels, "nfs-client") {
		t.Fatal("expected nfs-client")
	}
	if HasCapability(labels, "gpu-pool") {
		t.Fatal("unexpected gpu-pool")
	}
}

func TestCapabilityIDsSorted(t *testing.T) {
	ids := capabilityIDs(map[string]string{
		"workload":           "gpu",
		"accelerator":        "nvidia",
	})
	if len(ids) != 2 {
		t.Fatalf("got %v", ids)
	}
	if ids[0] != "gpu-nvidia" || ids[1] != "gpu-pool" {
		t.Fatalf("order: got %v", ids)
	}
}
