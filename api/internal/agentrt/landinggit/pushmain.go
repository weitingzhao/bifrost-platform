package landinggit

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// PushOutcome is how a push of main ended (CUT §2.5 step 7, protocol
// main_result.outcome), or that it was never sent.
type PushOutcome string

const (
	PushOK      PushOutcome = "ok"
	PushNonFF   PushOutcome = "non_ff"
	PushDenied  PushOutcome = "denied"
	PushUnknown PushOutcome = "unknown"
	// PushNotSent: refused or failed before anything reached the remote
	// (not the lander, R not on M0, past the deadline, credentials
	// unreadable, git not started). Not a remote verdict: it never stops
	// the repository (review 662 R-02). The caller records unknown and the
	// reconciler ends it from main.
	PushNotSent PushOutcome = "not_sent"
)

// PushTimeout bounds one push of main (S3, CUT §10.6 2A): the reconciler
// counts a missing R as never sent only after this and a margin, so a push
// must end, or be given up, within it. It is never retried here.
const PushTimeout = 2 * time.Minute

// pushOutputLimit bounds what a push's output is read from.
const pushOutputLimit = 64 << 10

// PushMain pushes r to the remote's main, fast-forward only, once (CUT §2.5
// step 7). Only the lander identity can; the drill's never. The outcome is
// what the remote said: ok, non_ff (main moved: not a fast-forward), denied
// (the remote refused this identity or a hook refused the update), or
// unknown (no answer in time, a broken connection, anything else); or
// not_sent. The remote's text never leaves this function: Git and servers
// can echo credentials.
//
// deadline is absolute: the main_intent's time plus PushTimeout (CUT §11.4
// B1). The whole call, its local checks and the reaping of its processes
// included, ends by it (review 662 R-01): the push itself is given up
// reapDelay before it, so the group's kill and the pipes' drain fit inside.
// A call with less than that left starts nothing.
func (w *Workspace) PushMain(ctx context.Context, r, m0 string, deadline time.Time) PushOutcome {
	giveUp := deadline.Add(-reapDelay)
	if deadline.IsZero() || !time.Now().Before(giveUp) {
		return PushNotSent // past the intent's deadline: never start (review 660 B-01)
	}
	if !w.git.lander || !fullSHA.MatchString(r) || !fullSHA.MatchString(m0) {
		return PushNotSent
	}
	ctx, cancel := context.WithDeadline(ctx, giveUp)
	defer cancel()
	// R must be the replay on M0 with M0 its only parent (review 661 B-01):
	// then a main that moved past M0 can never fast-forward to R. Anything
	// else is refused before any network.
	if line, err := w.local(ctx, "rev-list", "--parents", "-n", "1", r); err != nil {
		return PushNotSent
	} else if f := strings.Fields(line); len(f) != 2 || f[0] != r || f[1] != m0 {
		return PushNotSent
	}
	h, err := w.git.header()
	if err != nil || ctx.Err() != nil {
		return PushNotSent
	}
	// One explicit refspec, no "+", no force of any kind, no tags (review 660
	// R-04): the remote takes it only as a fast-forward. R's parent is M0
	// (the gate checked main = M0 just before), so if main moved meanwhile
	// the push is refused, never laid over someone else's commit.
	args := append(append([]string{}, isolation...), "-c", "push.followTags=false",
		"push", "--porcelain", "--no-verify", "origin", r+":refs/heads/main")
	cmd := exec.Command("git", args...)
	cmd.Dir = w.dir
	cmd.Env = append(isolatedEnv(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0="+h)
	out, stderr := &limitedBuffer{max: pushOutputLimit}, &limitedBuffer{max: pushOutputLimit}
	cmd.Stdout, cmd.Stderr = out, stderr
	err = runGroup(ctx, cmd)
	switch {
	case errors.Is(err, errNotStarted):
		return PushNotSent
	case ctx.Err() != nil:
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
//     branch answer), or Git's own HTTP 403 for this identity: the
//     transport's "fatal: unable to access '<url>': The requested URL
//     returned error: 403" line, never text the remote sent (Git prints
//     that as "remote: ..." lines, review 662 R-02).
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
	if err != nil && errors.As(err, &exit) {
		for _, line := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(line, "fatal: unable to access '") && strings.HasSuffix(strings.TrimRight(line, "\r"), "': The requested URL returned error: 403") {
				return PushDenied
			}
		}
	}
	return PushUnknown
}
