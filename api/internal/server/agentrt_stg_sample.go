package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// TD-460: the two Tier C actions behind B5's first-opening sample. They have
// no route: an approved request on the PROD Console is the only way in, and
// the executor runs with the platform's own ServiceAccount, the same one the
// B5 workers use. The PROD runtime is the one whose landing:dev rule gates
// B5, so a platform running any other runtime refuses.
func (s *Server) stgSampler() (*stgrelease.Sampler, error) {
	if !approvalhome.IsProd() {
		return nil, errors.New("the STG sample actions run on the PROD platform only; " + approvalhome.Required)
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil || rt.env != "prod" {
		return nil, errors.New("this platform does not run the PROD agent runtime that gates B5")
	}
	if s.cluster == nil {
		return nil, errors.New("no cluster connection for the STG sample")
	}
	ledger := stgrelease.PostgresSampleLedger{DB: rt.api.DB, Tenant: stgrelease.SampleTenant}
	adapter := &delivery.STGReleaseAdapter{Archive: ledger,
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
	return &stgrelease.Sampler{Runs: adapter, Ledger: ledger, CI: s.landingCIClient().ReadRelease}, nil
}

func (s *Server) stgSampleCreate(ctx context.Context, params map[string]any) (any, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%s runs only as an approval's execution", stgrelease.SampleCreateAction)
	}
	sampler, err := s.stgSampler()
	if err != nil {
		return nil, err
	}
	out, err := sampler.Create(ctx, stgrelease.SampleCreateRequest{Approval: att.ApprovalID, Env: text(params["env"]),
		P: text(params["revision"]), U: text(params["ui_revision"]), Infra: text(params["infra_revision"])})
	return out, stgSampleError(err)
}

func (s *Server) stgSampleFinalize(ctx context.Context, params map[string]any) (any, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%s runs only as an approval's execution", stgrelease.SampleFinalizeAction)
	}
	sampler, err := s.stgSampler()
	if err != nil {
		return nil, err
	}
	out, err := sampler.Finalize(ctx, stgrelease.SampleFinalizeRequest{Approval: att.ApprovalID, Env: text(params["env"]),
		Run: text(params["run"]), UID: text(params["uid"])})
	return out, stgSampleError(err)
}

// stgSampleError: a held window is retried with the same allocation; an
// unproven Create goes to unknown and is checked by its run name.
func stgSampleError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, stgrelease.ErrSampleBusy):
		return actions.Transient(err.Error())
	case errors.Is(err, stgrelease.ErrSampleUncertain):
		return actions.Uncertain(err.Error())
	default:
		return err
	}
}

// stgSampleCreateCheck refuses a malformed request before the approval row
// is written, so the Owner never approves one that cannot run.
func stgSampleCreateCheck(action string, params map[string]any) error {
	switch action {
	case stgrelease.SampleCreateAction:
		return stgrelease.CheckSampleCreate(text(params["env"]), text(params["revision"]), text(params["ui_revision"]), text(params["infra_revision"]))
	case stgrelease.SampleFinalizeAction:
		return stgrelease.CheckSampleFinalize(text(params["env"]), text(params["run"]), text(params["uid"]))
	}
	return nil
}
