package landinggit

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Subject is a commit and the first line of its message.
type Subject struct {
	SHA, Subject string
}

// FirstParents reads the first-parent commits of base..p, newest first, at
// most max of them, with their subjects: the landings between two commits of
// main, for a red full CI run's goal (controller/fullci.go). Reads only.
func (w *Workspace) FirstParents(ctx context.Context, base, p string, max int) ([]Subject, error) {
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(p) || max < 1 {
		return nil, errors.New("invalid first-parent range")
	}
	log, err := w.local(ctx, "log", "--first-parent", "-n", strconv.Itoa(max), "--format=%H%x00%s%x00", base+".."+p, "--")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(log, "\x00")
	out := make([]Subject, 0, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		s := Subject{SHA: strings.TrimSpace(parts[i]), Subject: parts[i+1]}
		if !fullSHA.MatchString(s.SHA) {
			return nil, errors.New("malformed first-parent history")
		}
		out = append(out, s)
	}
	return out, nil
}
