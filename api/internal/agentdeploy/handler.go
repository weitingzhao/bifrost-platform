package agentdeploy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

const (
	defaultRemote        = "vision@192.168.10.50"
	defaultStandbyRemote = "vision@192.168.10.52"
)

// DeployTarget describes one deployable agent host (primary or standby) with
// its mutual-watchdog peer wiring, surfaced to Console for per-host deploy.
type DeployTarget struct {
	ID      string `json:"id"`   // primary | standby
	Role    string `json:"role"` // primary | standby
	Remote  string `json:"remote"`
	PeerSSH string `json:"peer_ssh,omitempty"`
	PeerURL string `json:"peer_url,omitempty"`
}

// Handler runs deploy_mac_mini.sh from platform-api host (Mac Pro) over SSH to agent Mini.
type Handler struct {
	audit *actuation.AuditLog
	store *Store
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{
		audit: audit,
		store: NewStore(),
	}
}

type StatusResponse struct {
	Enabled    bool           `json:"enabled"`
	Remote     string         `json:"remote"`
	Targets    []DeployTarget `json:"targets,omitempty"`
	ScriptPath string         `json:"script_path,omitempty"`
	Hint       string         `json:"hint,omitempty"`
	Current    *Job           `json:"current,omitempty"`
	Last       *Job           `json:"last,omitempty"`
}

func (h *Handler) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	enabled, script, hint := deployConfig()
	writeJSON(w, http.StatusOK, StatusResponse{
		Enabled:    enabled,
		Remote:     defaultRemoteTarget(),
		Targets:    deployTargets(),
		ScriptPath: script,
		Hint:       hint,
		Current:    h.store.Current(),
		Last:       h.store.Last(),
	})
}

func deployConfig() (enabled bool, scriptPath string, hint string) {
	flag := strings.TrimSpace(os.Getenv("AGENT_DEPLOY_ENABLED"))
	enabled = flag == "1" || strings.EqualFold(flag, "true") || strings.EqualFold(flag, "yes")
	scriptPath = resolveDeployScript()
	if !enabled {
		hint = "Set AGENT_DEPLOY_ENABLED=1 on platform-api host to allow Console deploy"
		return false, scriptPath, hint
	}
	if _, err := os.Stat(scriptPath); err != nil {
		hint = fmt.Sprintf("Deploy script missing at %s", scriptPath)
		return true, scriptPath, hint
	}
	hint = "Runs deploy_mac_mini.sh via platform-api (rsync + launchctl). Requires SSH publickey (BatchMode) — Console cannot type passwords."
	return true, scriptPath, hint
}

func defaultRemoteTarget() string {
	if v := strings.TrimSpace(os.Getenv("AGENT_DEPLOY_REMOTE")); v != "" {
		if clean, err := sanitizeDeployRemote(v); err == nil {
			return clean
		}
	}
	return defaultRemote
}

func standbyRemoteTarget() string {
	v := strings.TrimSpace(os.Getenv("AGENT_DEPLOY_STANDBY_REMOTE"))
	if v == "" {
		v = defaultStandbyRemote
	}
	if clean, err := sanitizeDeployRemote(v); err == nil {
		return clean
	}
	return ""
}

// remoteHostPart extracts the host from an SSH target (user@host -> host).
func remoteHostPart(remote string) string {
	s := strings.TrimSpace(remote)
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// remoteToPlaneURL maps an SSH target to its operator-plane base URL.
// PEER_URL keeps its name; the value is the plane (:8783), which the
// peer watchdog probes.
func remoteToPlaneURL(remote string) string {
	host := remoteHostPart(remote)
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv("OPERATOR_PLANE_PORT"))
	if port == "" {
		port = "8783"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// deployTargets returns primary + standby hosts with cross-wired watchdog peers.
// Standby is omitted only if its remote is invalid/empty.
func deployTargets() []DeployTarget {
	primary := defaultRemoteTarget()
	standby := standbyRemoteTarget()

	targets := []DeployTarget{}
	p := DeployTarget{ID: "primary", Role: "primary", Remote: primary}
	if standby != "" && standby != primary {
		p.PeerSSH = standby
		p.PeerURL = remoteToPlaneURL(standby)
	}
	targets = append(targets, p)

	if standby != "" && standby != primary {
		targets = append(targets, DeployTarget{
			ID:      "standby",
			Role:    "standby",
			Remote:  standby,
			PeerSSH: primary,
			PeerURL: remoteToPlaneURL(primary),
		})
	}
	return targets
}

// sanitizeDeployRemote strips inline # comments (common .env mistake) and validates SSH target shape.
func sanitizeDeployRemote(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "#"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return "", fmt.Errorf("remote target is empty")
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return "", fmt.Errorf("remote target must not contain spaces (check AGENT_DEPLOY_REMOTE in .env — no inline comments)")
	}
	return s, nil
}

func resolveDeployScript() string {
	if p := strings.TrimSpace(os.Getenv("AGENT_DEPLOY_SCRIPT")); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	roots := []string{}
	if root := strings.TrimSpace(os.Getenv("PLATFORM_PROJECT_ROOT")); root != "" {
		roots = append(roots, root)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	rel := filepath.Join("scripts", "agent", "deploy_mac_mini.sh")
	for _, root := range roots {
		candidate := filepath.Join(root, rel)
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
			return candidate
		}
	}
	if len(roots) > 0 {
		return filepath.Join(roots[0], rel)
	}
	return rel
}

var writeJSONMu sync.Mutex

func writeJSON(w http.ResponseWriter, status int, v any) {
	writeJSONMu.Lock()
	defer writeJSONMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
