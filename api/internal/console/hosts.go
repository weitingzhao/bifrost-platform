package console

import (
	"net"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// LiveClusterNode is a K8s cluster node used to build the Linux SSH host row.
type LiveClusterNode struct {
	Name       string
	InternalIP string
	Status     string
}

// Host is an SSH target derived from topology nodes (allowlist).
type Host struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Port      int    `json:"port"`
	Group     string `json:"group"`
	JumpLabel string `json:"jump_label,omitempty"`

	jump *Host `json:"-"`
}

func ListHosts(topo *config.TopologyFile, sshUser string, allowLocalhost bool) []Host {
	if topo == nil {
		return nil
	}
	if sshUser == "" {
		sshUser = "vision"
	}
	out := make([]Host, 0, len(topo.Nodes))
	for _, n := range topo.Nodes {
		h, ok := resolveNodeHost(n, topo, sshUser, allowLocalhost, 0)
		if ok {
			out = append(out, h)
		}
	}
	return out
}

// ListHostsDynamic builds the SSH allowlist. Mac/external rows stay on topology;
// Linux/compute rows follow live K8s nodes when clusterNodesOK is true.
func ListHostsDynamic(
	topo *config.TopologyFile,
	liveNodes []LiveClusterNode,
	clusterNodesOK bool,
	sshUser string,
	allowLocalhost bool,
) []Host {
	if topo == nil {
		return nil
	}
	if sshUser == "" {
		sshUser = "vision"
	}
	if !clusterNodesOK {
		return ListHosts(topo, sshUser, allowLocalhost)
	}

	topoByIP := topologyByIP(topo)
	static := listNonLinuxHosts(topo, sshUser, allowLocalhost)
	linux := listLinuxHostsFromCluster(liveNodes, topo, topoByIP, sshUser, allowLocalhost)
	return append(static, linux...)
}

func FindHost(topo *config.TopologyFile, nodeID, host string, sshUser string, allowLocalhost bool) (Host, bool) {
	hosts := ListHosts(topo, sshUser, allowLocalhost)
	host = strings.TrimSpace(host)
	nodeID = strings.TrimSpace(nodeID)
	for _, h := range hosts {
		if nodeID != "" && h.ID == nodeID {
			return h, true
		}
		if host != "" && h.Host == host {
			return h, true
		}
	}
	return Host{}, false
}

func FindHostInList(hosts []Host, nodeID, host string) (Host, bool) {
	host = strings.TrimSpace(host)
	nodeID = strings.TrimSpace(nodeID)
	for _, h := range hosts {
		if nodeID != "" && h.ID == nodeID {
			return h, true
		}
		if host != "" && h.Host == host {
			return h, true
		}
	}
	return Host{}, false
}

func isLinuxGroup(group string) bool {
	return group == "linux" || group == "compute"
}

func topologyByIP(topo *config.TopologyFile) map[string]config.TopologyNode {
	out := make(map[string]config.TopologyNode)
	for _, n := range topo.Nodes {
		ip := strings.TrimSpace(n.Host)
		if ip != "" {
			out[ip] = n
		}
	}
	return out
}

func listNonLinuxHosts(topo *config.TopologyFile, sshUser string, allowLocalhost bool) []Host {
	out := make([]Host, 0)
	for _, n := range topo.Nodes {
		if isLinuxGroup(n.Group) {
			continue
		}
		h, ok := resolveNodeHost(n, topo, sshUser, allowLocalhost, 0)
		if ok {
			out = append(out, h)
		}
	}
	return out
}

func listLinuxHostsFromCluster(
	liveNodes []LiveClusterNode,
	topo *config.TopologyFile,
	topoByIP map[string]config.TopologyNode,
	sshUser string,
	allowLocalhost bool,
) []Host {
	out := make([]Host, 0, len(liveNodes))
	for _, node := range liveNodes {
		h, ok := resolveClusterNodeHost(node, topo, topoByIP, sshUser, allowLocalhost)
		if ok {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Host < out[j].Host
	})
	return out
}

func resolveClusterNodeHost(
	node LiveClusterNode,
	topo *config.TopologyFile,
	topoByIP map[string]config.TopologyNode,
	defaultUser string,
	allowLocalhost bool,
) (Host, bool) {
	ip := strings.TrimSpace(node.InternalIP)
	if ip == "" {
		return Host{}, false
	}
	k8sName := strings.TrimSpace(node.Name)
	if k8sName == "" {
		k8sName = ip
	}

	if tn, ok := topoByIP[ip]; ok {
		h, ok := resolveNodeHost(tn, topo, defaultUser, allowLocalhost, 0)
		if !ok {
			return Host{}, false
		}
		h.Label = k8sName
		return h, true
	}

	group := "linux"
	lower := strings.ToLower(k8sName)
	if strings.Contains(lower, "gpu") {
		group = "compute"
	}
	synth := config.TopologyNode{
		ID:    k8sName,
		Label: k8sName,
		Host:  ip,
		Group: group,
	}
	return resolveNodeHost(synth, topo, defaultUser, allowLocalhost, 0)
}

func resolveNodeHost(
	n config.TopologyNode,
	topo *config.TopologyFile,
	defaultUser string,
	allowLocalhost bool,
	depth int,
) (Host, bool) {
	if depth > 2 {
		return Host{}, false
	}
	h := strings.TrimSpace(n.Host)
	if h == "" {
		return Host{}, false
	}
	if !allowLocalhost && isLocalHost(h) {
		return Host{}, false
	}
	user := defaultUser
	if strings.TrimSpace(n.SSHUser) != "" {
		user = strings.TrimSpace(n.SSHUser)
	}
	port := 22
	if n.SSHPort > 0 {
		port = n.SSHPort
	}
	out := Host{
		ID:    n.ID,
		Label: n.Label,
		Host:  h,
		User:  user,
		Port:  port,
		Group: n.Group,
	}
	jumpID := strings.TrimSpace(n.SSHJump)
	if jumpID == "" {
		return out, true
	}
	jumpNode, ok := findTopologyNode(topo, jumpID)
	if !ok {
		return out, true
	}
	jumpHost, ok := resolveNodeHost(jumpNode, topo, defaultUser, allowLocalhost, depth+1)
	if !ok {
		return out, true
	}
	jumpCopy := jumpHost
	jumpCopy.jump = nil
	out.jump = &jumpCopy
	out.JumpLabel = jumpHost.Label
	return out, true
}

func findTopologyNode(topo *config.TopologyFile, id string) (config.TopologyNode, bool) {
	for _, n := range topo.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return config.TopologyNode{}, false
}

func isLocalHost(h string) bool {
	switch strings.ToLower(h) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

// ReachableHost probes direct TCP or bastion reachability for jump targets.
func ReachableHost(h Host, timeout time.Duration) bool {
	if h.jump != nil {
		return Reachable(h.jump.Host, h.jump.Port, timeout)
	}
	return Reachable(h.Host, h.Port, timeout)
}

// Reachable reports whether TCP port is open (short probe for UI badges).
func Reachable(host string, port int, timeout time.Duration) bool {
	if host == "" || port <= 0 {
		return false
	}
	addr := net.JoinHostPort(host, itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
