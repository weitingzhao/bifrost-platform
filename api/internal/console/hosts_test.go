package console

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestListHostsSkipsLocalhost(t *testing.T) {
	topo := &config.TopologyFile{
		Nodes: []config.TopologyNode{
			{ID: "a", Label: "A", Host: "192.168.10.73", Group: "linux"},
			{ID: "b", Label: "B", Host: "127.0.0.1", Group: "mac"},
		},
	}
	hosts := ListHosts(topo, "vision", false)
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Host != "192.168.10.73" {
		t.Fatalf("unexpected host %q", hosts[0].Host)
	}
}

func TestFindHost(t *testing.T) {
	topo := &config.TopologyFile{
		Nodes: []config.TopologyNode{
			{ID: "mini-pc-c", Label: "mini-pc-c", Host: "192.168.10.73", Group: "linux", SSHPort: 22},
		},
	}
	h, ok := FindHost(topo, "mini-pc-c", "", "vision", false)
	if !ok || h.Host != "192.168.10.73" {
		t.Fatalf("FindHost failed: ok=%v host=%q", ok, h.Host)
	}
	_, ok = FindHost(topo, "", "10.0.0.1", "vision", false)
	if ok {
		t.Fatal("expected unknown host to be rejected")
	}
}
