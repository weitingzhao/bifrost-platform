package agentd

import (
	"context"
	"os/exec"
	"slices"
	"strings"
)

// scopedVerification keeps task-specific checks and repository rules. Unknown
// paths (including shared configuration and build inputs) require both stacks.
// A missing parent or unreadable diff also keeps every check.
func scopedVerification(ctx context.Context, spec TaskSpec) [][]string {
	if spec.Repo != "bifrost-platform" {
		return spec.Verify
	}
	c := exec.CommandContext(ctx, "git", "diff", "--name-only", "--no-renames", "-z", "HEAD^", "HEAD")
	b, err := c.Output()
	if err != nil {
		return spec.Verify
	}
	return selectVerification(spec.Verify, strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00"))
}

func selectVerification(commands [][]string, paths []string) [][]string {
	goOnly, consoleOnly := len(paths) > 0, len(paths) > 0
	for _, p := range paths {
		goOnly = goOnly && strings.HasPrefix(p, "api/") && (strings.HasSuffix(p, ".go") || strings.Contains(p, "/testdata/"))
		consoleOnly = consoleOnly && strings.HasPrefix(p, "console/")
	}
	if !goOnly && !consoleOnly {
		return commands
	}
	var selected [][]string
	for _, c := range commands {
		skip := false
		if goOnly {
			for _, standard := range [][]string{{"npm", "--prefix", "console", "run", "lint"}, {"npm", "--prefix", "console", "test"}, {"npm", "--prefix", "console", "run", "build"}} {
				skip = skip || slices.Equal(c, standard)
			}
		}
		if consoleOnly {
			for _, standard := range [][]string{{"go", "-C", "api", "build", "./..."}, {"go", "-C", "api", "vet", "./..."}, {"go", "-C", "api", "run", "./cmd/agentrt-check", "changed"}} {
				skip = skip || slices.Equal(c, standard)
			}
		}
		if !skip {
			selected = append(selected, c)
		}
	}
	return selected
}
