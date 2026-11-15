package releasepolicy

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A release that changes a trust anchor may pass on a review record (the
// policy's trust_anchor_reviewed and reviewable_repos, Owner 2026-11-14):
// someone who wrote none of those changes read every changed trust-anchor
// file at exactly the commit being released, and recorded that it passed.
// Whatever cannot be established is a reason, so the answer stays the
// refusal a policy without the condition gives.

// History lists the commits a release brings in.
type History interface {
	// Commits is every commit next has and old lacks, each with its whole
	// message and the files it says it touched. old must be an ancestor.
	Commits(ctx context.Context, repo, old, next string) ([]Commit, error)
}

// Commit is one commit of a release's range.
type Commit struct {
	SHA, Message string
	Files        []string
}

// Reviews reads the review records of one commit of a repo, oldest first.
type Reviews interface {
	Reviews(ctx context.Context, repo, commit string) ([]Review, error)
}

// Review is one review record: who read which files of the commit and what
// they concluded.
type Review struct {
	Reviewer string
	Files    []string
	Passed   bool
	NotRead  string
	At       time.Time
}

var sessionTrailer = regexp.MustCompile(`(?mi)^Claude-Session:[ \t]*(\S.*?)[ \t]*$`)

// SessionTrailers is every Claude-Session value of a commit message.
func SessionTrailers(message string) []string {
	var out []string
	for _, m := range sessionTrailer.FindAllStringSubmatch(strings.ReplaceAll(message, "\r\n", "\n"), -1) {
		out = append(out, m[1])
	}
	return out
}

// sessionID is the form two session ids are compared in: the first word,
// lower case. A trailer line may carry more after the id, and the header a
// reviewer is known by may spell the same id in another case; neither makes
// the same session two people (independent review of 4bf04932, 2026-11-15,
// finding A).
func sessionID(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return strings.ToLower(f[0])
}

// isTestFile: a Go test file, or anything under a testdata directory.
func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, "testdata/") || strings.Contains(path, "/testdata/")
}

func withoutTests(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !isTestFile(f) {
			out = append(out, f)
		}
	}
	return out
}

// anchorReview looks for a review record that covers the trust-anchor files
// need of old..next. why is empty when one does; reviewer is then who.
// cut says the commits' own file lists may be cut short: then every commit
// of the range counts as one that touched a trust anchor.
func (e *Engine) anchorReview(ctx context.Context, repo, old, next string, need []string, cut bool) (reviewer, why string) {
	if e.d.History == nil || e.d.Reviews == nil {
		return "", "no reader for commit messages or review records"
	}
	commits, err := e.d.History.Commits(ctx, repo, old, next)
	if err != nil {
		return "", "cannot read the commits: " + err.Error()
	}
	if len(commits) == 0 {
		return "", "the mirror lists no commits"
	}
	wanted := map[string]bool{}
	for _, f := range need {
		wanted[f] = true
	}
	authors, accounted := map[string]bool{}, map[string]bool{}
	for _, c := range commits {
		touches := cut
		for _, f := range c.Files {
			if wanted[f] {
				touches, accounted[f] = true, true
			}
		}
		if !touches {
			continue
		}
		sessions := SessionTrailers(c.Message)
		if len(sessions) == 0 {
			return "", "commit " + short(c.SHA) + " touches a trust anchor and has no Claude-Session trailer"
		}
		for _, s := range sessions {
			authors[sessionID(s)] = true
		}
	}
	if !cut {
		// A changed file no listed commit names has an author nobody read.
		for _, f := range need {
			if !accounted[f] {
				return "", "no listed commit accounts for " + f + ", so its author is unknown"
			}
		}
	}

	rows, err := e.d.Reviews.Reviews(ctx, repo, next)
	if err != nil {
		return "", "cannot read the review records: " + err.Error()
	}
	// A reviewer's newest record is their verdict.
	newest := map[string]Review{}
	for _, r := range rows {
		if who := sessionID(r.Reviewer); who != "" {
			newest[who] = r
		}
	}
	who := make([]string, 0, len(newest))
	for name := range newest {
		who = append(who, name)
	}
	sort.Strings(who)
	for _, name := range who {
		if !newest[name].Passed {
			return "", "the review of " + short(next) + " by " + name + " failed"
		}
	}
	var whys []string
	for _, name := range who {
		if authors[name] {
			whys = append(whys, "the reviewer "+name+" is an author")
			continue
		}
		read := map[string]bool{}
		for _, f := range newest[name].Files {
			read[strings.TrimLeft(strings.TrimSpace(f), "/")] = true
		}
		var missing []string
		for _, f := range need {
			if !read[f] {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			whys = append(whys, "no review of "+short(next)+" covers: "+listed(missing)+" (by "+name+")")
			continue
		}
		return name, ""
	}
	if len(whys) == 0 {
		return "", "no review of " + short(next) + " covers: " + listed(need)
	}
	return "", strings.Join(whys, "; ")
}
