package vision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const devNamespace = "bifrost-dev"

type Service struct {
	cfg       *config.Config
	configDir string
	prober    *probe.Prober
	store     *Store
	s3store   *S3Store
	v2store   *V2Store
	delivery  *delivery.Service
}

func NewService(cfg *config.Config) *Service {
	entry := cfg.DefaultCluster()
	dir := configDirFrom(cfg)
	return &Service{
		cfg:       cfg,
		configDir: dir,
		prober:    probe.NewProber(),
		store:     NewStore(dir),
		s3store:   NewS3Store(dir),
		v2store:   NewV2Store(dir),
		delivery:  delivery.NewService(entry),
	}
}

func configDirFrom(cfg *config.Config) string {
	if cfg == nil || cfg.ConfigPath == "" {
		return "config"
	}
	return filepath.Dir(cfg.ConfigPath)
}

func (s *Service) V1Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.store.LoadGate()
	signoff, _ := s.store.LoadSignoff()
	checks := s.collectV1Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	blockers := gateBlockers(checks)
	ready := result == "pass" && len(blockers) == 0
	reach := probe.ReachOK
	if result == "fail" {
		reach = probe.ReachFail
	} else if !ready {
		reach = probe.ReachDegraded
	}
	out := V1GateResponse{
		Milestone:    "V1",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision V1 dev inner-loop gate %s (%d checks)", result, len(checks)),
		GeneratedAt:  now,
	}
	if rec != nil {
		at := rec.At.UTC()
		out.At = &at
		if rec.Result != "" {
			out.Result = rec.Result
			out.Ready = rec.Result == "pass" && len(blockers) == 0
		}
	}
	if signoff != nil {
		at := signoff.At.UTC()
		out.SignedAt = &at
		out.SignedBy = signoff.SignedBy
	}
	return out
}

