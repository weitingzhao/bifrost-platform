package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// Only the workers path calls this. The main reconciler's reader is wired
// in STG and PROD whatever the landing switches say (review 663 B-02):
// turning the switch off stops new claims and pushes, never the recovery of
// what was already pushed. A writing loop needs an explicit opt-in:
// AGENTRT_LANDING_DRILL=on on STG (the drill), or AGENTRT_LANDING_LIVE=on
// on PROD with the lander identity (S3), and the delivery adapters.
// Constructing adapters does not read credentials or contact services.
func (s *Server) wireLandingDrill(c *controller.Controller) {
	c.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"}}
	s.wireLandingRequests(c)
	if c.Env != "stg" && c.Env != "prod" {
		return
	}
	requested := []string{}
	if c.Env == "prod" {
		requested = landing.LiveRepos(os.LookupEnv)
	} else if os.Getenv("AGENTRT_LANDING_DRILL") == "on" {
		requested = []string{"bifrost-platform"}
		if _, set := os.LookupEnv("AGENTRT_LANDING_LIVE_REPOS"); set {
			requested = landing.LiveRepos(os.LookupEnv)
		}
	}
	c.LiveRepos = append([]string{}, requested...)
	// Discover previously started repositories once at startup, not on every
	// platform pass. Closing the live switch cannot orphan main_intent.
	if c.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		repos, err := store.ExistingLandingRepos(ctx, c.DB)
		cancel()
		if err != nil {
			slog.Error("agentrt landing repository recovery unavailable", "error", err)
			c.Reconcile.Discover = func(ctx context.Context) ([]string, error) { return store.ExistingLandingRepos(ctx, c.DB) }
		} else {
			c.Reconcile.Repos = repos
		}
	}
	for _, repo := range landing.LandingRepoNames() {
		if slices.Contains(requested, repo) && !slices.Contains(c.Reconcile.Repos, repo) {
			c.Reconcile.Repos = append(c.Reconcile.Repos, repo)
		}
	}
	slices.Sort(c.Reconcile.Repos)
	readers := map[string]*landinggit.Git{}
	unavailable := map[string]error{}
	for _, repo := range landing.LandingRepoNames() {
		if !slices.Contains(c.Reconcile.Repos, repo) && !slices.Contains(requested, repo) {
			continue
		}
		var g *landinggit.Git
		var err error
		if c.Env == "prod" {
			g, err = landinggit.LanderFromEnv(c.Env, repo)
		} else {
			g, err = landinggit.FromEnv(c.Env, repo)
		}
		if err != nil {
			unavailable[repo] = fmt.Errorf("main reader unavailable: %w", err)
			continue
		}
		readers[repo] = g
	}
	c.Reconcile.MainEvidence = func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
		if err := unavailable[repo]; err != nil {
			return store.MainEvidence{}, err
		}
		if g := readers[repo]; g != nil {
			return mainEvidenceReader(g)(ctx, repo, root, a, r)
		}
		// A repository found by a retried startup discovery gets a reader
		// only when its durable operation actually needs main evidence.
		if landing.RepoSupported(repo) && slices.Contains(c.Reconcile.Repos, repo) {
			var g *landinggit.Git
			var err error
			if c.Env == "prod" {
				g, err = landinggit.LanderFromEnv(c.Env, repo)
			} else {
				g, err = landinggit.FromEnv(c.Env, repo)
			}
			if err != nil {
				return store.MainEvidence{}, err
			}
			return mainEvidenceReader(g)(ctx, repo, root, a, r)
		}
		return store.MainEvidence{}, errors.New("unsupported or unconfigured landing repo")
	}
	if len(requested) == 0 || s.cluster == nil {
		return
	}
	if c.Env == "prod" && !controller.LandingCardsComplete(c.LandingCards) {
		return
	}
	ci := s.landingCIClient()
	c.DrillGit = map[string]controller.DrillGit{}
	for _, repo := range requested {
		if g := readers[repo]; g != nil {
			c.DrillGit[repo] = g
		}
	}
	if len(c.DrillGit) == 0 {
		c.DrillGit = nil
		return
	}
	facts := controller.MultiRepoGitFacts(c.DrillGit)
	c.Drill = &controller.DrillSources{Facts: facts,
		Main: func(ctx context.Context, repo string) (string, error) {
			if g := readers[repo]; g != nil {
				return g.Ref(ctx, "refs/heads/main")
			}
			return "", errors.New("landing repo unavailable")
		},
		CI: func(ctx context.Context, repo, r string, after time.Time) (landing.CIRun, error) {
			if repo != "bifrost-platform" {
				return landing.CIRun{R: r, Status: "Missing"}, nil
			}
			return ci.LandingCI(ctx, r, after)
		},
		EnsureCI: func(ctx context.Context, repo, name, r string) error {
			if repo != "bifrost-platform" {
				return errors.New("infra landing CI adapter requires S6-5")
			}
			return ci.EnsureLandingCI(ctx, name, r)
		},
		WindowBlocked: func(ctx context.Context, repo string) (bool, error) {
			if !landing.RepoSupported(repo) {
				return true, errors.New("unsupported landing repo")
			}
			return ci.LandingWindowBlocked(ctx)
		},
	}
	if c.Env == "prod" {
		if g := readers["bifrost-platform"]; g != nil {
			c.Drill.Facts.GrantPatch = landingGrantPatch(g)
			c.Drill.Facts.GrantCI = ci.GrantCI
			c.Drill.EnsurePinnedCI = ci.EnsureGrantCI
			if heads := s.agentRuntimeMains(); heads != nil {
				c.Drill.SiblingHeads = siblingHeads(heads)
			}
		}
		c.Live = true
	} else {
		c.Drill.Delete = func(ctx context.Context, repo, ref, r string) error {
			if g := readers[repo]; g != nil {
				return g.Delete(ctx, ref, r)
			}
			return errors.New("landing repo unavailable")
		}
	}
}

