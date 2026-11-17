package server

import (
	"context"
	"slices"

	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
)

// mainReader answers where main of a repository is.
type mainReader = func(ctx context.Context, repo string) (string, error)

// mainOf is the one way display readers ask where main of repo is: the bus
// ledger's last git.main.advanced when gate-git watches repo (the Gitea hook
// records a push within seconds; the workers reconcile every 5 minutes),
// else fallback (Gitea). Adding a repository to BUS_GIT_REPOS moves every
// reader of it onto the ledger; nothing else changes.
//
// Gates (release policy, head-release, migrations, the daily full CI) keep
// asking Gitea: the ledger is a hint for them, the source is the authority
// (nextgen/run-readers.md, the main-head table).
func (s *Server) mainOf(fallback mainReader) mainReader {
	var last mainReader
	if s.bus.gitLedger.Load() {
		last = func(ctx context.Context, repo string) (string, error) {
			db := s.bus.db()
			if db == nil {
				return "", nil
			}
			return bushook.LastCommit(ctx, db)(repo)
		}
	}
	return ledgeredMain(bushook.ReposFromEnv(), last, fallback)
}

// ledgeredMain reads last for watched repositories and fallback for the
// rest, and for a watched one the ledger has said nothing about (or could
// not be read). A nil fallback answers "" (the caller asks Gitea itself).
func ledgeredMain(watched []string, last, fallback mainReader) mainReader {
	return func(ctx context.Context, repo string) (string, error) {
		if last != nil && slices.Contains(watched, repo) {
			if commit, err := last(ctx, repo); err == nil && commitID.MatchString(commit) {
				return commit, nil
			}
		}
		if fallback == nil {
			return "", nil
		}
		return fallback(ctx, repo)
	}
}
