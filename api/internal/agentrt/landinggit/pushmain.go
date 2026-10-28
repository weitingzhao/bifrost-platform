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
//
// deadline is absolute: the main_intent's time plus PushTimeout (CUT §11.4
// B1). A call at or past it starts nothing and returns unknown; the push is
// never given a fresh two minutes.
func (w *Workspace) PushMain(ctx context.Context, r, m0 string, deadline time.Time) PushOutcome {
	if !w.git.lander || !fullSHA.MatchString(r) || !fullSHA.MatchString(m0) {
		return PushDenied
	}
	// R must be the replay on M0 with M0 its only parent (review 661 B-01):
	// then a main that moved past M0 can never fast-forward to R. Anything
	// else is refused before any network.
	if line, err := w.local(ctx, "rev-list", "--parents", "-n", "1", r); err != nil {
		return PushDenied
	} else if f := strings.Fields(line); len(f) != 2 || f[0] != r || f[1] != m0 {
		return PushDenied
	}
	if deadline.IsZero() || !time.Now().Before(deadline) {
		return PushUnknown // past the intent's deadline: never start (review 660 B-01)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	h, err := w.git.header()
	if err != nil {
		return PushDenied
	}
	// One explicit refspec, no "+", no force of any kind, no tags (review 660
	// R-04): the remote takes it only as a fast-forward. R's parent is M0
	// (the gate checked main = M0 just before), so if main moved meanwhile
	// the push is refused, never laid over someone else's commit.
	args := append(append([]string{}, isolation...), "-c", "push.followTags=false",
		"push", "--porcelain", "--no-verify", "origin", r+":refs/heads/main")
	cmd := exec.CommandContext(ctx, "git", args...)
	killGroupOnCancel(cmd)
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
// already r: ok, the reconciler confirms it from main); "!" is rejected,
// with the reason in brackets. Only an explicit answer is a verdict (review
// 661 R-04), because denied stops the repository (B6):
//   - non_ff: "non-fast-forward" or "fetch first", from the client before
//     sending or from the remote; main moved, k+1 replays.
//   - denied: the remote's hook declined the update (Gitea's protected
//     branch answer), or HTTP 403 for this identity.
//   - unknown: everything else, a remote rejection for another reason (a
//     ref lock, a server error) included; the reconciler reads main.
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
			case strings.Contains(summary, "(non-fast-forward)"), strings.Contains(summary, "(fetch first)"):
				return PushNonFF
			case strings.HasPrefix(summary, "[remote rejected]") && strings.Contains(summary, "hook declined"):
				return PushDenied
			}
			return PushUnknown
		}
	}
	var exit *exec.ExitError
	if err != nil && errors.As(err, &exit) && strings.Contains(stderr, "The requested URL returned error: 403") {
		return PushDenied
	}
	return PushUnknown
}
