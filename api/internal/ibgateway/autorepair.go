package ibgateway

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// StartAutoRepair runs L1 auto rollout when plugin self-heal streak exceeds threshold.
func (s *Service) StartAutoRepair(ctx context.Context, audit *actuation.AuditLog) {
	if !s.cfg.AutoRepairEnabled {
		return
	}
	go s.autoRepairLoop(ctx, audit)
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
			s.maybeAutoRollout(ctx, audit, &lastAutoRollout, maxStreak, cooldown)
		}
	}
}

func (s *Service) maybeAutoRollout(
	ctx context.Context,
	audit *actuation.AuditLog,
	lastAutoRollout *time.Time,
	maxStreak int,
	cooldown time.Duration,
) {
	if s.cluster == nil || s.cfg.RedisPlatformPass == "" {
		return
	}
	status := s.SelfHealStatus(ctx)
	if !status.Enabled {
		return
	}
	if status.StaleStreak < maxStreak {
		return
	}
	if !status.RolloutRecommended {
		return
	}
	deployReach, mode, _, _ := s.readDeployment(ctx)
	if deployReach != probe.ReachOK || mode != "live" {
		return
	}
	accountRaw, _ := s.redisCLI("HGETALL", "bifrost:health:ws_ib_account_agent")
	account := parseRedisHash(accountRaw)
	if !strings.EqualFold(strings.TrimSpace(account["host_connected"]), "true") {
		return
	}
	if !lastAutoRollout.IsZero() && time.Since(*lastAutoRollout) < cooldown {
		return
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
	} else {
		log.Printf("ib-gateway auto-repair: action_taken=%s ok=%v", resp.ActionTaken, resp.OK)
	}
}
