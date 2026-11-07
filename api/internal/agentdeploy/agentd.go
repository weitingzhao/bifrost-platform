package agentdeploy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// deploy_agentd (Tier D, PROD only): the platform deploys a pinned agentd
// build on an execution host once the Owner approved its card. In order:
// the card's params have their form and the Gitea package's .sha256 of the
// commit is the card's sha256; the host is set aside (a drain control, the
// Console's set-aside, which the presence pass counts as taken out of work)
// so it claims nothing; once no attempt runs on it (at most AgentdIdleWait;
// past it the host is set back and the deploy fails) the host's deploy user
// runs exactly `sudo -n agentd-update <commit> <sha256>` over ssh in
// BatchMode; then the host's claims must report that build (at most
// AgentdBuildWait), and the host is set back. A failure after the update
// swapped the binary runs `sudo -n agentd-update --rollback`. The host's ssh
// target is configuration (AGENTD_DEPLOY_TARGETS), never the card's.

// DeployAgentdAction is the action's id.
const DeployAgentdAction = "deploy_agentd"

// TargetsEnv names the configured hosts and their ssh targets:
// host=user@address, comma separated.
const TargetsEnv = "AGENTD_DEPLOY_TARGETS"

const (
	AgentdIdleWait  = 30 * time.Minute
	AgentdBuildWait = 5 * time.Minute
	agentdPoll      = 10 * time.Second
	// updateTimeout bounds the ssh run of agentd-update (and of its rollback).
	updateTimeout = 10 * time.Minute
	// setBackTimeout bounds setting the host back once the run's own context
	// ended.
	setBackTimeout = 30 * time.Second
)

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hostRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
)

// AgentdParams are the card's params.
type AgentdParams struct {
	Env, Host, Commit, SHA256 string
}

// ParseAgentdParams checks the card's params: exactly env (prod), host (a
// host id), commit (40 lower-case hex) and sha256 (64 lower-case hex).
func ParseAgentdParams(params map[string]any) (AgentdParams, error) {
	str := func(k string) string { s, _ := params[k].(string); return s }
	for k := range params {
		switch k {
		case "env", "host", "commit", "sha256":
		default:
			return AgentdParams{}, fmt.Errorf("deploy_agentd takes exactly env, host, commit and sha256, not %q", k)
		}
	}
	p := AgentdParams{Env: str("env"), Host: str("host"), Commit: str("commit"), SHA256: str("sha256")}
	switch {
	case p.Env != "prod":
		return p, errors.New("deploy_agentd is PROD only: env is prod")
	case !hostRE.MatchString(p.Host):
		return p, fmt.Errorf("deploy_agentd: host %q is not a host id", p.Host)
	case !commitRE.MatchString(p.Commit):
		return p, errors.New("deploy_agentd: commit is 40 lower-case hex")
	case !sha256RE.MatchString(p.SHA256):
		return p, errors.New("deploy_agentd: sha256 is 64 lower-case hex")
	}
	return p, nil
}

// ParseTargets reads host=user@address pairs, comma separated; an empty
// value is no host.
func ParseTargets(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, target, ok := strings.Cut(part, "=")
		host = strings.TrimSpace(host)
		if !ok || !hostRE.MatchString(host) {
			return nil, fmt.Errorf("%s: %q is not host=user@address", TargetsEnv, part)
		}
		clean, err := sanitizeDeployRemote(target)
		if err != nil {
			return nil, fmt.Errorf("%s: host %s: %w", TargetsEnv, host, err)
		}
		if strings.HasPrefix(clean, "-") || !strings.Contains(clean, "@") {
			return nil, fmt.Errorf("%s: host %s: the target is user@address", TargetsEnv, host)
		}
		if _, dup := out[host]; dup {
			return nil, fmt.Errorf("%s: host %s is named twice", TargetsEnv, host)
		}
		out[host] = clean
	}
	return out, nil
}

// TargetsFromEnv is ParseTargets of AGENTD_DEPLOY_TARGETS.
func TargetsFromEnv() (map[string]string, error) {
	return ParseTargets(os.Getenv(TargetsEnv))
}

// HostControl is the agent runtime's reach the deploy needs: set a host
// aside and back, count the attempts running on it, read the agentd commit
// its claims report ("" while none).
type HostControl interface {
	SetAside(ctx context.Context, host, reason string) (controlID string, err error)
	SetBack(ctx context.Context, controlID string) error
	Running(ctx context.Context, host string) (int, error)
	Build(ctx context.Context, host string) (string, error)
}

// Runner runs a command on target over ssh; err is non-nil when it did not
// exit 0, output is what it printed.
type Runner interface {
	Run(ctx context.Context, target string, args ...string) (output string, err error)
}

// SSHRunner is the real Runner: ssh in BatchMode, so a missing key fails at
// once instead of asking for a password.
type SSHRunner struct{}

