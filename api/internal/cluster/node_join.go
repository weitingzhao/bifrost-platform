package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type JoinNodeRequest struct {
	Profile string `json:"profile"`
}

type JoinProfileView struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	ExpectedNode string `json:"expected_node,omitempty"`
	Script       string `json:"script"`
}

type JoinProfilesResponse struct {
	ClusterID   string            `json:"cluster_id"`
	Profiles    []JoinProfileView `json:"profiles"`
	Enabled     bool              `json:"enabled"`
	Detail      string            `json:"detail,omitempty"`
	GeneratedAt time.Time         `json:"generated_at"`
}

func nodeJoinEnabled() bool {
	return os.Getenv("PLATFORM_NODE_JOIN_ENABLED") == "1"
}

func (s *Service) JoinProfiles() JoinProfilesResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	profiles := s.listJoinProfiles()
	detail := ""
	if !nodeJoinEnabled() {
		detail = "join disabled — set PLATFORM_NODE_JOIN_ENABLED=1"
	}
	return JoinProfilesResponse{
		ClusterID:   base.ClusterID,
		Profiles:    profiles,
		Enabled:     nodeJoinEnabled(),
		Detail:      detail,
		GeneratedAt: now,
	}
}

func (s *Service) listJoinProfiles() []JoinProfileView {
	if s.entry == nil || len(s.entry.JoinProfiles) == 0 {
		return defaultJoinProfiles()
	}
	out := make([]JoinProfileView, 0, len(s.entry.JoinProfiles))
	for _, p := range s.entry.JoinProfiles {
		out = append(out, joinProfileView(p))
	}
	return out
}

func defaultJoinProfiles() []JoinProfileView {
	return []JoinProfileView{{
		ID:           "gpu-server",
		Label:        "K3s agent join — gpu-server (P5a)",
		ExpectedNode: "gpu-server",
		Script:       "join-gpu-server.sh",
	}}
}

func joinProfileView(p config.JoinProfileSpec) JoinProfileView {
	return JoinProfileView{
		ID:           p.ID,
		Label:        p.Label,
		ExpectedNode: p.ExpectedNode,
		Script:       p.Script,
	}
}

func (s *Service) resolveJoinProfile(profileID string) (*config.JoinProfileSpec, error) {
	id := strings.TrimSpace(profileID)
	if id == "" {
		return nil, fmt.Errorf("profile is required")
	}
	if s.entry != nil {
		for i := range s.entry.JoinProfiles {
			if s.entry.JoinProfiles[i].ID == id {
				cp := s.entry.JoinProfiles[i]
				return &cp, nil
			}
		}
	}
	for _, d := range defaultJoinProfiles() {
		if d.ID == id {
			return &config.JoinProfileSpec{
				ID:           d.ID,
				Label:        d.Label,
				ExpectedNode: d.ExpectedNode,
				Script:       d.Script,
			}, nil
		}
	}
	return nil, fmt.Errorf("unknown join profile %q", id)
}

func (s *Service) JoinNode(ctx context.Context, req JoinNodeRequest) (ActuationResponse, error) {
	now := time.Now().UTC()
	if !nodeJoinEnabled() {
		return ActuationResponse{
			OK:          false,
			Action:      "cluster.node.join",
			Target:      req.Profile,
			Message:     "node join disabled (set PLATFORM_NODE_JOIN_ENABLED=1)",
			GeneratedAt: now,
		}, fmt.Errorf("node join disabled")
	}

	profile, err := s.resolveJoinProfile(req.Profile)
	if err != nil {
		return ActuationResponse{
			OK:          false,
			Action:      "cluster.node.join",
			Target:      req.Profile,
			Message:     err.Error(),
			GeneratedAt: now,
		}, err
	}

	scriptName := strings.TrimSpace(profile.Script)
	if scriptName == "" {
		err := fmt.Errorf("join profile %q has no script", profile.ID)
		return ActuationResponse{OK: false, Action: "cluster.node.join", Target: profile.ID, Message: err.Error(), GeneratedAt: now}, err
	}

	absScript := ResolveInfraScript("", scriptName)
	if _, statErr := os.Stat(absScript); statErr != nil {
		msg := fmt.Sprintf("join script not found: %s", absScript)
		return ActuationResponse{OK: false, Action: "cluster.node.join", Target: profile.ID, Message: msg, GeneratedAt: now}, statErr
	}

	if token := strings.TrimSpace(os.Getenv("K3S_TOKEN")); token == "" {
		home, _ := os.UserHomeDir()
		tokenPath := filepath.Join(home, ".bifrost-k3s-node-token")
		if _, err := os.Stat(tokenPath); err != nil {
			msg := "K3S_TOKEN or ~/.bifrost-k3s-node-token required for non-interactive join"
			return ActuationResponse{OK: false, Action: "cluster.node.join", Target: profile.ID, Message: msg, GeneratedAt: now}, fmt.Errorf("%s", msg)
		}
	}

	cmd := exec.CommandContext(ctx, "bash", absScript)
	cmd.Env = append(os.Environ(), joinEnvOverrides(s, profile)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += strings.TrimSpace(stderr.String())
	}
	if len(out) > 4000 {
		out = out[:4000] + "… (truncated)"
	}

	if runErr != nil {
		msg := fmt.Sprintf("join script failed: %v", runErr)
		if out != "" {
			msg += " — " + out
		}
		return ActuationResponse{OK: false, Action: "cluster.node.join", Target: profile.ID, Message: msg, GeneratedAt: now}, runErr
	}

	msg := fmt.Sprintf("join profile %q completed", profile.ID)
	if out != "" {
		msg += " — " + out
	}
	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.join",
		Target:      profile.ID,
		Changed:     true,
		Message:     msg,
		GeneratedAt: now,
	}, nil
}

func joinEnvOverrides(s *Service, profile *config.JoinProfileSpec) []string {
	var env []string
	if kubeconfig := s.kubeconfigPath(); kubeconfig != "" {
		env = append(env, fmt.Sprintf("KUBECONFIG=%s", kubeconfig))
	}
	if s.entry != nil {
		if host := strings.TrimSpace(s.entry.SSHHost); host != "" {
			env = append(env, fmt.Sprintf("BOOTSTRAP_HOST=%s", host))
		}
		if ip := strings.TrimSpace(s.entry.NodeIP); ip != "" {
			env = append(env, fmt.Sprintf("K3S_URL=https://%s:6443", ip))
		}
	}
	if name := strings.TrimSpace(profile.ExpectedNode); name != "" {
		env = append(env, fmt.Sprintf("K3S_NODE_NAME=%s", name))
	}
	return env
}
