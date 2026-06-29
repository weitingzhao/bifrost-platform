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

func TestListHostsDynamicUsesLiveClusterNodes(t *testing.T) {
	topo := &config.TopologyFile{
		Nodes: []config.TopologyNode{
			{ID: "mini-pc-a", Label: "mini-pc-a", Host: "192.168.10.70", Group: "linux"},
			{ID: "ubt-k3s-06", Label: "ubt-k3s-06", Host: "192.168.10.79", Group: "linux"},
			{ID: "stale-pg", Label: "stale-pg", Host: "192.168.10.80", Group: "linux"},
			{ID: "mac-mini-1", Label: "Mac Mini #1", Host: "192.168.10.50", Group: "mac"},
		},
	}
	live := []LiveClusterNode{
		{Name: "ubt-k3s-02", InternalIP: "192.168.10.70", Status: "Ready"},
		{Name: "ubt-k3s-06", InternalIP: "192.168.10.79", Status: "Ready"},
	}
	hosts := ListHostsDynamic(topo, live, true, "vision", false)
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts (2 linux + 1 mac), got %d", len(hosts))
	}
	ips := map[string]string{}
	for _, h := range hosts {
		ips[h.Host] = h.Label
	}
	if ips["192.168.10.80"] != "" {
		t.Fatal("stale topology-only node .80 should not appear when cluster is live")
	}
	if ips["192.168.10.79"] != "ubt-k3s-06" {
		t.Fatalf("live node .79 label: got %q", ips["192.168.10.79"])
	}
	if ips["192.168.10.70"] != "ubt-k3s-02" {
		t.Fatalf("live node .70 label: got %q", ips["192.168.10.70"])
	}
	if ips["192.168.10.50"] != "Mac Mini #1" {
		t.Fatalf("mac host label: got %q", ips["192.168.10.50"])
	}
}

func TestListHostsDynamicFallsBackToTopology(t *testing.T) {
	topo := &config.TopologyFile{
		Nodes: []config.TopologyNode{
			{ID: "a", Label: "A", Host: "192.168.10.73", Group: "linux"},
		},
	}
	hosts := ListHostsDynamic(topo, nil, false, "vision", false)
	if len(hosts) != 1 || hosts[0].Host != "192.168.10.73" {
		t.Fatalf("expected topology fallback, got %+v", hosts)
	}
}

func TestFindHostInList(t *testing.T) {
	hosts := []Host{
		{ID: "ubt-k3s-06", Host: "192.168.10.79"},
	}
	h, ok := FindHostInList(hosts, "ubt-k3s-06", "")
	if !ok || h.Host != "192.168.10.79" {
		t.Fatalf("FindHostInList by id failed: ok=%v host=%q", ok, h.Host)
	}
	h, ok = FindHostInList(hosts, "", "192.168.10.79")
	if !ok || h.ID != "ubt-k3s-06" {
		t.Fatalf("FindHostInList by ip failed: ok=%v id=%q", ok, h.ID)
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
