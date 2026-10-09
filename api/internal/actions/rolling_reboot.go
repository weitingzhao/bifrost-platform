package actions

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
)

// RollingRebootResult is what approve returns for rolling_reboot. The platform
// records the approval and does not reboot nodes.
func RollingRebootResult(approvalID string) map[string]any {
	if approvalID == "" {
		approvalID = "<id>"
	}
	command := fmt.Sprintf("bash scripts/k3s/rolling-reboot.sh --execute --approval %s", approvalID)
	return map[string]any{
		"recorded":             true,
		"executed_by_platform": false,
		"message":              "Run: " + command,
		"command":              command,
	}
}

// ExecuteRollingReboot reads the approval id from the approve request context.
func ExecuteRollingReboot(ctx context.Context, _ map[string]any) (any, error) {
	id := ""
	if rc := chi.RouteContext(ctx); rc != nil {
		id = rc.URLParam("id")
	}
	return RollingRebootResult(id), nil
}
