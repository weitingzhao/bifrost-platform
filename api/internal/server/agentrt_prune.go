package server

import (
	"context"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// The readers of the branch cleanup (TD-383, store/prune.go): which attempt
// branches a repository has, for the branches agentd is given to delete; and
// which results main holds, for when an accepted result is first seen there
// and whether it still stands.

// agentRuntimeAgentHeads reads the attempt branches of a repository in the
// organization agent branches go to (AGENTRT_GIT_ORG); nil without one.
func (s *Server) agentRuntimeAgentHeads() agentrtapi.AgentHeads {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return agentrtapi.AgentHeads(giteaAgentHeads(s.delivery.GiteaAccess, org, &http.Client{Timeout: 10 * time.Second}))
}

// mainHorizon and mainPages bound one read of a main: two months, 2000
// commits; mainTTL is how long a read is kept.
const (
	mainHorizon = 60 * 24 * time.Hour
	mainPages   = 40
	mainTTL     = 10 * time.Minute
)

// agentRuntimeMainResults reads a repository's main where agentd clones it
// from (the delivery's organization) for the runtime's results that stand
// there (lineage.MainResults). A read is kept while main's head stays where
// it was, and at most mainTTL; main's head is read every time, so a revert
// is seen at once and main out of reach is no read (review 437 A1). nil
// without the delivery service.
func (s *Server) agentRuntimeMainResults() store.MainResults {
	tip := s.agentRuntimeMains()
	if s.delivery == nil || tip == nil {
		return nil
	}
	access, client := s.delivery.GiteaAccess, &http.Client{Timeout: 20 * time.Second}
	return cachedMainResults(func(ctx context.Context, repo string) (store.OnMain, error) {
		a, err := access(ctx)
		if err != nil {
			return nil, err
		}
		acc := lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}
		since := time.Now().Add(-mainHorizon)
		out, err := lineage.MainResults(ctx, acc, client, repo, since, mainPages)
		if err != nil {
			return nil, err
		}
		pushed := map[string][]string{}
		if s.agentRuntime == nil || s.agentRuntime.late == nil {
			return out, nil
		}
		err = store.InTx(ctx, s.agentRuntime.late, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id, a.pushed_commit
				FROM agentrt.task t JOIN agentrt.attempt a ON a.task_id = t.task_id
				WHERE t.repo = $1 AND t.role = 'implement' AND a.pushed_commit IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.task_id = t.task_id
					AND e.event_type = 'note' AND e.body_json ? 'landed')
				ORDER BY t.task_id, a.attempt_no DESC`, repo)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var task, sha string
				if err := rows.Scan(&task, &sha); err != nil {
					return err
				}
				pushed[task] = append(pushed[task], sha)
			}
			return rows.Err()
		})
		if err != nil {
			slog.Warn("agentrt main changes: pushed attempts unavailable", "repo", repo, "err", err)
			return out, nil
		}
		if len(pushed) == 0 {
			return out, nil
		}
		changes, err := lineage.MainChanges(ctx, acc, client, repo, since, mainPages, pushed)
		if err != nil {
			slog.Warn("agentrt main changes", "repo", repo, "err", err)
			return out, nil
		}
		for task, results := range changes {
			if out[task] == nil {
				out[task] = map[string]string{}
			}
			for from, main := range results {
				if _, exists := out[task][from]; !exists {
					out[task][from] = main
				}
			}
		}
		return out, nil
	}, tip, mainTTL)
}

// cachedMainResults keeps each repository's last good read with main's head
// when it was read, and gives it again only while the head, read anew each
// time, is the same and the read is younger than ttl. A head or a read that
// fails is no read (ok false), and nothing is kept for it.
func cachedMainResults(read func(context.Context, string) (store.OnMain, error), tip func(context.Context, string) (string, error),
	ttl time.Duration) store.MainResults {
	type entry struct {
		at      time.Time
		head    string
		results store.OnMain
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	return func(ctx context.Context, repo string) (store.OnMain, bool) {
		head, err := tip(ctx, repo)
		if err != nil || head == "" {
			return nil, false
		}
		mu.Lock()
		e, ok := cache[repo]
		mu.Unlock()
		if ok && e.head == head && time.Since(e.at) < ttl {
			return e.results, true
		}
		results, err := read(ctx, repo)
		if err != nil {
			return nil, false
		}
		results = results.WithHead(head)
		mu.Lock()
		cache[repo] = entry{time.Now(), head, results}
		mu.Unlock()
		return results, true
	}
}
