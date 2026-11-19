package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
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
			return fullci.CheckSources{FirstParents: fullci.FirstParentsFrom(s.unitTrees.Get), RunModules: ci.RunModulesOf, FailedTasks: ci.FullRunFailedTasks}, true
		},
	}.ServeHTTP(w, r)
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
			return ciscore.LastCleanup(ctx, client, s.delivery.PipelinesNamespace())
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
	tree := s.unitTreesOf()
	return cidomains.Source{Head: s.mainHead, Tree: tree, Files: cidomains.ChangedFilesFrom(s.unitTrees.Get), CommitOK: commitID.MatchString}
}

// ciDomainsAhead reads the domain graph of each repository the scorecard knows
// before anybody asks (cidomains.Cache.ReadAhead).
func (s *Server) ciDomainsAhead(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	repos := make([]string, 0, len(ciscore.Repos))
	for _, r := range ciscore.Repos {
		repos = append(repos, r.Repo)
	}
	s.ciDomainsKept.ReadAhead(ctx, repos, s.ciDomainsSourceOf)
}

// wireEvidenceGate makes the ledger the delivery service's CI reader and
// returns it; nil when this process has no delivery service. The database is
// read when asked: before it connects, or without one, the answer is an error.
func (s *Server) wireEvidenceGate() delivery.CIReader {
	if s.delivery == nil {
		return nil
	}
	// Strict: this reader is the release policy's ci_succeeded.
	reader := evidenceread.LedgerCIStrict(func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}))
	s.delivery.SetCIReader(reader)
	slog.Info("evidence gate: CI is read from the evidence ledger")
	return reader
}

func (s *Server) handleEvidenceCI(w http.ResponseWriter, r *http.Request) {
	evidenceread.CIRoute{
		RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (evidenceread.CISources, bool) {
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
				return evidenceread.CISources{}, false
			}
			return evidenceread.CISources{DB: s.agentRuntime.api.DB, Tree: giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})}, true
		},
	}.ServeHTTP(w, r)
}

func (s *Server) handleUnitScope(w http.ResponseWriter, r *http.Request) {
	evidenceread.ScopeRoute{
		RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (evidenceread.ScopeSources, bool) {
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
				return evidenceread.ScopeSources{}, false
			}
			return evidenceread.ScopeSources{DB: s.agentRuntime.api.DB,
				Tree:  giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}),
				Trees: s.unitTreesOf()}, true
		},
	}.ServeHTTP(w, r)
}
