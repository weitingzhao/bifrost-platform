package store

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Core work (Owner 2026-10-19 「核心字段 A」: a plan may mark a task core; the
// chief of staff judges which by the rules; routing takes core tasks as it
// does). Core work is the runtime's own core, as design v5 names it (§0 item
// 6, the Owner 10-13): the data layer, the Redis data forms, the kernel's
// interfaces, agentd, the bus and stopping; in a repository, a change under
// one of its Core directories (repos.go). Routing gives it to the holder of
// only author_core and its review to the holder of only review_core (the
// Owner's capability facts: Claude writes it, Codex reviews it read-only);
// agentd's deterministic check verifies it as any work (route.go). Work
// beside it is not core even under those directories: test boilerplate,
// documentation, a page filled in to an agreed interface.

// CoreText is what a seat's spec says core work is, from Repos.
func CoreText() string {
	var where []string
	for _, name := range slices.Sorted(maps.Keys(Repos)) {
		if dirs := Repos[name].Core; len(dirs) > 0 {
			where = append(where, name+": "+strings.Join(withSlash(dirs), ", "))
		}
	}
	return "## Core work\n\n" +
		"Core work is the runtime's own core: the data layer, the Redis data forms, the kernel's interfaces, agentd, the bus and\n" +
		"stopping. In a repository it is a change under one of these directories (" + strings.Join(where, "; ") + "); no other\n" +
		"repository has core work. By the Owner's rules Claude writes core work and Codex reviews it, read-only; agentd's\n" +
		"deterministic check verifies it as any work. Work beside it is not core, even under those directories: test\n" +
		"boilerplate, documentation, a page filled in to an agreed interface.\n\n"
}

func withSlash(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = d + "/"
	}
	return out
}

// coreDirNamed says whether text names a path under one of dirs: the
// directory itself or a path beneath it, as a whole path (not
// "api/internal/agentdx", nor "x/api/internal/agentd").
func coreDirNamed(text string, dirs []string) bool {
	for _, d := range dirs {
		re := regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])(?:\./)?` + regexp.QuoteMeta(d) + `(?:/|[^A-Za-z0-9_.-]|\.(?:[^A-Za-z0-9_-]|$)|$)`)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// checkCore is where a task may be core: an implementation in a repository
// with core directories. A plan's task also names in its text a file it
// changes under them (checkPlannedCore).
func checkCore(role, repo string) error {
	if role != "implement" {
		return fmt.Errorf("core work is an implementation; a %s task is not core", role)
	}
	if len(Repos[repo].Core) == 0 {
		return fmt.Errorf("%s has no core work (core work is a change under the runtime's core directories); leave core out", orNone(repo))
	}
	return nil
}

func orNone(repo string) string {
	if repo == "" {
		return "a task with no repository"
	}
	return repo
}

// checkPlannedCore checks a planned task's core label: true, false or left
// out (null and every other type are refused), and, when true, a task of a
// repository with core work whose text names where it changes it.
func checkPlannedCore(at string, t PlannedTask) error {
	switch string(t.Core) {
	case "", "false":
		return nil
	case "true":
	default:
		return fmt.Errorf("%s: core is true or false, or left out; not %s", at, t.Core)
	}
	if err := checkCore("implement", t.Repo); err != nil {
		return fmt.Errorf("%s: %v", at, err)
	}
	if dirs := Repos[t.Repo].Core; !coreDirNamed(t.Text, dirs) {
		return fmt.Errorf("%s: a core task names in its text the files it changes under %s; this one names none, so it is not core "+
			"work, or its text does not say where", at, strings.Join(withSlash(dirs), ", "))
	}
	return nil
}
