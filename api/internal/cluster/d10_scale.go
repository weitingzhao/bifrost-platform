package cluster

import (
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

// refuseDaemonScaleUp blocks every replica increase of Deployment "daemon"
// while spine D10 is not UNLOCKED. A decrease, or the same count, is allowed.
// The deployment name is the canonical one (AGENT_FACTS §8a); Trade runs it in
// bifrost-dev, bifrost-stg and bifrost-prod. Any other namespace is held to
// the same rule so a second Deployment of that name cannot slip past.
func (s *Service) refuseDaemonScaleUp(name string, current, requested int32) error {
	if name != "daemon" || requested <= current {
		return nil
	}
	if s.d10Unlocked() {
		return nil
	}
	return fmt.Errorf("Trading execution is BLOCKED (D10). Daemon scale-up requires Owner unlock.")
}

// d10Unlocked reports spine decisions[id=D10].status == UNLOCKED.
// Anything else, including a missing or unreadable file, is locked.
func (s *Service) d10Unlocked() bool {
	if s != nil && s.d10StatusFn != nil {
		return strings.EqualFold(strings.TrimSpace(s.d10StatusFn()), "UNLOCKED")
	}
	path := ""
	if s != nil {
		path = s.opsContextPath
	}
	return spineD10Status(path) == "UNLOCKED"
}

// spineD10Status reads decisions[id=D10].status from ops-context.yaml.
// preflight.js reads the same field and fails closed to BLOCKED.
func spineD10Status(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "BLOCKED"
	}
	file, err := opscontext.Load(path)
	if err != nil || file == nil {
		return "BLOCKED"
	}
	for _, d := range file.Decisions {
		if strings.TrimSpace(d.ID) != "D10" {
			continue
		}
		status := strings.ToUpper(strings.TrimSpace(d.Status))
		if status == "" {
			return "BLOCKED"
		}
		return status
	}
	return "BLOCKED"
}
