package releasepolicy

import (
	"fmt"
	"regexp"
	"strings"
)

// Additive DDL (ADR §5, 2026-10-08 revision): adding a table, adding a column
// and building an index concurrently may ship on the policy. Dropping,
// renaming, changing a type or a constraint, rewriting rows, or changing
// grants and owners stays tier D. The check is textual and errs toward the
// Owner: any removed or rewritten line, and any added line that names one of
// these, is not additive.
var (
	ddlForbidden = regexp.MustCompile(`\b(DROP|TRUNCATE|RENAME|REVOKE|GRANT|REPLACE|OWNER\s+TO|ALTER\s+COLUMN|ALTER\s+TYPE|SET\s+DATA\s+TYPE|SET\s+NOT\s+NULL|ADD\s+CONSTRAINT|DELETE\s+FROM|INSERT\s+INTO|UPDATE\s+\S+\s+SET)\b`)
	ddlIndex     = regexp.MustCompile(`\bCREATE\s+(UNIQUE\s+)?INDEX\b`)
	ddlAddColumn = regexp.MustCompile(`\bADD\s+COLUMN\b`)
)

// ClassifyDDL compares a DDL file before and after a release. ok is true when
// the change only adds; otherwise why names the first line that does not.
// A file that did not exist before is compared against nothing.
func ClassifyDDL(before, after string) (ok bool, why string) {
	have := map[string]int{}
	for _, l := range ddlLines(before) {
		have[l]++
	}
	var added []string
	for _, l := range ddlLines(after) {
		if have[l] > 0 {
			have[l]--
			continue
		}
		added = append(added, l)
	}
	for l, n := range have {
		if n > 0 {
			return false, fmt.Sprintf("removes or rewrites %q", clip(l))
		}
	}
	for _, l := range added {
		up := strings.ToUpper(l)
		if m := ddlForbidden.FindString(up); m != "" {
			return false, fmt.Sprintf("adds %s in %q", m, clip(l))
		}
		if ddlIndex.MatchString(up) && !strings.Contains(up, "CONCURRENTLY") {
			return false, fmt.Sprintf("builds an index without CONCURRENTLY in %q", clip(l))
		}
		if ddlAddColumn.MatchString(up) && strings.Contains(up, "NOT NULL") && !strings.Contains(up, "DEFAULT") {
			return false, fmt.Sprintf("adds a NOT NULL column without a DEFAULT in %q", clip(l))
		}
	}
	return true, ""
}

// ddlLines drops blank and comment-only lines and surrounding whitespace.
func ddlLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "--") || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// dbStep is the front matter of one db-steps.d file, read the way
// bifrost-trade-infra scripts/release/release_tool.py read_step reads it.
type dbStep struct {
	id, when   string
	envs, done []string
}

var frontMatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---`)

func parseDBStep(text string) (dbStep, bool) {
	m := frontMatter.FindStringSubmatch(strings.ReplaceAll(text, "\r\n", "\n"))
	if m == nil {
		return dbStep{}, false
	}
	meta := map[string]string{}
	for _, line := range strings.Split(m[1], "\n") {
		line, _, _ = strings.Cut(line, "#")
		if k, v, found := strings.Cut(line, ":"); found {
			meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	s := dbStep{id: meta["id"], when: meta["when"], envs: strings.Fields(meta["envs"]), done: strings.Fields(meta["done"])}
	if s.id == "" || len(s.envs) == 0 || (s.when != "before" && s.when != "after") {
		return dbStep{}, false
	}
	return s, true
}

func (s dbStep) pendingBefore(env string) bool {
	return s.when == "before" && contains(s.envs, env) && !contains(s.done, env)
}
