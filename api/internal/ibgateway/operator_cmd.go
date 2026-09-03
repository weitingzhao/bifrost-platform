package ibgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	operatorCmdStream       = "ib:operator:cmd"
	operatorResultPrefix    = "ib:operator:result:"
	accountSnapshotKey      = "ib:account:snapshot:v1"
	selfHealRedisKey        = "ib:control:gateway_self_heal"
	defaultSnapshotStaleSec = 90.0
)

func snapshotAgeSec(raw string, now time.Time) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return 0, false
	}
	updated, ok := asFloatOK(m["updated_at"])
	if !ok {
		return 0, false
	}
	age := float64(now.Unix()) - updated
	if updated > 1e12 {
		age = float64(now.UnixMilli()) - updated
		age = age / 1000
	}
	return age, true
}

func snapshotFresh(raw string, now time.Time, staleSec float64) bool {
	age, ok := snapshotAgeSec(raw, now)
	if !ok {
		return false
	}
	return age <= staleSec
}

func (s *Service) sendOperatorCommand(ctx context.Context, op string, timeout time.Duration) error {
	if s.cfg.RedisPlatformPass == "" {
		return fmt.Errorf("REDIS_IB_PLATFORM_PASS not configured")
	}
	reqID := fmt.Sprintf("platform-%d", time.Now().UnixNano())
	deadlineMs := time.Now().Add(timeout).UnixMilli()
	_, err := s.redisCLI(
		"XADD", operatorCmdStream, "*",
		"req_id", reqID,
		"v", "1",
		"op", op,
		"payload", "{}",
		"caller", "platform-api",
		"deadline_ms", fmt.Sprintf("%d", deadlineMs),
	)
	if err != nil {
		return err
	}
	pollDeadline := time.Now().Add(timeout)
	for time.Now().Before(pollDeadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		raw, getErr := s.redisCLI("GET", operatorResultPrefix+reqID)
		if getErr == nil && strings.TrimSpace(raw) != "" {
			var envelope map[string]any
			if json.Unmarshal([]byte(raw), &envelope) == nil {
				if ok, exists := envelope["ok"]; exists {
					if b, isBool := ok.(bool); isBool && !b {
						errMsg := strings.TrimSpace(fmt.Sprint(envelope["error"]))
						if errMsg == "" {
							errMsg = "operator returned ok=false"
						}
						return fmt.Errorf("%s", errMsg)
					}
				}
			}
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("operator command %s timed out after %s", op, timeout)
}

func (s *Service) waitSnapshotFresh(ctx context.Context, maxWait time.Duration, staleSec float64) bool {
	if staleSec <= 0 {
		staleSec = defaultSnapshotStaleSec
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		raw, err := s.redisCLI("GET", accountSnapshotKey)
		if err == nil && snapshotFresh(raw, time.Now().UTC(), staleSec) {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}
