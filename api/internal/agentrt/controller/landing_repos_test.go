package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
)

type repoProbe struct {
	repo  string
	opens int
}

func (g *repoProbe) Repository() string { return g.repo }
func (g *repoProbe) Open(context.Context, string) (*landinggit.Workspace, error) {
	g.opens++
	return nil, errors.New(g.repo)
}
func (g *repoProbe) Ref(context.Context, string) (string, error)  { panic("unexpected remote read") }
func (g *repoProbe) Delete(context.Context, string, string) error { panic("unexpected remote write") }

type noLandingDB struct{}

func (noLandingDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("disabled loop accessed database")
}

// T61: an identical SHA selects only its repository's adapter, for both
// the candidate and dependency history. Unknown/misbound repos open nothing.
func TestS6EvidenceDispatchIsByRepo(t *testing.T) {
	t.Parallel()
	p := &repoProbe{repo: "bifrost-platform"}
	i := &repoProbe{repo: "bifrost-trade-infra"}
	src := MultiRepoGitFacts(map[string]DrillGit{p.repo: p, i.repo: i})
	sha := strings.Repeat("a", 40)
	for _, g := range []*repoProbe{p, i} {
		_, _, err := src.Result(context.Background(), g.repo, "task", "attempt", sha, sha)
		if err == nil || err.Error() != g.repo {
			t.Fatal("result routed incorrectly", err)
		}
		_, err = src.History(context.Background(), g.repo, "task", sha, sha)
		if err == nil || err.Error() != g.repo {
			t.Fatal("history routed incorrectly", err)
		}
	}
	if _, _, err := src.Result(context.Background(), "bifrost-research", "task", "attempt", sha, sha); err == nil {
		t.Fatal("unknown repo accepted")
	}
	bad := MultiRepoGitFacts(map[string]DrillGit{i.repo: p})
	if _, _, err := bad.Result(context.Background(), i.repo, "task", "attempt", sha, sha); err == nil {
		t.Fatal("misbound transport accepted")
	}
	if p.opens != 2 || i.opens != 2 {
		t.Fatal("cross-repository workspace", p.opens, i.opens)
	}
}

func TestS6ClosedReposPerformNoWork(t *testing.T) {
	t.Parallel()
	p := &repoProbe{repo: "bifrost-platform"}
	i := &repoProbe{repo: "bifrost-trade-infra"}
	c := Controller{Env: "prod", Live: true, LiveRepos: []string{}, DB: noLandingDB{}, Drill: &DrillSources{}, DrillGit: map[string]DrillGit{p.repo: p, i.repo: i}}
	if err := c.LandingDrillPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*repoProbe{p, i} {
		if err := c.LandingDrillBranch(context.Background(), g, "op", time.Now(), g.repo); err == nil {
			t.Fatal("closed branch enabled")
		}
		if _, err := c.LandingDrillCI(context.Background(), *c.Drill, "op", time.Now(), g.repo); err == nil {
			t.Fatal("closed CI enabled")
		}
		if err := c.landLiveWith(context.Background(), *c.Drill, "op", "sha", false, nil, g.repo); err == nil {
			t.Fatal("closed live push enabled")
		}
	}
	c.LiveRepos = nil // old LIVE=on contract is platform only
	if !slices.Equal(c.landingRepoNames(), []string{p.repo}) {
		t.Fatal(c.landingRepoNames())
	}
	if err := c.LandingDrillBranch(context.Background(), i, "op", time.Now(), i.repo); err == nil {
		t.Fatal("legacy live enabled infra")
	}
	if _, err := c.LandingDrillCI(context.Background(), *c.Drill, "op", time.Now(), i.repo); err == nil {
		t.Fatal("legacy live enabled infra CI")
	}
	if err := c.landLiveWith(context.Background(), *c.Drill, "op", "sha", false, nil, i.repo); err == nil {
		t.Fatal("legacy live enabled infra push")
	}
	if p.opens != 0 || i.opens != 0 {
		t.Fatal("disabled transport opened")
	}
}
