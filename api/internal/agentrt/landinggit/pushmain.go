package landinggit

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// PushOutcome is how a push of main ended (CUT §2.5 step 7, protocol
// main_result.outcome).
type PushOutcome string

const (
	PushOK      PushOutcome = "ok"
	PushNonFF   PushOutcome = "non_ff"
	PushDenied  PushOutcome = "denied"
	PushUnknown PushOutcome = "unknown"
)

// PushTimeout bounds one push of main (S3, CUT §10.6 2A): the reconciler
// counts a missing R as never sent only after this and a margin, so a push
// must end, or be given up, within it. It is never retried here.
const PushTimeout = 2 * time.Minute

// PushMain pushes r to the remote's main, fast-forward only, once (CUT §2.5
// step 7). Only the lander identity can; the drill's never. The outcome is
// what the remote said: ok, non_ff (main moved: not a fast-forward), denied
// (the remote refused this identity or a hook refused the update), or
// unknown (no answer within PushTimeout, a broken connection, anything
// else). The remote's text never leaves this function: Git and servers can
// echo credentials.
func (w *Workspace) PushMain(ctx context.Context, r, m0 string) PushOutcome {
	if !w.git.lander || !fullSHA.MatchString(r) || !fullSHA.MatchString(m0) {
		return PushDenied
	}
	ctx, cancel := context.WithTimeout(ctx, PushTimeout)
	defer cancel()
	h, err := w.git.header()
	if err != nil {
		return PushDenied
	}
	// --force-with-lease pins main to m0: the push lands only if main is
	// still m0, and a plain fast-forward refusal otherwise. No --force.
	args := append(append([]string{}, isolation...), "push", "--porcelain", "--no-verify",
		"--force-with-lease=refs/heads/main:"+m0, "origin", r+":refs/heads/main")
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.dir
	cmd.Env = append(isolatedEnv(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0="+h)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return PushUnknown
	}
	return classifyPush(out.String(), stderr.String(), err)
}

// classifyPush reads git push --porcelain: a line "<flag>\t<from>:<to>\t
// <summary>" per ref. Flag " " or "+" is done; "=" is up to date (main is
// already r: treated as ok, the reconciler confirms it); "!" is rejected,
// with the reason in brackets.
func classifyPush(stdout, stderr string, err error) PushOutcome {
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || !strings.HasSuffix(parts[1], ":refs/heads/main") {
			continue
		}
		summary := ""
		if len(parts) == 3 {
			summary = parts[2]
		}
		switch parts[0] {
		case " ", "+", "=", "*":
			if err == nil {
				return PushOK
			}
		case "!":
			switch {
			case strings.Contains(summary, "non-fast-forward"), strings.Contains(summary, "fetch first"),
				strings.Contains(summary, "stale info"):
				return PushNonFF
			case strings.Contains(summary, "remote rejected"):
				return PushDenied
			}
			return PushUnknown
		}
	}
	var exit *exec.ExitError
	if err != nil && errors.As(err, &exit) {
		low := strings.ToLower(stderr)
		if strings.Contains(low, "403") || strings.Contains(low, "permission") || strings.Contains(low, "not allowed") || strings.Contains(low, "denied") {
			return PushDenied
		}
	}
	return PushUnknown
}
