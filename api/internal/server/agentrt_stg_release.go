package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// The supervisor reads only the signed rule while none. No B5 controller,
// window holder or repository stream is started until the rule opts in.
// Keeping this outside the landing loop lets a rule change take effect without
// a restart and lets live-off continue observation of an outstanding run.
func (s *Server) startSTGRelease(ctx context.Context, c *controller.Controller) func() {
	if c.Env != "prod" || c.DB == nil || s.cluster == nil {
		return func() {}
	}
	done := make(chan struct{})
	safego.Go("agentrt-stg-release", func() {
		defer close(done)
		var release *stgrelease.Controller
		var policy atomic.Pointer[stgrelease.WindowPolicy]
		defer func() {
			if release != nil {
				if err := release.Close(); err != nil {
					slog.Warn("STG window shutdown", "error", err)
				}
			}
		}()
		pass := func() {
			readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			rule, err := store.StgControlRule(readCtx, c.DB, "dev")
			cancel()
			p := &stgrelease.WindowPolicy{Environment: c.Env, Live: c.Live, Rule: rule}
			policy.Store(p)
			if err != nil {
				slog.Warn("STG rule read", "error", err)
			}
			enabled := false
			if rule != nil && rule.Landing != nil {
				repo, ok := rule.Landing.Repo("bifrost-platform")
				enabled = ok && repo.STGRelease == delivery.STGReleasePipeline
			}
			if !enabled || err != nil {
				if release != nil {
					if err := release.Close(); err != nil {
						slog.Warn("STG window release", "error", err)
					}
					release = nil
				}
				return
			}
			if release == nil {
				release = s.newSTGRelease(ctx, c, func() stgrelease.WindowPolicy { return *policy.Load() })
			}
			if err := release.Pass(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("STG release pass", "error", err)
			}
		}
		safego.Do("agentrt-stg-release-pass", pass)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				safego.Do("agentrt-stg-release-pass", pass)
			}
		}
	})
	return func() { <-done }
}
func (s *Server) newSTGRelease(ctx context.Context, c *controller.Controller, policy func() stgrelease.WindowPolicy) *stgrelease.Controller {
	ledger := stgrelease.PostgresLedger{DB: c.DB, Tenant: "dev"}
	adapter := &delivery.STGReleaseAdapter{HTTP: &http.Client{Timeout: 10 * time.Second}, Archive: ledger,
		Core: func() (kubernetes.Interface, error) {
			client, _, err := s.cluster.Service().KubernetesClient()
			return client, err
		},
		Dynamic: func() (dynamic.Interface, error) {
			cfg, _, err := s.cluster.Service().RestConfig()
			if err != nil {
				return nil, err
			}
			return dynamic.NewForConfig(cfg)
		},
	}
	cluster := ""
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil {
			cluster = entry.ID
		}
	}
	ci := s.landingCIClient()
	// Construct the existing read-only identity lazily. none never even reads
	// credential paths, and no candidate code or Git push enters this loop.
	git := func() (*landinggit.Git, error) { return landinggit.ReaderFromEnv("bifrost-platform") }
	sources := stgrelease.Sources{
		MainEvidence: func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
			g, err := git()
			if err != nil {
				return store.MainEvidence{}, err
			}
			return mainEvidenceReader(g)(ctx, repo, root, a, r)
		},
		Head: func(ctx context.Context) (string, error) {
			g, err := git()
			if err != nil {
				return "", err
			}
			return g.Ref(ctx, "refs/heads/main")
		},
		CI: func(ctx context.Context, p string) (store.StgCIObservation, error) {
			result, err := ci.ReadRelease(ctx, p)
			return stgrelease.ReleaseCIObservation(result), err
		},
		Range: func(ctx context.Context, base, p string) ([]landing.ReleaseCommit, error) {
			g, err := git()
			if err != nil {
				return nil, err
			}
			w, err := g.Open(ctx, p)
			if err != nil {
				return nil, err
			}
			defer w.Close()
			head, err := w.Main(ctx)
			if err != nil {
				return nil, err
			}
			if head != p {
				return nil, errors.New("STG main changed during range read")
			}
			return w.ReleaseRange(ctx, base, p)
		},
	}
	return &stgrelease.Controller{DB: c.DB, Tenant: "dev", Cluster: cluster, Ledger: ledger, Runs: adapter, Windows: adapter, Sources: sources, Policy: policy, Lifetime: ctx}
}
