package actions

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// D10Paths is what classifying an owner_run_command needs to know about D10.
type D10Paths struct {
	// Locked is spine decisions[id=D10].status != UNLOCKED.
	Locked bool
	// Known is false when the Owner-signed policy cannot be read.
	Known bool
	// Patterns are the globs of the signed no_d10_paths rule, every repo.
	Patterns []string
	// Match is the release policy's glob matcher (releasepolicy.PathMatches).
	Match func(path, pattern string) bool
}

// Both values come from where every other D10 check reads them: the lock from
// the spine, the paths from the signed release policy. Nothing here is a
// second copy.
var (
	d10Mu    sync.RWMutex
	d10Paths func(ctx context.Context) D10Paths
)

// SetD10Paths installs the lookup used to classify owner_run_command. Only
// platform-api installs it. A process without it (the operator plane has no
// cluster to read the policy from) classifies the action as before, tier D.
func SetD10Paths(fn func(ctx context.Context) D10Paths) {
	d10Mu.Lock()
	d10Paths = fn
	d10Mu.Unlock()
}

// classifyOwnerRun is tier X while D10 is locked and the command names a path
// under no_d10_paths, or the signed paths cannot be read. Tier X is refused
// when the card is requested and again when it is claimed for execution.
//
// This reads the command's text; it is not a shell parser. It catches a
// command that names a D10 file or a directory above one. A command that
// reaches the trade system without naming such a path is stopped at the
// cluster, by the admission policy bound to the runner's identity.
func classifyOwnerRun(ctx context.Context, params map[string]any) Tier {
	d10Mu.RLock()
	fn := d10Paths
	d10Mu.RUnlock()
	if fn == nil {
		return TierD
	}
	d := fn(ctx)
	if !d.Locked {
		return TierD
	}
	if !d.Known || d.Match == nil {
		return TierX
	}
	if D10PathHit(str(params["command"]), d.Patterns, d.Match) != "" {
		return TierX
	}
	return TierD
}

var notPathChar = regexp.MustCompile(`[^A-Za-z0-9_./*?-]+`)

// D10PathHit returns the first pattern a command's text touches, or "".
// A word touches a pattern when it, or any trailing part of it after a "/"
// (so a repository name or an absolute prefix in front does not hide it),
// matches the glob, or is a directory of two or more segments above the
// pattern's fixed part.
func D10PathHit(command string, patterns []string, match func(path, pattern string) bool) string {
	for _, word := range notPathChar.Split(command, -1) {
		word = strings.Trim(word, "./")
		if !strings.Contains(word, "/") && !strings.Contains(word, ".") {
			continue
		}
		for i := 0; i < len(word); i++ {
			if i > 0 && word[i-1] != '/' {
				continue
			}
			tail := word[i:]
			for _, p := range patterns {
				if match(tail, p) {
					return p
				}
				fixed := p
				if j := strings.IndexAny(fixed, "*?"); j >= 0 {
					fixed = fixed[:j]
				}
				if strings.Count(tail, "/") >= 1 && strings.HasPrefix(fixed, tail+"/") {
					return p
				}
			}
		}
	}
	return ""
}
