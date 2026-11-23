package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	lineageassembly "github.com/weitingzhao/bifrost-platform/api/internal/lineage/assembly"
)

func (s *Server) agentRuntimePlanSources() planstore.PlanSources {
	var src planstore.PlanSources
	if s.delivery != nil {
		reader := lineageassembly.PlanHistory(func(ctx context.Context) (lineageassembly.Gitea, error) {
			a, err := s.delivery.GiteaAccess(ctx)
			return lineageassembly.Gitea{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, err
		}, s.mainOf("plan-history", nil), planHistoryRules())
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
			return reader.History(ctx, repo, task, result, snapshot)
		}
	}
	return src
}

// planHistoryRules binds verification's landing rules (landinggit) into the
// lineage reader's port: lineage reads main's history from Gitea and does not
// import verification (OWNER_LAW #28, 2026-11-20). The commits are copied
// field by field; the rules are landinggit's, unchanged.
func planHistoryRules() lineageassembly.HistoryRules {
	return lineageassembly.HistoryRules{
		Evaluate: func(task, result, message string, commits []lineageassembly.HistoryCommit, complete bool) (landing.MainHistory, error) {
			in := make([]landinggit.HistoryCommit, len(commits))
			for i, c := range commits {
				in[i] = landinggit.HistoryCommit{SHA: c.SHA, Message: c.Message, Parents: c.Parents}
			}
			return landinggit.EvaluateHistory(task, result, message, in, complete)
		},
		CherryPickSource: landinggit.CherryPickSource,
		HasTaskTrailer:   landinggit.HasTaskTrailer,
	}
}