func (SSHRunner) Run(ctx context.Context, target string, args ...string) (string, error) {
	argv := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "StrictHostKeyChecking=yes", target}, args...)
	out, err := exec.CommandContext(ctx, "ssh", argv...).CombinedOutput()
	return clipOutput(string(out)), err
}

func clipOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		s = "…" + s[len(s)-4000:]
	}
	return s
}

// ErrNotPublished is a package's answer when no build of the commit is
// published.
var ErrNotPublished = errors.New("agentdeploy: no agentd build is published for the commit")

// Package reads the sha256 the Gitea package publishes for an agentd build:
// URL is the .sha256 file's address with {commit} in it, Auth the
// platform's read-only Gitea account (nil: anonymous).
type Package struct {
	URL    string
	Auth   func(ctx context.Context) (user, pass string, err error)
	Client *http.Client
}

// SHA256 is the published sha256 of commit's build: the first field of the
// .sha256 file (sha256sum's form). A 404 is ErrNotPublished.
func (p Package) SHA256(ctx context.Context, commit string) (string, error) {
	if !commitRE.MatchString(commit) {
		return "", errors.New("agentdeploy: commit is 40 lower-case hex")
	}
	if !strings.Contains(p.URL, "{commit}") {
		return "", errors.New("agentdeploy: the package URL has no {commit}")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.ReplaceAll(p.URL, "{commit}", commit), nil)
	if err != nil {
		return "", err
	}
	if p.Auth != nil {
		user, pass, err := p.Auth(ctx)
		if err != nil {
			return "", err
		}
		req.SetBasicAuth(user, pass)
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agentdeploy: read the package's sha256: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotPublished
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("agentdeploy: the package's sha256 answered HTTP %d", resp.StatusCode)
	}
	line, err := bufio.NewReader(io.LimitReader(resp.Body, 4096)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || !sha256RE.MatchString(strings.ToLower(fields[0])) {
		return "", errors.New("agentdeploy: the package's .sha256 does not start with a sha256")
	}
	return strings.ToLower(fields[0]), nil
}

// Agentd runs deploy_agentd. Targets and PackageSHA256 are configuration and
// the Gitea read; Now, Sleep and Poll are the clock (nil and zero: the real
// one, every 10 seconds); IdleWait and BuildWait zero are AgentdIdleWait and
// AgentdBuildWait.
type Agentd struct {
	Targets       func() (map[string]string, error)
	PackageSHA256 func(ctx context.Context, commit string) (string, error)
	Hosts         HostControl
	SSH           Runner
	Store         *Store

	Now                       func() time.Time
	Sleep                     func(ctx context.Context, d time.Duration) error
	Poll, IdleWait, BuildWait time.Duration
}

// Run is the executor: it returns what it did, or why it failed and what it
// left behind.
func (d *Agentd) Run(ctx context.Context, params map[string]any) (map[string]any, error) {
	p, err := ParseAgentdParams(params)
	if err != nil {
		return nil, err
	}
	if d.Targets == nil || d.PackageSHA256 == nil || d.Hosts == nil || d.SSH == nil || d.Store == nil {
		return nil, errors.New("deploy_agentd is not wired on this platform")
	}
	targets, err := d.Targets()
	if err != nil {
		return nil, err
	}
	target, ok := targets[p.Host]
	if !ok {
		return nil, fmt.Errorf("host %s has no ssh target in %s: refused", p.Host, TargetsEnv)
	}
	published, err := d.PackageSHA256(ctx, p.Commit)
	switch {
	case errors.Is(err, ErrNotPublished):
		return nil, fmt.Errorf("no agentd build of %s is published: refused", p.Commit)
	case err != nil:
		return nil, err
	case published != p.SHA256:
		return nil, fmt.Errorf("the package's sha256 of %s is %s, not the card's %s: refused", p.Commit, published, p.SHA256)
	}
	job := &Job{ID: "agentd-" + p.Host + "-" + p.Commit[:12] + "-" + d.now().UTC().Format("20060102T150405Z"),
		Remote: target, Role: "agentd", StartedAt: d.now().UTC()}
	if err := d.Store.Begin(job); err != nil {
		return nil, err
	}
	out := map[string]any{"job": job.ID, "host": p.Host, "commit": p.Commit, "sha256": p.SHA256}
	res, err := d.deploy(ctx, p, target, out)
	d.Store.Finish(err)
	return res, err
}

func (d *Agentd) deploy(ctx context.Context, p AgentdParams, target string, out map[string]any) (map[string]any, error) {
	logf := d.Store.Logf
	short := p.Commit[:12]
	control, err := d.Hosts.SetAside(ctx, p.Host, "deploy_agentd: install agentd "+short+"; the platform sets the host back when it is done")
	if err != nil {
		return out, fmt.Errorf("set host %s aside: %w", p.Host, err)
	}
	out["control_id"] = control
	logf("host %s set aside (control %s)", p.Host, control)
	back := func(why string) error {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), setBackTimeout)
		defer cancel()
		if err := d.Hosts.SetBack(bctx, control); err != nil {
			logf("could not set host %s back (%s): %v", p.Host, why, err)
			return fmt.Errorf("could not set host %s back: lift control %s: %w", p.Host, control, err)
		}
		logf("host %s set back (%s)", p.Host, why)
		return nil
	}

	if n, err := d.waitIdle(ctx, p.Host); err != nil || n > 0 {
		why := fmt.Sprintf("%d attempt(s) still run on host %s after %s", n, p.Host, d.idleWait())
		if err != nil {
			why = fmt.Sprintf("could not tell whether attempts run on host %s: %v", p.Host, err)
		}
		logf("%s", why)
		return out, errors.Join(errors.New(why+"; nothing was installed"), back("no deploy"))
	}
	logf("no attempt runs on host %s", p.Host)

	uctx, cancel := context.WithTimeout(ctx, updateTimeout)
	output, err := d.SSH.Run(uctx, target, "sudo", "-n", "agentd-update", p.Commit, p.SHA256)
	cancel()
	out["update_output"] = output
	if err != nil {
		// agentd-update swaps the binary only when it succeeds: a failed
		// run left the build the host had.
		logf("agentd-update failed: %v", err)
		return out, errors.Join(fmt.Errorf("agentd-update on %s failed: %w: %s", p.Host, err, lastLine(output)), back("update failed"))
	}
	logf("agentd-update %s ran on %s", short, p.Host)

	got, err := d.waitBuild(ctx, p.Host, p.Commit)
	if err == nil && got == p.Commit {
		logf("host %s reports agentd %s", p.Host, short)
		if err := back("deployed"); err != nil {
			return out, fmt.Errorf("agentd %s is deployed on %s, but %w", short, p.Host, err)
		}
		return out, nil
	}
	why := fmt.Sprintf("host %s did not report agentd %s within %s (it reports %q)", p.Host, short, d.buildWait(), got)
	if err != nil {
		why = fmt.Sprintf("could not read the build host %s reports: %v", p.Host, err)
	}
	logf("%s; rolling back", why)
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), updateTimeout)
	routput, rerr := d.SSH.Run(rctx, target, "sudo", "-n", "agentd-update", "--rollback")
	cancel()
	out["rollback_output"] = routput
	if rerr != nil {
		// The host runs a build nobody checked: it stays aside until the
		// Owner looks at it and lifts the control.
		logf("rollback failed: %v; host %s stays aside (control %s)", rerr, p.Host, control)
		return out, fmt.Errorf("%s; the rollback failed too (%v: %s), so host %s stays set aside: lift control %s once it is fixed",
			why, rerr, lastLine(routput), p.Host, control)
	}
	logf("rolled back on %s", p.Host)
	return out, errors.Join(errors.New(why+"; agentd-update --rollback restored the previous build"), back("rolled back"))
}

