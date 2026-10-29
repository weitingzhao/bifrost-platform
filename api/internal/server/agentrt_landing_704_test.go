package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type startup704DB struct {
	pgx.Tx
	signed, history, fail    bool
	begins, queries, commits int
	bounded                  bool
}

func (d *startup704DB) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	d.begins++
	deadline, ok := ctx.Deadline()
	d.bounded = ok && time.Until(deadline) <= 10*time.Second
	if d.fail {
		return nil, errors.New("discovery unavailable")
	}
	return d, nil
}
func (d *startup704DB) Commit(context.Context) error   { d.commits++; return nil }
func (d *startup704DB) Rollback(context.Context) error { return nil }

type startup704Row func(...any) error

func (r startup704Row) Scan(dest ...any) error { return r(dest...) }
func (d *startup704DB) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	d.queries++
	return startup704Row(func(dest ...any) error {
		if strings.Contains(q, "SELECT EXISTS") {
			*dest[0].(*bool) = d.history
			return nil
		}
		if !d.signed {
			return pgx.ErrNoRows
		}
		text := `rule_id: landing:dev
rule_version: 1
direction: initial
effective_at: 2026-10-01T00:00:00Z
expires_at: 2027-01-15T00:00:00Z
tenant: dev
repos:
 - repo: bifrost-platform
   allow: ["console/src/components/**/*.tsx"]
   deny: []
   stg_release: none
 - repo: bifrost-trade-infra
   allow: ["agent-config/work/multi-agent/REPORT-*.md"]
   deny: []
   stg_release: none
limits: {max_files: 10, max_changed_lines: 3000, max_landings_per_day: 5, max_in_flight_per_repo: 1}
`
		r, err := rules.Parse([]byte(text))
		if err != nil {
			return err
		}
		b, err := r.BodyJSON()
		if err != nil {
			return err
		}
		*dest[0].(*int) = 1
		*dest[1].(*string) = "initial"
		*dest[2].(*time.Time) = r.Effective
		*dest[3].(*time.Time) = r.Expires
		*dest[4].(*string) = text
		*dest[5].(*[]byte) = b
		by := "owner"
		*dest[6].(**string) = &by
		return nil
	})
}
func TestReview704UnsignedWiringAndRecovery(t *testing.T) {
	t.Setenv("AGENTRT_LANDING_LIVE_REPOS", "bifrost-platform,bifrost-trade-infra")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("AGENTRT_LANDER_USERNAME_FILE", filepath.Join(dir, "missing-user"))
	t.Setenv("AGENTRT_LANDER_PASSWORD_FILE", filepath.Join(dir, "missing-password"))
	s := &Server{cluster: cluster.NewHandler(&config.Config{}, nil)}
	for _, signed := range []bool{false, true} {
		for _, history := range []bool{false, true} {
			t.Run(fmt.Sprintf("signed=%t/history=%t", signed, history), func(t *testing.T) {
				db := &startup704DB{signed: signed, history: history}
				c := &controller.Controller{Env: "prod", DB: db, LandingCards: completeCardQuestions()}
				s.wireLandingDrill(c)
				repo := "bifrost-trade-infra"
				if (c.DrillGit[repo] != nil) != signed || slices.Contains(c.LiveRepos, repo) != signed || slices.Contains(c.Reconcile.Repos, repo) != (signed || history) {
					t.Fatal("incorrect admission", c.LiveRepos, c.Reconcile.Repos)
				}
				if !signed {
					_, err := c.Drill.Facts.History(context.Background(), repo, "dependency", strings.Repeat("a", 40), "")
					if err == nil || strings.Contains(err.Error(), "username file") {
						t.Fatal("unsigned infra reached credentials", err)
					}
					entries, err := os.ReadDir(dir)
					if err != nil || len(entries) != 0 {
						t.Fatal("workspace leaked", entries, err)
					}
				} else {
					// Same reader is admitted while signed, then immediately closes when
					// the current rule is removed, without rebuilding the controller.
					db.signed = false
					_, err := c.Drill.Facts.History(context.Background(), repo, "dependency", strings.Repeat("a", 40), "")
					if err == nil || strings.Contains(err.Error(), "username file") {
						t.Fatal("revoked reader reached credentials", err)
					}
				}
				if history {
					_, err := c.Reconcile.MainEvidence(context.Background(), repo, "root", strings.Repeat("a", 40), strings.Repeat("b", 40))
					if err == nil || err.Error() != "cannot read landing username file" {
						t.Fatal("historical recovery lost its independent reader", err)
					}
				}
			})
		}
	}
}
func TestReview704LegacyDiscoveryCost(t *testing.T) {
	t.Setenv("AGENTRT_LANDING_LIVE_REPOS", "")
	_ = os.Unsetenv("AGENTRT_LANDING_LIVE_REPOS")
	t.Setenv("AGENTRT_LANDING_LIVE", "on")
	t.Setenv("AGENTRT_LANDER_USERNAME_FILE", "/not-read/username")
	t.Setenv("AGENTRT_LANDER_PASSWORD_FILE", "/not-read/password")
	s := &Server{cluster: cluster.NewHandler(&config.Config{}, nil)}
	for _, fail := range []bool{false, true} {
		db := &startup704DB{fail: fail}
		c := &controller.Controller{Env: "prod", DB: db, LandingCards: completeCardQuestions()}
		s.wireLandingDrill(c)
		if db.begins != 1 || !db.bounded || !slices.Equal(c.LiveRepos, []string{"bifrost-platform"}) || len(c.DrillGit) != 1 || (c.Reconcile.Discover != nil) != fail {
			t.Fatal("legacy startup cost/admission", db, c.LiveRepos)
		}
		if !fail && (db.queries != 1 || db.commits != 1) {
			t.Fatal("extra startup SQL", db)
		}
	}
}
func TestReview704SignedFactsControlAndRevocation(t *testing.T) {
	db := &startup704DB{signed: true}
	c := &controller.Controller{DB: db}
	reads := 0
	src := controller.MultiRepoGitFacts(nil)
	src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		reads++
		return landing.MainHistory{State: landing.Effective, Commit: strings.Repeat("a", 40)}, nil
	}
	guarded := (&Server{}).landingRepoFacts(c, src)
	_, err := guarded.History(context.Background(), "bifrost-trade-infra", "dependency", "a", "")
	if err != nil || reads != 1 {
		t.Fatal("signed control", err, reads)
	}
	db.signed = false
	_, err = guarded.History(context.Background(), "bifrost-trade-infra", "dependency", "a", "")
	if err == nil || reads != 1 {
		t.Fatal("revoked evidence read", err, reads)
	}
	before := db.begins
	_, err = guarded.History(context.Background(), "bifrost-platform", "root", "a", "")
	if err != nil || reads != 2 || db.begins != before {
		t.Fatal("platform gained a rule query", err, reads, db)
	}
}
