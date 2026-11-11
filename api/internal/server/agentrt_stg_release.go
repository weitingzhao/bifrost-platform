package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
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
			// R5:5.7-5: the passes share one tick and one lock; a slow tick
			// says which of them took the time (logging only). ExternalPass runs
			// first so an unblock and a new baseline are seen in this tick; it
			// reads the cluster only while a manual run is needed and logs its
			// own slow reads (R5:5.7-11).
			start := time.Now()
			if err := release.ExternalPass(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("STG external release pass", "error", err)
			}
			external := time.Since(start)
			coverStart := time.Now()
			if err := release.CoverPass(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("STG coverage pass", "error", err)
			}
			cover := time.Since(coverStart)
			if err := release.Pass(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("STG release pass", "error", err)
			}
			if total := time.Since(start); total >= 5*time.Second {
				slog.Info("STG release tick took "+total.Round(time.Millisecond).String(),
					"external", external.Round(time.Millisecond).String(), "cover", cover.Round(time.Millisecond).String())
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
	adapter := &delivery.STGReleaseAdapter{HTTP: &http.Client{Timeout: 10 * time.Second}, Archive: ledger, WindowRecorder: s.recordWindowEvent,
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
	ci.ReuseAccepted = func(ctx context.Context, revision, tree, run, uid string) error {
		_, err := store.RecordCIReuse(ctx, c.DB, "bifrost-platform", revision, tree, run, uid)
		return err
	}
	scopeLog := &ciScopeLog{}
	// Construct the existing read-only identity lazily. none never even reads
	// credential paths, and no candidate code or Git push enters this loop.
	git := func() (*landinggit.Git, error) { return landinggit.ReaderFromEnv("bifrost-platform") }
	sources := stgrelease.Sources{
		ReleaseEvidence: func(ctx context.Context, state store.StgState, m store.StgMemberKey, p string) (store.MainEvidence, error) {
			g, err := git()
			if err != nil {
				return store.MainEvidence{}, err
			}
			return releaseEvidence(ctx, g.ReleaseEvidence, state, m, p)
		},
		// R5:5.7-5 slice 4: one workspace (main fetched once) for a coverage
		// pass; each read still fetches its own P and A.
		EvidenceSession: func(ctx context.Context) (stgrelease.EvidenceRead, func() error, error) {
			g, err := git()
			if err != nil {
				return nil, nil, err
			}
			er, err := g.OpenEvidence(ctx)
			if err != nil {
				return nil, nil, err
			}
			read := func(ctx context.Context, state store.StgState, m store.StgMemberKey, p string) (store.MainEvidence, error) {
				return releaseEvidence(ctx, er.ReleaseEvidence, state, m, p)
			}
			return read, er.Close, nil
		},
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
			scopeLog.note(p, result, err)
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

// ciScopeLog says once per P and verdict why P's CI does not stand for it at
// STG: the deciding run's scope and the last full green's run, commit, age
// and cap (for example: last full CI green 51 h ago, cap 48 h). The age
// within a wait is not a new verdict, so it does not log again by itself.
type ciScopeLog struct {
	mu   sync.Mutex
	last string
}

func (l *ciScopeLog) note(p string, e landingci.ReleaseEvidence, err error) {
	if err != nil {
		return
	}
	if _, recErr := e.Record(); recErr == nil && len(e.Stale) == 0 {
		return
	}
	key := fmt.Sprint(p, e.Status, e.Reason, e.Run, e.Scope(), len(e.Stale), e.LastFullRun)
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == l.last {
		return
	}
	l.last = key
	slog.Info("STG release CI of P "+e.ScopeNote(), "P", p, "status", e.Status, "reason", e.Reason, "ci_run", e.Run, "scope", e.Scope(), "stale", e.Stale,
		"last_full_run", e.LastFullRun, "last_full_commit", e.LastFullCommit, "last_full_age_h", e.LastFullAgeHours(), "cap_h", int(landingci.FullMaxAge.Hours()))
}

// releaseEvidence asks read whether P holds the member's R. For an InMain
// member the recorded H is asked without root and A (O-3); otherwise its
// validity is read with the operation's root and A.
func releaseEvidence(ctx context.Context, read func(context.Context, string, string, string, string) (string, bool, landing.MainHistory, error), state store.StgState, m store.StgMemberKey, p string) (store.MainEvidence, error) {
	root, a := m.Root, state.Legacy.Operations[m.Op].A
	if state.InMain[m.Op] == m.R {
		root, a = "", "" // O-3 asks whether the recorded H is contained in P
	}
	head, has, hist, err := read(ctx, p, m.R, root, a)
	return store.MainEvidence{Head: head, HasR: has, Validity: string(hist.State), RevertedBy: hist.RevertedBy}, err
}
