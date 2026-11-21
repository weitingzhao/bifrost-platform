package computenodes

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Moved from cluster/node_power.go on 2026-11-19, unchanged: only the compute
// nodes' power and host probes ran SSH there.

func runSSHCommand(ctx context.Context, host string, remoteCmd ...string) (string, error) {
	return runSSHCommandWithTimeout(ctx, host, 15, remoteCmd...)
}

func runSSHCommandWithTimeout(ctx context.Context, host string, timeoutSec int, remoteCmd ...string) (string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeoutSec),
		"-o", "StrictHostKeyChecking=accept-new",
		host,
	}
	args = append(args, remoteCmd...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += stderr.String()
	}
	return out, err
}
