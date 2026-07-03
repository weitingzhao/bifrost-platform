package network

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (s *Service) ApplyFirewall(ctx context.Context, includeDefaultDeny bool) (map[string]any, error) {
	if s.applyFirewall != nil {
		return s.applyFirewall(ctx, includeDefaultDeny)
	}
	return runFirewallApplyScript(ctx, includeDefaultDeny)
}

func runFirewallApplyScript(ctx context.Context, includeDefaultDeny bool) (map[string]any, error) {
	scriptPath := resolvePlatformScript("scripts/unifi_firewall_setup.py")
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("firewall apply script not found at %s", scriptPath)
	}

	args := []string{scriptPath, "apply"}
	if includeDefaultDeny {
		args = append(args, "--include-default-deny")
	}

	cmd := exec.CommandContext(ctx, "python3", args...)
	out, err := cmd.CombinedOutput()
	stdout := strings.TrimSpace(string(out))
	if err != nil {
		return nil, fmt.Errorf("apply script: %w: %s", err, stdout)
	}

	return map[string]any{
		"executor":             "scripts/unifi_firewall_setup.py apply",
		"include_default_deny": includeDefaultDeny,
		"stdout":               stdout,
	}, nil
}