// mainEvidenceReader reads main for the reconciler with the environment's
// Git identity.
func mainEvidenceReader(g *landinggit.Git) store.MainEvidenceFunc {
	return func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
		if repo != g.Repository() {
			return store.MainEvidence{}, errors.New("unsupported landing repo")
		}
		head, has, hist, err := g.MainEvidence(ctx, r, root, a)
		ev := store.MainEvidence{Head: head, HasR: has, Validity: "unknown"}
		switch hist.State {
		case landing.Effective:
			ev.Validity = "effective"
		case landing.Reverted:
			ev.Validity, ev.RevertedBy = "reverted", hist.RevertedBy
		}
		return ev, err
	}
}

// landingLiveOff says in a phrase why PROD's live landing is off: the switch,
// the lander identity, delivery, or the landing cards' reach.
func (s *Server) landingLiveOff(c *controller.Controller) string {
	switch {
	case len(landing.LiveRepos(os.LookupEnv)) == 0:
		if _, set := os.LookupEnv("AGENTRT_LANDING_LIVE_REPOS"); set {
			return "AGENTRT_LANDING_LIVE_REPOS has no enabled repositories"
		}
		return "AGENTRT_LANDING_LIVE is not on"
	case !controller.LandingCardsComplete(c.LandingCards):
		return "the landing cards cannot reach the question store"
	case s.cluster == nil:
		return "cluster unavailable"
	}
	return "no lander identity"
}

// Landing transports are wired here, not through delivery. Evidence and
// creation policy live entirely within the protected landingci package.
func (s *Server) landingCIClient() *landingci.Client {
	namespace := "cicd"
	// Tests build a bare Server; production always has a config.
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil && entry.ResolvedStackNamespace() != "" {
			namespace = entry.ResolvedStackNamespace()
		}
	}
	return &landingci.Client{Namespace: namespace, Dynamic: func() (dynamic.Interface, error) {
		cfg, _, err := s.cluster.Service().RestConfig()
		if err != nil {
			return nil, err
		}
		return dynamic.NewForConfig(cfg)
	}, ConfigMap: func(ctx context.Context, name string) (map[string]string, bool, error) {
		client, _, err := s.cluster.Service().KubernetesClient()
		if err != nil {
			return nil, false, err
		}
		cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return cm.Data, true, nil
	}}
}

// siblingHeads reads the platform CI's sibling repositories' main heads
// (bifrost-ui, bifrost-trade-infra) for a pinned grant CI key.
func siblingHeads(main func(context.Context, string) (string, error)) func(context.Context) (map[string]string, error) {
	return func(ctx context.Context) (map[string]string, error) {
		out := map[string]string{}
		for _, repo := range []string{"bifrost-trade-infra", "bifrost-ui"} {
			head, err := main(ctx, repo)
			if err != nil {
				return nil, err
			}
			if !landing.ValidSHA(head) {
				return nil, fmt.Errorf("%s main head unreadable", repo)
			}
			out[repo] = head
		}
		return out, nil
	}
}
