package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

// ErrApplyNotStarted is a refusal before the apply executor is invoked.
var ErrApplyNotStarted = errors.New("firewall apply not started")

func (s *Service) ApplyFirewall(ctx context.Context, includeDefaultDeny bool) (map[string]any, error) {
	if s.applyFirewall != nil {
		return s.applyFirewall(ctx, includeDefaultDeny)
	}
	return runFirewallApplyScript(ctx, includeDefaultDeny)
}

func runFirewallApplyScript(ctx context.Context, includeDefaultDeny bool) (map[string]any, error) {
	scriptPath := resolvePlatformScript("scripts/unifi_firewall_setup.py")
	if _, err := os.Stat(scriptPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: firewall apply script not found at %s", ErrApplyNotStarted, scriptPath)
		}
		return nil, fmt.Errorf("firewall apply script at %s: %w", scriptPath, err)
	}
	// Validate configuration here, before starting a script whose output is
	// untrusted for deciding whether an approval may be executed again.
	if _, err := unifi.ConfigFromEnv(); err != nil {
		return nil, err
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
