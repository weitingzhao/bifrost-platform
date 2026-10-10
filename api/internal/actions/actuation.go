package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// PlanSummary is the slice of a plan the classifier needs.
type PlanSummary struct {
	Ready      bool
	Namespaces []string
	Daemon     bool
	// Path and Objects feed the approval summary only.
	Path    string
	Objects int
}

var (
	actMu      sync.RWMutex
	actPolicy  *actuationpolicy.Policy
	planLookup func(ctx context.Context, planID string) (PlanSummary, error)
	// commandRunner is installed only by tests. ExecuteOwnerRunCommand never calls it.
	commandRunner func(ctx context.Context, command string) error
)

// SetActuationPolicy installs the allow-list used to classify the generic actions.
func SetActuationPolicy(p *actuationpolicy.Policy) {
	actMu.Lock()
	actPolicy = p
	actMu.Unlock()
}

// SetPlanLookup installs the plan reader used to classify apply_manifest.
func SetPlanLookup(fn func(ctx context.Context, planID string) (PlanSummary, error)) {
	actMu.Lock()
	planLookup = fn
	actMu.Unlock()
}

// SetCommandRunner installs a shell. owner_run_command does not call it.
func SetCommandRunner(fn func(ctx context.Context, command string) error) {
	actMu.Lock()
	commandRunner = fn
	actMu.Unlock()
}

func currentPolicy() *actuationpolicy.Policy {
	actMu.RLock()
	defer actMu.RUnlock()
	return actPolicy
}

func tierOf(s string) Tier {
	switch s {
	case "B":
		return TierB
	case "C":
		return TierC
	case "D":
		return TierD
	case "X":
		return TierX
	default:
		return TierX
	}
}

func classifyPlanManifest(_ context.Context, _ map[string]any) Tier {
	return TierB
}

func classifyApply(ctx context.Context, params map[string]any) Tier {
	p := currentPolicy()
	actMu.RLock()
	look := planLookup
	actMu.RUnlock()
	if p == nil || look == nil {
		return TierC
	}
	summary, err := look(ctx, str(params["plan_id"]))
	if err != nil || !summary.Ready {
		return TierC
	}
	if summary.Daemon {
		return TierX
	}
	tier, err := p.ManifestTier(summary.Namespaces, false)
	if err != nil {
		return TierX
	}
	return tierOf(tier)
}

func classifyJob(_ context.Context, params map[string]any) Tier {
	p := currentPolicy()
	if p == nil {
		return TierX
	}
	tier, err := p.JobTier(str(params["namespace"]))
	if err != nil {
		return TierX
	}
	return tierOf(tier)
}

func classifyProbe(_ context.Context, params map[string]any) Tier {
	p := currentPolicy()
	if p == nil {
		return TierX
	}
	envFrom := str(params["env_from"]) != ""
	tier, err := p.ProbeTier(str(params["namespace"]), str(params["image"]), envFrom)
	if err != nil {
		return TierX
	}
	return tierOf(tier)
}

// OwnerRunResult is the handoff stored on approve. The platform does not run
// the command: the approval stays approved until owner-run.sh (runner owner)
// or the host executor (runner host) claims it and posts the result.
func OwnerRunResult(approvalID, command string) map[string]any {
	if approvalID == "" {
		approvalID = "<id>"
	}
	sum := sha256.Sum256([]byte(command))
	script := fmt.Sprintf("bash scripts/owner/owner-run.sh %s", approvalID)
	return map[string]any{
		"recorded":             true,
		"executed_by_platform": false,
		"command":              command,
		"command_sha256":       hex.EncodeToString(sum[:]),
		"message":              "Run: " + script,
	}
}

// Handoff is the result stored on approve for an action the platform does
// not run itself (runner owner or host).
func Handoff(id, approvalID string, params map[string]any) any {
	switch id {
	case "owner_run_command":
		return OwnerRunResult(approvalID, str(params["command"]))
	case "rolling_reboot":
		return RollingRebootResult(approvalID)
	}
	return nil
}

// ExecuteOwnerRunCommand records the command. It does not call commandRunner.
func ExecuteOwnerRunCommand(ctx context.Context, params map[string]any) (any, error) {
	id := ""
	if rc := chi.RouteContext(ctx); rc != nil {
		id = rc.URLParam("id")
	}
	return OwnerRunResult(id, str(params["command"])), nil
}
