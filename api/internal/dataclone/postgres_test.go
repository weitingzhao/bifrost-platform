package dataclone

import (
	"context"
	"errors"
)

// The tests of this package came from api/internal/cluster on 2026-11-17,
// where they set the CNPG primary (primaryOverride) and replaced the pod exec
// (SetPodExecForTest) on cluster's Service. testPostgres stands in for
// cluster's adapter the same way, so the tests read as they did: the primary
// given, and each command handed to the fake exec as cluster handed it
// (namespace data, container postgres).

// PodExecFunc is cluster.PodExecFunc's shape.
type PodExecFunc func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error)

type testPostgres struct {
	primary string
	exec    PodExecFunc
}

func (p *testPostgres) ResolveCNPGPrimary(context.Context) (string, error) {
	if p.primary == "" {
		return "", errors.New("test: no CNPG primary set")
	}
	return p.primary, nil
}

func (p *testPostgres) ExecOnPrimary(ctx context.Context, primary string, command ...string) (string, error) {
	if p.exec == nil {
		return "", errors.New("test: no pod exec set")
	}
	return p.exec(ctx, "", "data", primary, "postgres", command...)
}

func (s *Service) testPostgres() *testPostgres {
	if p, ok := s.pg.(*testPostgres); ok {
		return p
	}
	p := &testPostgres{}
	s.pg = p
	return p
}

// SetPodExecForTest replaces the pod exec, as cluster's did.
func (s *Service) SetPodExecForTest(fn PodExecFunc) { s.testPostgres().exec = fn }

// setPrimaryForTest sets the CNPG primary, as cluster's primaryOverride did.
func (s *Service) setPrimaryForTest(pod string) { s.testPostgres().primary = pod }
