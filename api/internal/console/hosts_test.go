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

func TestListHostsResolvesSSHJump(t *testing.T) {
	topo := &config.TopologyFile{
		Nodes: []config.TopologyNode{
			{ID: "mac-mini-1", Label: "Mac Mini #1", Host: "192.168.10.50", Group: "mac"},
			{
				ID:      "mac-mini-1-orb",
				Label:   "Mac Mini #1 Ubuntu",
				Host:    "192.168.139.147",
				Group:   "linux",
				SSHJump: "mac-mini-1",
			},
		},
	}
	hosts := ListHosts(topo, "vision", false)
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	var orb Host
	for _, h := range hosts {
		if h.ID == "mac-mini-1-orb" {
			orb = h
		}
	}
	if orb.jump == nil {
		t.Fatal("expected jump host resolved")
	}
	if orb.jump.Host != "192.168.10.50" {
		t.Fatalf("jump host ip: got %q", orb.jump.Host)
	}
	if orb.JumpLabel != "Mac Mini #1" {
		t.Fatalf("jump label: got %q", orb.JumpLabel)
	}
}
