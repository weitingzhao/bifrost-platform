package agentdeploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

type StartResponse struct {
	Status string `json:"status"`
	Job    *Job   `json:"job,omitempty"`
	Error  string `json:"error,omitempty"`
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

func (h *Handler) HandleStart(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	enabled, script, hint := deployConfig()
	if !enabled {
		writeJSON(w, http.StatusForbidden, StartResponse{
			Status: "error",
			Error:  hint,
		})
		return
	}
	if _, err := os.Stat(script); err != nil {
		writeJSON(w, http.StatusBadRequest, StartResponse{
			Status: "error",
			Error:  fmt.Sprintf("deploy script not found: %s", script),
		})
		return
	}

	var body struct {
		Remote string `json:"remote"`
		Target string `json:"target"`
	}
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, StartResponse{Status: "error", Error: err.Error()})
			return
		}
	}

	// Resolve the selected target (primary/standby) for role + peer wiring.
	targets := deployTargets()
	selected := targets[0] // default: primary
	if t := strings.TrimSpace(body.Target); t != "" {
		found := false
		for _, dt := range targets {
			if dt.ID == t {
				selected = dt
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusBadRequest, StartResponse{
				Status: "error",
				Error:  fmt.Sprintf("unknown deploy target %q", t),
			})
			return
		}
	} else if rmt := strings.TrimSpace(body.Remote); rmt != "" {
		matched := false
		for _, dt := range targets {
			if dt.Remote == rmt {
				selected = dt
				matched = true
				break
			}
		}
		if !matched {
			selected = DeployTarget{ID: "custom", Role: "primary", Remote: rmt}
		}
	}

	remote, remoteErr := sanitizeDeployRemote(selected.Remote)
	if remoteErr != nil {
		writeJSON(w, http.StatusBadRequest, StartResponse{
			Status: "error",
			Error:  remoteErr.Error(),
		})
		return
	}

	id := time.Now().UTC().Format("20060102T150405.000Z")
	job, ok := h.store.Start(id, remote, selected.Role)
	if !ok {
		writeJSON(w, http.StatusConflict, StartResponse{
			Status: "error",
			Error:  "agent deploy already running",
			Job:    h.store.Current(),
		})
		return
	}
	h.store.AppendLog(fmt.Sprintf("==> Deploy started %s → %s (role=%s)\n", id, remote, selected.Role))

	h.audit.RecordDirect(
		"agent-deploy",
		principal.Role,
		"agent.deploy.start",
		remote,
		"running",
		fmt.Sprintf("job=%s role=%s script=%s", id, selected.Role, script),
	)

	go h.runDeploy(id, script, remote, selected.Role, selected.PeerSSH, selected.PeerURL, principal.Role)

	writeJSON(w, http.StatusAccepted, StartResponse{
		Status: "accepted",
		Job:    job,
	})
}

type logWriter struct {
	store *Store
	buf   *bytes.Buffer
}

func (w *logWriter) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	text := strings.ReplaceAll(string(p), "\x04", "")
	w.store.AppendLog(text)
	return n, err
}

func (h *Handler) runDeploy(id, script, remote, agentRole, peerSSH, peerURL string, principalRole actuation.Role) {
	// script -q gives the child a pseudo-TTY so rsync/ssh line-buffer into our pipe (live log in Console).
	cmd := exec.Command("script", "-q", "/dev/null", "bash", script, remote)
	env := append(os.Environ(), deployExtraEnv()...)
	if agentRole != "" {
		env = append(env, "AGENT_ROLE="+agentRole)
	}
	if peerSSH != "" {
		env = append(env, "PEER_SSH="+peerSSH)
	}
	if peerURL != "" {
		env = append(env, "PEER_URL="+peerURL)
	}
	cmd.Env = env

	var buf bytes.Buffer
	writer := &logWriter{store: h.store, buf: &buf}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	exitCode := 0
	errMsg := ""
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
		errMsg = strings.TrimSpace(buf.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
	}

	job := h.store.Finish(exitCode, errMsg)

	status := "done"
	detail := fmt.Sprintf("job=%s exit=%d", id, exitCode)
	if exitCode != 0 {
		status = "failed"
		if errMsg != "" {
			detail = errMsg
		}
	}
	h.audit.RecordDirect("agent-deploy", principalRole, "agent.deploy.finish", remote, status, detail)

	_ = job
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
	hint = "Runs deploy_mac_mini.sh via platform-api (rsync + launchctl restart on agent host)"
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

// remoteToRunnerURL maps an SSH target to its runner base URL for peer wiring.
func remoteToRunnerURL(remote string) string {
	host := remoteHostPart(remote)
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv("REMEDIATION_RUNNER_PORT"))
	if port == "" {
		port = "8781"
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
		p.PeerURL = remoteToRunnerURL(standby)
	}
	targets = append(targets, p)

	if standby != "" && standby != primary {
		targets = append(targets, DeployTarget{
			ID:      "standby",
			Role:    "standby",
			Remote:  standby,
			PeerSSH: primary,
			PeerURL: remoteToRunnerURL(primary),
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

func deployExtraEnv() []string {
	out := []string{}
	if kc := strings.TrimSpace(os.Getenv("PLATFORM_KUBECONFIG")); kc != "" {
		out = append(out, fmt.Sprintf("KUBECONFIG=%s", kc))
	}
	if kc := strings.TrimSpace(os.Getenv("KUBECONFIG")); kc != "" {
		out = append(out, fmt.Sprintf("KUBECONFIG=%s", kc))
	}
	return out
}

var writeJSONMu sync.Mutex

func writeJSON(w http.ResponseWriter, status int, v any) {
	writeJSONMu.Lock()
	defer writeJSONMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
