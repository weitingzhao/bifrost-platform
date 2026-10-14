package server

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type approvalLoopSpy struct {
	retries, finishes int
}

func (s *approvalLoopSpy) RunRetries(context.Context, time.Duration)      { s.retries++ }
func (s *approvalLoopSpy) RunHeldFinishes(context.Context, time.Duration) { s.finishes++ }

// Workers/all were already green: one RunRetries and no separate flusher.
// API was red: no loop at all. This checks the production role dispatch.
func TestApprovalRoleHasOneResultLoop(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorkers, config.RoleAll} {
		t.Run(role.String(), func(t *testing.T) {
			spy := &approvalLoopSpy{}
			runApprovalLoop(context.Background(), spy, role.RunsWorkers(), approvalRetryInterval)
			if spy.retries+spy.finishes != 1 || (spy.retries == 1) != role.RunsWorkers() {
				t.Fatalf("role=%s retries=%d finishes=%d", role, spy.retries, spy.finishes)
			}
			if approvalRetryInterval != 15*time.Second {
				t.Fatalf("interval=%s", approvalRetryInterval)
			}
		})
	}
}

func TestCloseCancelsApprovalLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{approvalsCancel: cancel}
	srv.Close()
	srv.Close()
	if ctx.Err() != context.Canceled {
		t.Fatal("Close did not cancel approval loop")
	}
}

// The review found that deleting the startApprovalLoop call in New left every
// test green. This builds the server the way production does, per role.
func TestNewStartsTheApprovalLoopInEveryRole(t *testing.T) {
	for _, role := range []string{"api", "all"} {
		t.Run(role, func(t *testing.T) {
			t.Setenv(config.RoleEnv, role)
			srv := newTestServer(t, newTestConfig(t))
			if srv.approvalsCancel == nil {
				t.Fatal("New did not start the approval loop")
			}
		})
	}
}
