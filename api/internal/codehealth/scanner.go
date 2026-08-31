package codehealth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultScanTimeout = 3 * time.Minute

// resolveWorkspaceRoot finds the Bifrost multi-repo workspace (sibling of
// bifrost-platform) so live rescan can see the same trees scan.sh measures.
func resolveWorkspaceRoot() (string, error) {
	if v := strings.TrimSpace(os.Getenv("BIFROST_WORKSPACE_ROOT")); v != "" {
		if looksLikeWorkspace(v) {
			return filepath.Clean(v), nil
		}
		return "", fmt.Errorf("BIFROST_WORKSPACE_ROOT=%s is not a Bifrost workspace (missing scan.sh siblings)", v)
	}
	if root := strings.TrimSpace(os.Getenv("PLATFORM_PROJECT_ROOT")); root != "" {
		parent := filepath.Dir(filepath.Clean(root))
		if looksLikeWorkspace(parent) {
			return parent, nil
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, cand := range []string{
		wd,
		filepath.Join(wd, ".."),
		filepath.Join(wd, "../.."),
		filepath.Join(wd, "../../.."),
	} {
		abs, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		if looksLikeWorkspace(abs) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("workspace root not found — set BIFROST_WORKSPACE_ROOT to the stocks/ directory containing bifrost-trade-infra")
}

func looksLikeWorkspace(dir string) bool {
	scan := filepath.Join(dir, "bifrost-trade-infra", "agent-config", "scripts", "code-health", "scan.sh")
	if _, err := os.Stat(scan); err != nil {
		return false
	}
	// Need at least one measured repo present.
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-frontend", "bifrost-research"} {
		if st, err := os.Stat(filepath.Join(dir, repo)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func resolveScanScript(workspace string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_CODE_HEALTH_SCAN_SH")); v != "" {
		if _, err := os.Stat(v); err != nil {
			return "", fmt.Errorf("PLATFORM_CODE_HEALTH_SCAN_SH=%s: %w", v, err)
		}
		return v, nil
	}
	p := filepath.Join(workspace, "bifrost-trade-infra", "agent-config", "scripts", "code-health", "scan.sh")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("scan.sh not found at %s", p)
	}
	return p, nil
}

func infraHead(workspace string) (string, error) {
	dir := filepath.Join(workspace, "bifrost-trade-infra")
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// commitsMatch treats short/long hashes as equal when one prefixes the other.
func commitsMatch(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == "" || b == "" || a == "unknown" || b == "unknown" {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// buildFreshness describes whether the stored reading matches live infra HEAD
// and whether this API host can run scan.sh against the workspace.
func buildFreshness(latest *Report) Freshness {
	f := Freshness{}
	ws, err := resolveWorkspaceRoot()
	if err != nil {
		f.RescanAvailable = false
		f.Note = err.Error()
		if latest != nil {
			f.ReadingCommit = latest.Commit
		}
		return f
	}
	f.WorkspaceRoot = ws
	f.RescanAvailable = true
	if head, err := infraHead(ws); err == nil {
		f.InfraHead = head
	} else {
		f.Note = "could not read bifrost-trade-infra HEAD: " + err.Error()
	}
	if latest != nil {
		f.ReadingCommit = latest.Commit
		if f.InfraHead != "" {
			f.StaleVsHead = !commitsMatch(latest.Commit, f.InfraHead)
		}
	}
	if f.StaleVsHead && f.Note == "" {
		f.Note = "stored reading commit differs from live bifrost-trade-infra HEAD — Live Re-scan before Agent cut planning"
	}
	return f
}

// runLiveScan executes scan.sh --json - against the workspace and returns a Report.
// Exit code 1 (OVER baseline) still yields a valid reading; exit 2 is a hard failure.
func runLiveScan(ctx context.Context) (Report, []byte, error) {
	ws, err := resolveWorkspaceRoot()
	if err != nil {
		return Report{}, nil, err
	}
	script, err := resolveScanScript(ws)
	if err != nil {
		return Report{}, nil, err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultScanTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "bash", script, "--root", ws, "--json", "-")
	cmd.Dir = ws
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return Report{}, stderr.Bytes(), fmt.Errorf("scan produced no JSON: %s", msg)
	}

	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return Report{}, raw, fmt.Errorf("parse scan JSON: %w", err)
	}
	if len(rep.Metrics) == 0 {
		return Report{}, raw, fmt.Errorf("scan returned zero metrics — refusing to store as a clean reading")
	}
	if strings.TrimSpace(rep.Commit) == "" {
		return Report{}, raw, fmt.Errorf("scan returned empty commit")
	}
	rep.Source = "live-rescan"
	rep.ReceivedAt = time.Now().UTC()
	if rep.GeneratedAt.IsZero() {
		rep.GeneratedAt = rep.ReceivedAt
	}

	// Exit 1 = OVER (still a valid truth). Exit 2 / other = refuse.
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			if ee.ExitCode() == 1 {
				return rep, stderr.Bytes(), nil
			}
			return Report{}, stderr.Bytes(), fmt.Errorf("scan exited %d: %s", ee.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		return Report{}, stderr.Bytes(), runErr
	}
	return rep, stderr.Bytes(), nil
}
