package assembly

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// What assembly hands lineage and gets back goes through this package, the
// lineage domain's public face (law 71, 2026-11-23): server used to build
// lineage.Access, lineage.Release and lineage.PlanHistoryReader itself and
// held the lineage core package and recorderimport directly. These names are
// the contract; the core packages behind them can change without assembly
// noticing.

// Gitea is the Gitea access the platform lends lineage (delivery's).
type Gitea struct{ Base, Org, User, Pass string }

// GiteaFunc reads that access.
type GiteaFunc func(ctx context.Context) (Gitea, error)

type (
	// Handler serves the lineage routes (Mount).
	Handler = lineage.Handler
	// Release is one recorded delivery run as lineage reads it.
	Release = lineage.Release
	// HistoryRules are verification's landing rules, bound by assembly: lineage
	// reads main's history and does not import verification (OWNER_LAW #28).
	HistoryRules = lineage.HistoryRules
	// HistoryCommit is one commit of that history, as the rules get it.
	HistoryCommit = lineage.HistoryCommit
	// PlanHistoryReader reads main's history for the plan (History).
	PlanHistoryReader = lineage.PlanHistoryReader
	// RecorderRequest is the plan import's recorder log request.
	RecorderRequest = recorderimport.Request
)

// RecorderFormat names the recorder log request's shape.
const RecorderFormat = recorderimport.Format

// access adds the agent organization (AGENTRT_GIT_ORG): its branches are read
// too (T36), as the result check reads them.
func access(gitea GiteaFunc) lineage.AccessFunc {
	return func(ctx context.Context) (lineage.Access, error) {
		a, err := gitea(ctx)
		return lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass,
			AgentOrg: strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))}, err
	}
}

// Wire builds the lineage handler from the Gitea access, the release records
// and the thread titles.
func Wire(gitea GiteaFunc, releases func(ctx context.Context) ([]Release, error), titles *threadtitles.Store) *Handler {
	return New(access(gitea), releases, titles)
}

// PlanHistory is the plan's main-history reader: one for the process, which
// keeps each repo's main between passes. main, when set, answers main's head
// first; rules are verification's landing rules.
func PlanHistory(gitea GiteaFunc, main func(ctx context.Context, repo string) (string, error), rules HistoryRules) *PlanHistoryReader {
	return &lineage.PlanHistoryReader{
		Access:   access(gitea),
		Client:   &http.Client{Timeout: 20 * time.Second},
		MaxPages: 200,
		Main:     main,
		Rules:    rules,
	}
}
