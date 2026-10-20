// Command agentd is the agent host daemon of the agent runtime (design v5
// §5.2 「agentd」): it runs under launchd on an agent host, opens no port,
// claims work from the platform, runs each attempt's executor in its own
// process group and hands the result over. One per host (a machine-wide
// lock); concurrency 1 in round 2.
//
// Configuration, all from the environment:
//
//	AGENTD_PLATFORM        the agentrt routes, e.g. https://platform.example/api/v1/agentrt
//	AGENTD_HOST            this host's id (its token is bound to it)
//	AGENTD_TOKEN_FILE      a file holding the host's agentd token (mode 0600)
//	AGENTD_TOOLS           executors here: exec:codex=openai:gpt-5,exec:cursor=cursor-mix:auto,exec:claude=anthropic:opus,exec:deterministic=local
//	AGENTD_GIT_BASE        where repositories live: <base>/<repo>.git
//	AGENTD_GIT_PUSH_BASE   optional: where attempt branches go, <base>/<repo>.git, when the
//	                       repositories work is cloned from take no push (a mirror)
//	AGENTD_GIT_TOKEN_FILE  optional: a token git sends to that base (as an http header, never on a command line)
//	AGENTD_CURSOR_KEY_FILE optional: cursor's API key (mode 0600), given to the cursor executor alone
//	AGENTD_CLAUDE_TOKEN_FILE optional: Claude's OAuth token (mode 0600), given to the claude executor alone
//	AGENTD_ROOT            workspaces, registry and lock; default ~/agent-work/agentd
//
// The first argument "launch" makes it the launcher of one executor, and
// "verify" the deterministic executor; neither is for people. "bench" runs
// the benchmark question set with one executor instead of the daemon
// (bench.go), and "exam" sits a job's qualifying exam (exam.go).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case agentd.LaunchArg:
			if len(os.Args) < 4 || os.Args[2] != "--" {
				os.Exit(125)
			}
			os.Exit(agentd.RunLauncher(os.Args[3:]))
		case "verify":
			os.Exit(agentd.RunVerify(os.Args[2:]))
		case agentd.SetupArg:
			os.Exit(agentd.RunSetup(os.Args[2:]))
		case agentd.ConfineArg:
			os.Exit(agentd.RunConfine(os.Args[2:]))
		case agentd.CheckArg:
			os.Exit(agentd.RunConfineCheck(os.Args[2:]))
		case agentd.ProbeArg:
			os.Exit(agentd.RunConfineProbe(os.Args[2:]))
		case "bench":
			os.Exit(runBench(os.Args[2:]))
		case "exam":
			os.Exit(runExam(os.Args[2:]))
		default:
			// Never the daemon by mistake: an older unit or script naming a
			// subcommand this build does not have must not start claiming
			// work (review 287 B7).
			log.Printf("agentd: unknown subcommand %q", os.Args[1])
			os.Exit(2)
		}
	}
	if err := run(); err != nil {
		log.Printf("agentd: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config()
	if err != nil {
		return err
	}
	lockPath, err := agentd.HostLockPath()
	if err != nil {
		return err
	}
	lock, err := agentd.LockHost(lockPath)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	reg, err := agentd.OpenRegistry(filepath.Join(cfg.root, "registry.json"))
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	d := &agentd.Daemon{
		Client:   &agentd.Client{Base: cfg.platform, Host: cfg.host, Token: cfg.token},
		Registry: reg,
		Root:     cfg.root,
		Self:     self,
		Remote:   func(repo string) string { return strings.TrimRight(cfg.gitBase, "/") + "/" + repo + ".git" },
		GitEnv:   cfg.gitEnv,
		Logf:     log.Printf,
	}
	d.PushRemote = remoteAt(cfg.pushBase)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	// What a previous run left is ended first, whatever this host can offer
	// now (review 164 A8).
	if err = d.Recover(ctx); err != nil {
		return err
	}
	executors, refused := agentd.Isolated(agentd.Executors(self, os.Environ(), agentd.Secrets{
		CursorAPIKey: cfg.cursorKey, ClaudeOAuthToken: cfg.claudeToken,
	}), agentd.MCPChecks)
	for _, why := range refused {
		log.Printf("agentd: %s", why)
	}
	var offered []agentd.Tool
	for _, tl := range cfg.tools {
		if _, ok := executors[tl.Executor]; ok {
			offered = append(offered, tl)
		}
	}
	if len(offered) == 0 {
		log.Printf("agentd: no executor of AGENTD_TOOLS can be offered on this host: it takes no work, and goes on handing over what it has")
	}
	d.Executors = executors
	// The write wall, proven here before any work (TD-371): an executor it
	// does not hold runs alone.
	codex := ""
	if slices.ContainsFunc(offered, func(tl agentd.Tool) bool { return tl.Executor == "exec:codex" }) {
		codex = agentd.CodexBin(os.Environ())
	}
	wall, proof := agentd.ProveWall(ctx, self, cfg.root, agentd.AllowedEnv(os.Environ()), codex)
	for _, line := range proof {
		log.Printf("agentd: write wall: %s", line)
	}
	d.Wall, d.CodexHome = wall, agentd.CodexHome(os.Environ())
	// Up to MaxSlots attempts at once (W-31 2.4 slice ③); the platform
	// leases no more than the signed concurrency rule's per_host. Each runs in
	// its own goroutine; on SIGTERM every one hands over before agentd exits.
	var runs sync.WaitGroup
	defer runs.Wait()
	var lastPrune time.Time
	for ctx.Err() == nil {
		// what a previous run left, and hand-overs that waited for the platform
		if err := d.Recover(ctx); err != nil {
			log.Printf("agentd: takeover: %v", err)
			sleep(ctx, 15*time.Second)
			continue
		}
		// The attempt branches the platform lets go (TD-383): a refusal is
		// the platform's to record, never a reason to stop.
		if time.Since(lastPrune) >= agentd.PruneEvery {
			lastPrune = time.Now()
			pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if n, err := d.Prune(pctx); err != nil {
				log.Printf("agentd: prune: %v", err)
			} else if n > 0 {
				log.Printf("agentd: deleted %d attempt branches", n)
			}
			cancel()
		}
		if id, err := d.Blocked(); err != nil || id != "" || len(offered) == 0 {
			if id != "" {
				log.Printf("agentd: the process group of %s is not gone; no new work until it is", id)
			}
			sleep(ctx, time.Minute)
			continue
		}
		// With every slot taken (or the host overloaded) the claim asks for
		// none: it still keeps the host's poll fresh, and waits its long poll.
		free := agentd.MaxSlots - d.Running()
		if free < 0 || overloaded() {
			free = 0
		}
		cl, err := d.Client.Claim(ctx, free, offered)
		if err != nil {
			log.Printf("agentd: claim: %v", err)
			sleep(ctx, 15*time.Second)
			continue
		}
		if cl == nil {
			continue
		}
		d.Hold(cl.AttemptID) // counted from now, not from when its goroutine starts
		runs.Add(1)
		safego.Go("agentd-run", func() {
			defer runs.Done()
			out, err := d.Run(ctx, cl)
			log.Printf("agentd: %s attempt %d: %s %s submitted=%v salvaged=%v err=%v",
				cl.TaskID, cl.AttemptNo, out.EndHow, out.Conclusion, out.Submitted, out.Salvaged, err)
		})
	}
	return nil
}

// remoteAt is <base>/<repo>.git, or nil for no base.
func remoteAt(base string) func(string) string {
	if base == "" {
		return nil
	}
	return func(repo string) string { return strings.TrimRight(base, "/") + "/" + repo + ".git" }
}

type settings struct {
	platform, host, token, gitBase, pushBase, root, cursorKey, claudeToken string
	gitEnv                                                                 []string
	tools                                                                  []agentd.Tool
}

func config() (settings, error) {
	s := settings{platform: os.Getenv("AGENTD_PLATFORM"), host: os.Getenv("AGENTD_HOST"), gitBase: os.Getenv("AGENTD_GIT_BASE"),
		pushBase: strings.TrimSpace(os.Getenv("AGENTD_GIT_PUSH_BASE")), root: os.Getenv("AGENTD_ROOT")}
	if s.platform == "" || s.host == "" {
		return s, errors.New("AGENTD_PLATFORM and AGENTD_HOST are required")
	}
	if s.root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return s, err
		}
		s.root = filepath.Join(home, "agent-work", "agentd")
	}
	tok, err := secret(os.Getenv("AGENTD_TOKEN_FILE"))
	if err != nil || tok == "" {
		return s, fmt.Errorf("AGENTD_TOKEN_FILE: %v", err)
	}
	s.token = tok
	s.gitEnv = append(agentd.AllowedEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=agentd "+s.host, "GIT_AUTHOR_EMAIL=agentd@"+s.host, "GIT_COMMITTER_NAME=agentd "+s.host, "GIT_COMMITTER_EMAIL=agentd@"+s.host)
	if path := os.Getenv("AGENTD_GIT_TOKEN_FILE"); path != "" {
		gt, gerr := secret(path)
		if gerr != nil {
			return s, fmt.Errorf("AGENTD_GIT_TOKEN_FILE: %v", gerr)
		}
		s.gitEnv = append(s.gitEnv, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: token "+gt)
	}
	if path := os.Getenv("AGENTD_CURSOR_KEY_FILE"); path != "" {
		if s.cursorKey, err = secret(path); err != nil || s.cursorKey == "" {
			return s, fmt.Errorf("AGENTD_CURSOR_KEY_FILE: %v", err)
		}
	}
	if path := os.Getenv("AGENTD_CLAUDE_TOKEN_FILE"); path != "" {
		if s.claudeToken, err = secret(path); err != nil || s.claudeToken == "" {
			return s, fmt.Errorf("AGENTD_CLAUDE_TOKEN_FILE: %v", err)
		}
	}
	if err = sameServer(s.gitBase, s.pushBase); err != nil {
		return s, err
	}
	if s.tools, err = tools(os.Getenv("AGENTD_TOOLS")); err != nil {
		return s, err
	}
	return s, nil
}

// sameServer: the git token goes to every remote (http.extraHeader), so the
// push base must be on the server of the git base (review 195 B).
func sameServer(gitBase, pushBase string) error {
	if pushBase == "" {
		return nil
	}
	g, gerr := url.Parse(gitBase)
	p, perr := url.Parse(pushBase)
	if gerr != nil || perr != nil || p.Host == "" || !strings.EqualFold(g.Scheme, p.Scheme) || !strings.EqualFold(g.Host, p.Host) {
		return fmt.Errorf("AGENTD_GIT_PUSH_BASE %q is not on the server of AGENTD_GIT_BASE %q", pushBase, gitBase)
	}
	return nil
}

// secret reads a token file that only its owner may read.
func secret(path string) (string, error) {
	if path == "" {
		return "", errors.New("not set")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is readable by others (mode %o)", path, info.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("%s is empty", path) // review 196 B2
	}
	return s, nil
}

// tools parses executor=provider[:model], e.g. exec:claude=anthropic:opus.
func tools(spec string) ([]agentd.Tool, error) {
	var out []agentd.Tool
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		executor, rest, ok := strings.Cut(item, "=")
		provider, model, _ := strings.Cut(rest, ":")
		if !ok || executor == "" || provider == "" {
			return nil, fmt.Errorf("AGENTD_TOOLS: %q is not executor=provider[:model]", item)
		}
		out = append(out, agentd.Tool{Executor: executor, Provider: provider, Model: model})
	}
	if len(out) == 0 {
		return nil, errors.New("AGENTD_TOOLS names no executor")
	}
	return out, nil
}

// overloaded: the one-minute load is above the number of cores (lesson 105):
// the host asks for nothing until it eases.
func overloaded() bool {
	out, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		b, xerr := execOutput("sysctl", "-n", "vm.loadavg")
		if xerr != nil {
			return false
		}
		out = []byte(strings.Trim(strings.TrimSpace(string(b)), "{} "))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	return err == nil && load > float64(runtime.NumCPU())
}

// execOutput runs a tool agentd needs (sysctl) with the allowed environment
// only, as every helper does (review 197 A1).
func execOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = agentd.AllowedEnv(os.Environ())
	return cmd.Output()
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