func (s *Service) RunV1Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectV1Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	rec := GateRecord{
		At:          now,
		Result:      result,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     fmt.Sprintf("Vision V1 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.V1Gate(ctx)
	msg := fmt.Sprintf("Vision V1 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.v1-gate",
		Target:      "vision-v1-dev-topology",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignV1(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.V1Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision V1 gate not ready — run gate first and fix blockers")
	}
	rec := V1SignoffRecord{
		At:       now,
		SignedBy: signedBy,
		Notes:    strings.TrimSpace(notes),
		GateAt:   now,
		Result:   "SIGNED",
	}
	if gate.At != nil {
		rec.GateAt = *gate.At
	}
	if err := s.store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.V1Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.v1-signoff",
		Target:      "vision-v1-dev-topology",
		Changed:     true,
		Message:     "Vision V1 dev inner-loop SIGNED",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectV1Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 16)
	checks = append(checks, s.checkDevNamespace(ctx))
	checks = append(checks, s.checkDevRedisPG(ctx)...)
	smoke := s.delivery.DevSmoke(ctx)
	if len(smoke.Targets) == 0 {
		checks = append(checks, GateCheck{
			ID: "dev-smoke", Label: "K3s dev smoke (9 APIs)", Required: true,
			Reachability: probe.ReachFail,
			Detail:       smoke.Detail,
		})
	} else {
		for _, t := range smoke.Targets {
			checks = append(checks, GateCheck{
				ID: t.ID, Label: t.ID, Required: strings.HasPrefix(t.ID, "dev-api-"),
				Reachability: t.Reachability, Detail: t.Detail,
			})
		}
	}
	checks = append(checks, s.checkDevMatrix(ctx))
	checks = append(checks, s.checkFrontendK3sEnv())
	return checks
}

func (s *Service) checkDevNamespace(ctx context.Context) GateCheck {
	check := GateCheck{
		ID: "dev-namespace", Label: "bifrost-dev workloads", Required: true,
		Reachability: probe.ReachFail, Detail: "cluster unavailable",
	}
	clientset, _, err := s.delivery.ClusterClient()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	deploys, err := clientset.AppsV1().Deployments(devNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if len(deploys.Items) == 0 {
		check.Detail = "no deployments in bifrost-dev"
		return check
	}
	ready := 0
	notReady := []string{}
	for _, d := range deploys.Items {
		if d.Name == "celery-worker" && d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
			continue
		}
		if d.Status.ReadyReplicas > 0 && d.Status.ReadyReplicas >= d.Status.Replicas {
			ready++
		} else {
			notReady = append(notReady, d.Name)
		}
	}
	if len(notReady) == 0 {
		check.Reachability = probe.ReachOK
		check.Detail = fmt.Sprintf("%d deployment(s) ready in %s", ready, devNamespace)
	} else {
		check.Reachability = probe.ReachDegraded
		check.Detail = fmt.Sprintf("not ready: %s", strings.Join(notReady, ", "))
	}
	return check
}

func (s *Service) checkDevRedisPG(ctx context.Context) []GateCheck {
	reach, detail := s.delivery.DevRedisInCluster(ctx)
	redis := GateCheck{
		ID: "dev-redis", Label: "In-cluster Redis (bifrost-dev)", Required: true,
		Reachability: reach, Detail: detail,
	}
	pgReach, pgDetail := s.delivery.DevPostgresInCluster(ctx)
	pg := GateCheck{
		ID: "dev-postgres", Label: "In-cluster PostgreSQL (bifrost-dev)", Required: true,
		Reachability: pgReach, Detail: pgDetail,
	}
	return []GateCheck{redis, pg}
}

func (s *Service) checkDevMatrix(ctx context.Context) GateCheck {
	check := GateCheck{
		ID: "dev-matrix", Label: "Dev matrix (K3s gateway)", Required: true,
		Reachability: probe.ReachFail, Detail: "config unavailable",
	}
	if s.cfg == nil {
		return check
	}
	env, ok := s.cfg.GetEnvironment("dev")
	if !ok {
		check.Detail = "dev environment not configured"
		return check
	}
	matrix := s.prober.ProbeEnvironment(ctx, *env)
	failIDs := []string{}
	for _, t := range matrix.Targets {
		if t.Category == "trade_write" {
			continue
		}
		if t.ID == "redis" {
			if reach, _ := s.delivery.DevRedisInCluster(ctx); reach == probe.ReachOK {
				continue
			}
		}
		if t.Reachability == probe.ReachFail {
			failIDs = append(failIDs, t.ID)
		}
	}
	check.Detail = fmt.Sprintf("probed via %s", env.NginxBase)
	if len(failIDs) > 0 {
		check.Detail = fmt.Sprintf("failing: %s", strings.Join(failIDs, ", "))
	} else {
		check.Reachability = probe.ReachOK
		check.Detail = "no failing dev targets"
	}
	return check
}

func (s *Service) checkFrontendK3sEnv() GateCheck {
	check := GateCheck{
		ID: "frontend-env-k3s", Label: "Frontend .env.development.k3s template", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := os.Getenv("PLATFORM_VISION_V1_FRONTEND_ENV")
	candidates := []string{}
	if path != "" {
		candidates = append(candidates, path)
	}
	if s.configDir != "" {
		candidates = append(candidates, filepath.Join(s.configDir, "..", "..", "bifrost-trade-frontend", ".env.development.k3s"))
	}
	candidates = append(candidates, filepath.Join("..", "bifrost-trade-frontend", ".env.development.k3s"))
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			check.Reachability = probe.ReachOK
			check.Detail = "Mac Vite + 1 local API; remaining VITE_API_* → K3s dev gateway (" + filepath.Base(abs) + ")"
			return check
		}
	}
	check.Detail = "create bifrost-trade-frontend/.env.development.k3s (see Vision V1 deliverables)"
	return check
}

func gateBlockers(checks []GateCheck) []string {
	out := []string{}
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			out = append(out, c.Label+": "+c.Detail)
		}
	}
	return out
}
