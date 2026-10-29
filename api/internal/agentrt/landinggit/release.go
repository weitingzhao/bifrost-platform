package landinggit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// LandedOperation accepts one exact, uncontinued PROD trailer in the terminal
// trailer area. A quoted marker in the commit body confers no authority.
func LandedOperation(message string) string {
	op, count, previous := "", 0, false
	for _, line := range strings.Split(historyTrailers(message), "\n") {
		if previous && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			return ""
		}
		key, value, _ := strings.Cut(line, ":")
		previous = strings.EqualFold(key, "Landed-By")
		if previous {
			count++
			value = strings.TrimSpace(value)
			if !strings.HasPrefix(value, "agentrt-prod/") {
				return ""
			}
			op = strings.TrimPrefix(value, "agentrt-prod/")
		}
	}
	if count != 1 || strings.ContainsAny(op, " \t\r\n") {
		return ""
	}
	return op
}

// ReleaseRange reads every commit, including unauthorized ones. A contiguous
// single-parent chain from base to p proves ancestry without interpreting a
// failed merge-base command as a negative answer. The store checks the chain
// again, including its endpoints. It never uses Deployed's boolean projection.
func (w *Workspace) ReleaseRange(ctx context.Context, base, p string) ([]landing.ReleaseCommit, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if w.git.Repository() != "bifrost-platform" || !fullSHA.MatchString(base) || !fullSHA.MatchString(p) {
		return nil, errors.New("invalid STG release range")
	}
	shallow, err := w.local(ctx, "rev-parse", "--is-shallow-repository")
	if err != nil || strings.TrimSpace(shallow) != "false" {
		return nil, errors.New("incomplete release history")
	}
	log, err := w.local(ctx, "log", "--first-parent", "--reverse", "--format=%H%x00%P%x00%B%x00", base+".."+p, "--")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(log, "\x00")
	if len(parts)%3 != 1 || strings.TrimSpace(parts[len(parts)-1]) != "" {
		return nil, errors.New("malformed release history")
	}
	commits := make([]landing.ReleaseCommit, 0, len(parts)/3)
	for i := 0; i+2 < len(parts); i += 3 {
		c := landing.ReleaseCommit{SHA: strings.TrimSpace(parts[i]), Parents: strings.Fields(parts[i+1]), Message: parts[i+2]}
		if !fullSHA.MatchString(c.SHA) {
			return nil, errors.New("invalid release commit")
		}
		// Unlanded and merge commits remain in the range, so the gate refuses
		// them explicitly instead of silently filtering them out.
		tasks := taskTrailer.FindAllStringSubmatch(historyTrailers(c.Message), -1)
		if len(c.Parents) == 1 && len(tasks) == 1 && LandedOperation(c.Message) != "" {
			var single bool
			single, c.Changes, err = w.Result(ctx, tasks[0][1], c.Parents[0], c.SHA)
			if err != nil || !single {
				return nil, errors.New("unreadable release commit changes")
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}
