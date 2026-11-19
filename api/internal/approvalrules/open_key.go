package approvalrules

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/ruleapprove"
)

// OpenKey is the one-open-request rule of every action that has one.
//
// Two more subjects have one (2026-11-15; origin: card #575/#576, the same
// command asked twice by one session left two cards pending, and #562/#564/
// #566 for releases): an execution card is the one command it carries, and a
// release card is the pipeline it runs. A requester's newer card of the same
// subject replaces its own unrun older one (approvals/withdraw.go); another
// requester's open card of the same subject still blocks (409).
func OpenKey(action string, params map[string]any) string {
	switch action {
	case "owner_run_command":
		command, _ := params["command"].(string)
		if strings.TrimSpace(command) == "" {
			return ""
		}
		sum := sha256.Sum256([]byte(command))
		return "exec:" + hex.EncodeToString(sum[:])
	case "start_pipeline_run":
		name, _ := params["name"].(string)
		if strings.TrimSpace(name) == "" {
			return ""
		}
		return "pipeline:" + strings.TrimSpace(name)
	}
	return ruleapprove.OpenKey(action, params)
}
