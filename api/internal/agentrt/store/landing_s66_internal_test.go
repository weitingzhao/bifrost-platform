package store

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// SQL-call probe for byte order and empty-stream compatibility. Real lock
// waits/deadlocks are covered separately by TestS66T52*PG, never by this fake.
type s66LockTx struct {
	pgx.Tx
	locks []string
}

func (*s66LockTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &s66RepoRows{repos: []string{"bifrost-trade-infra", "bifrost-platform"}}, nil
}
func (*s66LockTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return funcRow(func(v ...any) error { *v[0].(*int64) = 0; return nil })
}
func (x *s66LockTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "FOR UPDATE") {
		x.locks = append(x.locks, args[0].(string))
	}
	return pgconn.CommandTag{}, nil
}

type s66RepoRows struct {
	pgx.Rows
	repos []string
	index int
}

func (r *s66RepoRows) Next() bool          { r.index++; return r.index <= len(r.repos) }
func (r *s66RepoRows) Scan(v ...any) error { *v[0].(*string) = r.repos[r.index-1]; return nil }
func (*s66RepoRows) Close()                {}
func (*s66RepoRows) Err() error            { return nil }

func TestS66B4SortsStreamsAndLeavesUnsignedInfraEmpty(t *testing.T) {
	t.Parallel()
	for _, infra := range []bool{false, true} {
		tx := &s66LockTx{}
		repos, err := missionLandingRepos(context.Background(), tx, "M-3")
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
		folds := landingFolds{
			"landing:bifrost-platform":    {state: landing.State{LiveAt: at}},
			"landing:bifrost-trade-infra": {state: landing.State{}},
		}
		wantRepos := []string{"bifrost-platform"}
		wantLocks := []string{"landing:bifrost-platform"}
		if infra {
			folds["landing:bifrost-trade-infra"] = landingFold{state: landing.State{LiveAt: at}}
			wantRepos = append(wantRepos, "bifrost-trade-infra")
			wantLocks = append(wantLocks, "landing:bifrost-trade-infra")
		}
		live, err := lockMissionLandingStreams(context.Background(), tx, folds, "M-3", repos)
		if err != nil || !reflect.DeepEqual(live, wantRepos) || !reflect.DeepEqual(tx.locks, wantLocks) {
			t.Fatal("wrong byte order or unsigned stream locked", live, tx.locks, err)
		}
	}
}

func TestS66InfraCardExplainsSoftScope(t *testing.T) {
	t.Parallel()
	tr := CardTrigger{Kind: CardFloor, Op: "land:task:1", Choices: []string{"do_not_merge", "wait"}, Floor: map[string]any{"soft": []string{"size"}, "grant_supported": false}}
	soft := LandingCardRequest("bifrost-trade-infra", tr, "Cannot land")
	if body := soft["body"].(string); !strings.Contains(body, "Split the change") || !strings.Contains(body, "Owner push it manually") || strings.Contains(body, "no landing rule can allow") {
		t.Fatal(body)
	}
	tr.Floor = map[string]any{"hard": "scripts/x"}
	hard := LandingCardRequest("bifrost-trade-infra", tr, "Cannot land")
	if !strings.Contains(hard["body"].(string), "no landing rule can allow") {
		t.Fatal(hard)
	}
	platform := LandingCardRequest("bifrost-platform", tr, "Cannot land")
	if !strings.Contains(platform["body"].(string), "no landing rule can allow") {
		t.Fatal("platform text changed", platform)
	}
}
