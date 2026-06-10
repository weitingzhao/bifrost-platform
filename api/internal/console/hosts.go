package console

import (
	"net"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// Host is an SSH target derived from topology nodes (allowlist).
type Host struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Host  string `json:"host"`
	User  string `json:"user"`
	Port  int    `json:"port"`
	Group string `json:"group"`
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
		h := strings.TrimSpace(n.Host)
		if h == "" {
			continue
		}
		if !allowLocalhost && isLocalHost(h) {
			continue
		}
		port := 22
		if n.SSHPort > 0 {
			port = n.SSHPort
		}
		out = append(out, Host{
			ID:    n.ID,
			Label: n.Label,
			Host:  h,
			User:  sshUser,
			Port:  port,
			Group: n.Group,
		})
	}
	return out
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

func isLocalHost(h string) bool {
	switch strings.ToLower(h) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
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
