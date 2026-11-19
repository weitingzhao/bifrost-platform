package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The CI read routes live in the verification packages (cidomains for
// /ci/domains and /ci/changes, ciscore for /ci/scorecard, evidence for
// /evidence/commit and /units/plan, fullci for /units/full-ci/check); this
// file hands them what they stand on: the names a request may carry, Gitea's
// reads, the cluster, the ledger.

func (s *Server) handleEvidenceCommit(w http.ResponseWriter, r *http.Request) {
	evidenceread.CommitRoute{
		RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (evidenceread.CommitSources, bool) {
			main := agentrtapi.MainHead(nil)
			if m := s.agentRuntimeMains(); m != nil {
				main = s.mainOf("evidence", m)
			}
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil || main == nil {
				return evidenceread.CommitSources{}, false
			}
			return evidenceread.CommitSources{DB: s.agentRuntime.api.DB, Tree: giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), Main: main}, true
		},
	}.ServeHTTP(w, r)
}

func (s *Server) handleUnitPlan(w http.ResponseWriter, r *http.Request) {
	evidenceread.PlanRoute{
		RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (evidenceread.PlanSources, bool) {
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
				return evidenceread.PlanSources{}, false
			}
			return evidenceread.PlanSources{DB: s.agentRuntime.api.DB, Trees: s.unitTreesOf()}, true
		},
	}.ServeHTTP(w, r)
}

func (s *Server) handleUnitFullCICheck(w http.ResponseWriter, r *http.Request) {
	fullci.CheckRoute{
		RepoOK: giteaName.MatchString, RunOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (fullci.CheckSources, bool) {
			if s.cluster == nil || s.delivery == nil {
				return fullci.CheckSources{}, false
			}
			s.unitTreesOf()
			ci := s.landingCIClient()
			return fullci.CheckSources{FirstParents: s.giteaFirstParents, RunModules: ci.RunModulesOf, FailedTasks: ci.FullRunFailedTasks}, true
		},
	}.ServeHTTP(w, r)
}

// giteaFirstParents walks main's history in Gitea from red back along first
// parents until green.
func (s *Server) giteaFirstParents(ctx context.Context, repo, green, red string, max int) ([][2]string, error) {
	body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/commits?sha="+red+"&limit=100&stat=false&verification=false&files=false", 8<<20)
	if err != nil {
		return nil, err
	}
	return fullci.FirstParentsOfPage(body, repo, green, red, max)
}

func (s *Server) handleCIScorecard(w http.ResponseWriter, r *http.Request) {
	ciscore.Route{Cache: &s.ciScorecardKept, Source: func() (ciscore.Source, bool) {
		if s.cluster == nil || s.delivery == nil {
			return ciscore.Source{}, false
		}
		return s.ciScoreSourceOf(), true
	}}.ServeHTTP(w, r)
}

func (s *Server) ciScoreSourceOf() ciscore.Source {
	return ciscore.Source{
		Runs: s.delivery.PipelineRunsOf,
		Tasks: func(ctx context.Context, selector string) ([]unstructured.Unstructured, error) {
			return s.delivery.UnitTaskRuns(ctx, "", selector)
		},
		Declaration: func(ctx context.Context, repo string) ciscore.Declaration {
			return s.ciScorecardKept.Declaration(ctx, repo, s.mainHead, s.unitTreesOf())
		},
		Cleanup: func(ctx context.Context) (time.Time, error) {
			client, _, err := s.cluster.Service().KubernetesClient()
			if err != nil {
				return time.Time{}, err
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			job, err := client.BatchV1().CronJobs(s.delivery.PipelinesNamespace()).Get(ctx, ciscore.CleanupJob, metav1.GetOptions{})
			if err != nil || job.Status.LastSuccessfulTime == nil {
				return time.Time{}, err
			}
			return job.Status.LastSuccessfulTime.Time, nil
		},
	}
}

func (s *Server) ciDomainsRoute() cidomains.Route {
	return cidomains.Route{Cache: &s.ciDomainsKept, RepoOK: giteaName.MatchString, Source: func() (cidomains.Source, bool) {
		if s.delivery == nil {
			return cidomains.Source{}, false
		}
		return s.ciDomainsSourceOf(), true
	}}
}

func (s *Server) handleCIDomains(w http.ResponseWriter, r *http.Request) {
	s.ciDomainsRoute().HandleDomains(w, r)
}

func (s *Server) handleCIChanges(w http.ResponseWriter, r *http.Request) {
	s.ciDomainsRoute().HandleChanges(w, r)
}

func (s *Server) ciDomainsSourceOf() cidomains.Source {
	return cidomains.Source{Head: s.mainHead, Tree: s.unitTreesOf(), Files: s.giteaChangedFiles, CommitOK: commitID.MatchString}
}

// giteaChangedFiles are the paths a commit changed against its first parent,
// as Gitea lists them for the commit.
func (s *Server) giteaChangedFiles(ctx context.Context, repo, commit string) ([]string, error) {
	s.unitTreesOf()
	body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/git/commits/"+commit+"?stat=false&verification=false&files=true", 8<<20)
	if err != nil {
		return nil, err
	}
	return cidomains.ChangedFilesOf(body, repo, commit)
}

// ciDomainsAheadEvery is how often the workers look whether main moved.
const ciDomainsAheadEvery = time.Minute

// ciDomainsAhead reads the domain graph before anybody asks: once a minute,
// in the workers, it asks for the graph of each repository the scorecard
// knows, which starts the read of a new head and keeps the result in the
// platform's state; the API then answers from what is kept.
//
// Owner 2026-11-16 22:5x, the coordinator's reading of it: the graph was
// computed when he clicked, so he waited. A click should only read. This is
// the plain form (a look a minute); waking on git.main.advanced instead is
// the communication line's bus, and can replace the ticker.
func (s *Server) ciDomainsAhead(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	safego.Go("ci-domains-ahead", func() {
		t := time.NewTicker(ciDomainsAheadEvery)
		defer t.Stop()
		for {
			for _, r := range ciscore.Repos {
				pass, cancel := context.WithTimeout(kubemetrics.WithSite(ctx, "ci-domains-ahead"), 20*time.Second)
				if _, err := s.ciDomainsKept.Graph(pass, s.ciDomainsSourceOf(), r.Repo); err != nil && ctx.Err() == nil {
					slog.Debug("ci domains ahead", "repo", r.Repo, "error", err)
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
}
