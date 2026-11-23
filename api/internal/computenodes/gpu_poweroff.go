package computenodes

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// gpuPowerOffCommand uses a separate key pinned to the GPU. The remote account
// can only run the idle check and request shutdown through its forced command.
func gpuPowerOffCommand(ctx context.Context, spec *config.ComputeNodeSpec, operation, want string) error {
	if spec.Name != wakeGPUNode || (operation != "check" && operation != "poweroff") {
		return fmt.Errorf("operation is outside the GPU poweroff scope")
	}
	dir := os.Getenv("GPU_POWEROFF_SSH_DIR")
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("GPU poweroff credential is not configured")
	}
	_, host, ok := strings.Cut(strings.TrimSpace(spec.SSHHost), "@")
	address, err := netip.ParseAddr(host)
	if !ok || err != nil || !address.Is4() {
		return fmt.Errorf("GPU poweroff requires the configured GPU IPv4 address")
	}
	key := filepath.Join(dir, "id_ed25519")
	knownHosts := filepath.Join(dir, "known_hosts")
	for _, file := range []string{key, knownHosts} {
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("GPU poweroff credential or pinned host key is unavailable")
		}
	}
	cmd := exec.CommandContext(ctx, "ssh", "-F", "/dev/null", "-T",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none", "-o", "StrictHostKeyChecking=yes",
		"-o", "GlobalKnownHostsFile=/dev/null", "-o", "UserKnownHostsFile="+knownHosts,
		"-i", key, "bifrost-gpu-poweroff@"+address.String(), operation)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("GPU poweroff %s: %w (%s)", operation, err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != want {
		return fmt.Errorf("GPU poweroff %s returned no valid acknowledgment", operation)
	}
	return nil
}
