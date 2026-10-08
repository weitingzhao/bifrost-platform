package progress

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

const (
	infraRepo = "bifrost-trade-infra"
	debtPath  = "agent-config/TECH_DEBT.md"
	workPath  = "agent-config/WORK.md"
	gitRef    = "main"
)

// Source is the Gitea read lineage already uses, plus the commits it can list.
type Source interface {
	ReadFile(ctx context.Context, repo, ref, path string) (string, error)
	CommitsSince(ctx context.Context, since time.Time) ([]lineage.Commit, []string)
}

// Service builds GET /api/v1/progress from infra main and commit lineage.
type Service struct {
	src Source
	now func() time.Time
}

func NewService(src Source) *Service {
	return &Service{src: src, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Build(ctx context.Context, stuckDays int) Response {
	now := s.now()
	var errs []string
	debt, err := s.src.ReadFile(ctx, infraRepo, gitRef, debtPath)
	if err != nil {
		errs = append(errs, "TECH_DEBT.md: "+err.Error())
		debt = ""
	}
	work, err := s.src.ReadFile(ctx, infraRepo, gitRef, workPath)
	if err != nil {
		errs = append(errs, "WORK.md: "+err.Error())
		work = ""
	}
	commits, cerrs := s.src.CommitsSince(ctx, now.AddDate(0, 0, -scanDays))
	errs = append(errs, cerrs...)
	resp := Assemble(now, stuckDays, debt, work, commits)
	if len(errs) > 0 {
		resp.Errors = append(errs, resp.Errors...)
	}
	return resp
}
