package releases

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// followFresh is how long one answer of Follow is used before the records are
// read again: plans are built for every evidence read, and the release moves
// at most every twenty minutes.
const followFresh = time.Minute

// Follow gives, for a repository, its commit in the current release of lane
// and env, and where that came from ("stg release <run>"): what a unit plan's
// pin with advance stg follows (TD-588). While the release is not settled (a
// delivery in flight) or cannot be read, it keeps the last commit it gave, so
// the plans of one commit do not change back and forth during a release; ""
// before it has read one.
func (s *Service) Follow(lane, env string) func(repo string) (string, string) {
	var (
		mu   sync.Mutex
		rec  Record
		read time.Time
		have bool
	)
	return func(repo string) (string, string) {
		mu.Lock()
		defer mu.Unlock()
		if !have || s.now().Sub(read) >= followFresh {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			got, err := s.Current(ctx, lane, env)
			cancel()
			read = s.now()
			if err != nil {
				slog.Warn("releases: follow: the current release is not read; the last one holds",
					"lane", lane, "env", env, "had_one", have, "error", err)
			} else {
				rec, have = got, true
			}
		}
		if !have {
			return "", ""
		}
		b, ok := rec.Repos[repo]
		if !ok || !fullSHA.MatchString(b.SHA) {
			return "", ""
		}
		return b.SHA, env + " release " + rec.Run
	}
}
