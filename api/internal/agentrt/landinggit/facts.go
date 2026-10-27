package landinggit

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Result reads the actual tree delta, including both rename paths, modes,
// binary markers and added lines. No executor-provided summary is trusted.
func (w *Workspace) Result(ctx context.Context, task, base, a string) (bool, []rules.LandingChange, error) {
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(a) {
		return false, nil, errors.New("invalid result SHA")
	}
	parents, e := w.local(ctx, "rev-list", "--parents", "-n", "1", a)
	if e != nil {
		return false, nil, e
	}
	p := strings.Fields(parents)
	if len(p) != 2 || p[1] != base {
		return false, nil, nil
	}
	msg, e := w.local(ctx, "show", "-s", "--format=%B", a)
	if e != nil {
		return false, nil, e
	}
	tasks, ids := taskTrailer.FindAllStringSubmatch(msg, -1), changeTrailer.FindAllStringSubmatch(msg, -1)
	if len(tasks) != 1 || tasks[0][1] != task || len(ids) != 1 {
		return false, nil, errors.New("invalid result lineage")
	}
	raw, e := w.local(ctx, "diff", "--raw", "-z", "--no-abbrev", "--find-renames", base, a, "--")
	if e != nil {
		return false, nil, e
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	changes := []rules.LandingChange{}
	for i := 0; i < len(fields) && fields[i] != ""; {
		meta := strings.Fields(fields[i])
		i++
		if len(meta) != 5 || i >= len(fields) {
			return false, nil, errors.New("malformed raw diff")
		}
		path := fields[i]
		i++
		c := rules.LandingChange{OldPath: path, NewPath: path, OldMode: strings.TrimPrefix(meta[0], ":"), NewMode: meta[1]}
		switch meta[4][0] {
		case 'A':
			c.OldPath, c.OldMode = "", ""
		case 'D':
			c.NewPath, c.NewMode = "", ""
		case 'R':
			if i >= len(fields) {
				return false, nil, errors.New("malformed rename")
			}
			c.NewPath = fields[i]
			i++
		case 'M', 'T':
		default:
			return false, nil, errors.New("unsupported diff status")
		}
		paths := []string{}
		if c.OldPath != "" {
			paths = append(paths, c.OldPath)
		}
		if c.NewPath != "" && c.NewPath != c.OldPath {
			paths = append(paths, c.NewPath)
		}
		// --no-renames produces one simple numstat row per side. This also works
		// for filenames containing tabs/newlines because rows are NUL separated.
		args := append([]string{"diff", "--numstat", "-z", "--no-renames", base, a, "--"}, paths...)
		stats, e := w.local(ctx, args...)
		if e != nil {
			return false, nil, e
		}
		for _, row := range strings.Split(stats, "\x00") {
			if row == "" {
				continue
			}
			f := strings.SplitN(row, "\t", 3)
			if len(f) != 3 {
				return false, nil, errors.New("malformed numstat")
			}
			if f[0] == "-" || f[1] == "-" {
				c.Binary = true
				continue
			}
			add, e := strconv.Atoi(f[0])
			if e != nil {
				return false, nil, e
			}
			del, e := strconv.Atoi(f[1])
			if e != nil {
				return false, nil, e
			}
			c.Added += add
			c.Deleted += del
		}
		// Use rename detection for the actual addition count, so a pure rename
		// does not count all pre-existing lines as newly added display content.
		if strings.HasPrefix(meta[4], "R") {
			stats, e = w.local(ctx, append([]string{"diff", "--numstat", "-z", "--find-renames", base, a, "--"}, paths...)...)
			if e != nil {
				return false, nil, e
			}
			f := strings.SplitN(stats, "\t", 3)
			if len(f) != 3 {
				return false, nil, errors.New("malformed rename numstat")
			}
			if f[0] == "-" || f[1] == "-" {
				c.Binary = true
			} else {
				c.Added, e = strconv.Atoi(f[0])
				if e != nil {
					return false, nil, e
				}
				c.Deleted, e = strconv.Atoi(f[1])
				if e != nil {
					return false, nil, e
				}
			}
		}
		diff, e := w.local(ctx, append([]string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--unified=0", base, a, "--"}, paths...)...)
		if e != nil {
			return false, nil, e
		}
		var added strings.Builder
		inHunk := false
		for _, line := range strings.Split(diff, "\n") {
			if strings.HasPrefix(line, "@@ ") {
				inHunk = true
				continue
			}
			if strings.HasPrefix(line, "diff --git ") {
				inHunk = false
			}
			if inHunk && strings.HasPrefix(line, "+") {
				added.WriteString(line[1:])
				added.WriteByte('\n')
			}
		}
		c.AddedText = added.String()
		changes = append(changes, c)
	}
	return true, changes, nil
}

// revertLine is git's revert line, of a commit ("X.") or of a merge ("X, reversing").
var revertLine = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{40})(?:\.\s*$|,\s)`)

// History scans complete local history, preserving both ever-present and
// currently-effective evidence. It never calls the effective-only MainResults.
func (w *Workspace) History(ctx context.Context, task, a, snapshot string) (landing.MainHistory, error) {
	unknown := landing.MainHistory{State: landing.Unknown}
	if !fullSHA.MatchString(a) {
		return unknown, errors.New("invalid accepted SHA")
	}
	tip := "refs/remotes/origin/main"
	if snapshot != "" {
		if !fullSHA.MatchString(snapshot) {
			return unknown, errors.New("invalid snapshot SHA")
		}
		tip = snapshot
	}
	shallow, e := w.local(ctx, "rev-parse", "--is-shallow-repository")
	if e != nil || strings.TrimSpace(shallow) != "false" {
		return unknown, errors.New("incomplete history")
	}
	msg, e := w.local(ctx, "show", "-s", "--format=%B", a)
	if e != nil {
		return unknown, e
	}
	log, e := w.local(ctx, "log", "--topo-order", "--format=%H%x00%P%x00%B%x00", tip, "--")
	if e != nil {
		return unknown, e
	}
	parts := strings.Split(log, "\x00")
	commits := []HistoryCommit{}
	for i := 0; i+2 < len(parts); i += 3 {
		commits = append(commits, HistoryCommit{SHA: strings.TrimSpace(parts[i]), Parents: strings.Fields(parts[i+1]), Message: parts[i+2]})
	}
	return EvaluateHistory(task, a, msg, commits, true)
}
