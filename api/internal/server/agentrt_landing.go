package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

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
	// denied settles from the database in every environment (S3 must 3).
	c.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"}}
	s.wireLandingRequests(c)
	var g *landinggit.Git
	var err error
	switch c.Env {
	case "stg":
		g, err = landinggit.FromEnv(c.Env)
	case "prod":
		g, err = landinggit.LanderFromEnv(c.Env)
	default:
		return
	}
	if err != nil {
		// No Git identity: every read of main fails visibly (a landing's
		// evidence records unreadable), never a silent nil reader.
		unavailable := fmt.Errorf("main reader unavailable: %w", err)
		c.Reconcile.MainEvidence = func(context.Context, string, string, string, string) (store.MainEvidence, error) {
			return store.MainEvidence{}, unavailable
		}
		slog.Error("agentrt landing: no Git identity; main reads fail and no landing runs", "env", c.Env, "error", err)
		return
	}
	c.Reconcile.MainEvidence = mainEvidenceReader(g)
	write := c.Env == "stg" && os.Getenv("AGENTRT_LANDING_DRILL") == "on" || c.Env == "prod" && os.Getenv("AGENTRT_LANDING_LIVE") == "on"
	if !write {
		return
	}
	if s.cluster == nil {
		slog.Error("agentrt landing disabled: cluster unavailable", "env", c.Env)
		return
	}
	ci := s.landingCIClient()
	c.DrillGit = g
	c.Drill = &controller.DrillSources{Facts: controller.DrillGitFacts(g), Main: func(ctx context.Context) (string, error) { return g.Ref(ctx, "refs/heads/main") }, CI: ci.LandingCI, EnsureCI: ci.EnsureLandingCI, WindowBlocked: ci.LandingWindowBlocked}
	if c.Env == "prod" {
		// C-5 (review 694 B-01): an operation that needs a grant runs CI
		// pinned to the siblings' main heads read at its kick, and its
		// grant's evidence is the strict read of that run.
		c.Drill.Facts.GrantPatch = landingGrantPatch(g)
		c.Drill.Facts.GrantCI = ci.GrantCI
		c.Drill.EnsurePinnedCI = ci.EnsureGrantCI
		if heads := s.agentRuntimeMains(); heads != nil {
			c.Drill.SiblingHeads = siblingHeads(heads)
		}
	}
	if c.Env == "prod" {
		// A new live landing needs every reach the landing cards need (CUT
		// §11.6 v3 item 9): without them the Owner could not be asked.
		if !controller.LandingCardsComplete(c.LandingCards) {
			slog.Error("agentrt landing live disabled: the landing cards cannot reach the question store")
			c.Drill, c.DrillGit = nil, nil
			return
		}
		c.Live = true // land/prod branches are kept (B-05): no Delete
	} else {
		c.Drill.Delete = g.Delete
	}
}

// mainEvidenceReader reads main for the reconciler with the environment's
// Git identity.
func mainEvidenceReader(g *landinggit.Git) store.MainEvidenceFunc {
	return func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
		if repo != "bifrost-platform" {
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
	case os.Getenv("AGENTRT_LANDING_LIVE") != "on":
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
