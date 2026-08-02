package devsession

import (
	"context"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/selfhealth"
)

// Service routes List/Logs/Control to the env-appropriate SessionProvider.
type Service struct {
	provider SessionProvider
	mode     string
	env      string
}

// NewService chooses BdevProvider for local/dev seats and K8sProvider for stg/prod.
// Local make start stays on bdev even when clusters.yaml pins viewer_env: prod
// (ResolveViewerEnv already ignores that pin outside the cluster).
func NewService(cfg *config.Config, clusterSvc *cluster.Service) *Service {
	env := selfhealth.ResolveViewerEnv(cfg)
	switch env {
	case "stg", "prod":
		configDir := ""
		if cfg != nil {
			configDir = cfg.ConfigDir()
		}
		catalog, err := LoadSessionsCatalog(configDir)
		if err != nil {
			// Fall back to empty catalog; List returns [] rather than failing startup.
			catalog = &SessionsCatalog{Envs: map[string][]CatalogEntry{}}
		}
		return &Service{
			provider: NewK8sProvider(clusterSvc, catalog, env),
			mode:     ModeK8s,
			env:      env,
		}
	default:
		return &Service{
			provider: NewBdevProvider(env),
			mode:     ModeBdev,
			env:      env,
		}
	}
}

// Mode returns "bdev" or "k8s".
func (s *Service) Mode() string { return s.mode }

// Env returns the resolved viewer seat.
func (s *Service) Env() string { return s.env }

func (s *Service) List(ctx context.Context) ([]DevSession, error) {
	sessions, err := s.provider.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].Mode == "" {
			sessions[i].Mode = s.mode
		}
		if sessions[i].Env == "" {
			sessions[i].Env = s.env
		}
	}
	return sessions, nil
}

func (s *Service) Logs(ctx context.Context, name string, lines int) (*LogResponse, error) {
	return s.provider.Logs(ctx, strings.TrimSpace(name), lines)
}

func (s *Service) Control(ctx context.Context, name, action string) (*ControlResponse, error) {
	return s.provider.Control(ctx, strings.TrimSpace(name), strings.TrimSpace(action))
}
