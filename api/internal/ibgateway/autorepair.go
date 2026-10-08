package ibgateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// StartAutoRepair runs L1 auto rollout when plugin self-heal streak exceeds threshold.
func (s *Service) StartAutoRepair(ctx context.Context, audit *actuation.AuditLog) {
	if !s.cfg.AutoRepairEnabled {
		return
	}
	safego.Go("ibgateway.autoRepairLoop", func() { s.autoRepairLoop(ctx, audit) })
}

func (s *Service) autoRepairLoop(ctx context.Context, audit *actuation.AuditLog) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	var lastAutoRollout time.Time
	staleSec := s.cfg.SnapshotStaleSec
	if staleSec <= 0 {
		staleSec = defaultSnapshotStaleSec
	}
	maxStreak := s.cfg.SnapshotStaleMaxRollout
	if maxStreak <= 0 {
		maxStreak = 3
	}
	cooldown := time.Duration(s.cfg.AutoRolloutCooldownSec) * time.Second
	if cooldown <= 0 {
		cooldown = 900 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			safego.Do("ibgateway.maybeAutoRollout", func() {
				id := maintainer.PlatformID(maintainer.LoopIBAutoRepair)
				if err := s.maybeAutoRollout(ctx, audit, &lastAutoRollout, maxStreak, cooldown); err != nil {
					maintainer.Failure(id)
					return
				}
				maintainer.Success(id)
			})
		}
	}
}

func (s *Service) maybeAutoRollout(
	ctx context.Context,
	audit *actuation.AuditLog,
	lastAutoRollout *time.Time,
	maxStreak int,
	cooldown time.Duration,
) error {
	if s.cluster == nil || s.cfg.RedisPlatformPass == "" {
		return nil
	}
	status := s.SelfHealStatus(ctx)
	if !status.Enabled {
		return nil
	}
	if status.StaleStreak < maxStreak {
		return nil
	}
	if !status.RolloutRecommended {
		return nil
	}
	deployReach, mode, _, _ := s.readDeployment(ctx)
	if deployReach != probe.ReachOK || mode != "live" {
		return nil
	}
	account, _ := s.redisHGetAll("bifrost:health:ws_ib_account_agent")
	if !strings.EqualFold(strings.TrimSpace(account["host_connected"]), "true") {
		return nil
	}
	if !lastAutoRollout.IsZero() && time.Since(*lastAutoRollout) < cooldown {
		return nil
	}
	resp, err := s.Reconnect(ctx)
	*lastAutoRollout = time.Now().UTC()
	statusLabel := "ok"
	if !resp.OK {
		statusLabel = "failed"
	}
	if audit != nil {
		audit.RecordDirect("platform-auto-repair", actuation.RoleOperator, "ib-gateway.auto_reconnect", resp.Target, statusLabel, resp.Message)
	}
	if err != nil {
		log.Printf("ib-gateway auto-repair rollout: %v", err)
		return err
	}
	log.Printf("ib-gateway auto-repair: action_taken=%s ok=%v", resp.ActionTaken, resp.OK)
	if !resp.OK {
		return fmt.Errorf("auto-repair not ok: %s", resp.Message)
	}
	return nil
}