// waitIdle polls the attempts running on host until none does or IdleWait
// passes; it returns the last count. A read error ends the wait only when
// the time is up.
func (d *Agentd) waitIdle(ctx context.Context, host string) (int, error) {
	deadline := d.now().Add(d.idleWait())
	for {
		n, err := d.Hosts.Running(ctx, host)
		if err == nil && n == 0 {
			return 0, nil
		}
		if !d.now().Before(deadline) {
			return n, err
		}
		if serr := d.sleep(ctx, d.poll()); serr != nil {
			return n, serr
		}
	}
}

// waitBuild polls the build host's claims report until it is commit or
// BuildWait passes; it returns the last one read.
func (d *Agentd) waitBuild(ctx context.Context, host, commit string) (string, error) {
	deadline := d.now().Add(d.buildWait())
	for {
		got, err := d.Hosts.Build(ctx, host)
		if err == nil && got == commit {
			return got, nil
		}
		if !d.now().Before(deadline) {
			return got, err
		}
		if serr := d.sleep(ctx, d.poll()); serr != nil {
			return got, serr
		}
	}
}

func (d *Agentd) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Agentd) sleep(ctx context.Context, dur time.Duration) error {
	if d.Sleep != nil {
		return d.Sleep(ctx, dur)
	}
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (d *Agentd) poll() time.Duration {
	if d.Poll > 0 {
		return d.Poll
	}
	return agentdPoll
}

func (d *Agentd) idleWait() time.Duration {
	if d.IdleWait > 0 {
		return d.IdleWait
	}
	return AgentdIdleWait
}

func (d *Agentd) buildWait() time.Duration {
	if d.BuildWait > 0 {
		return d.BuildWait
	}
	return AgentdBuildWait
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
