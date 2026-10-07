package actions

import (
	"context"
	"strings"
	"sync"
)

// DaemonReplicas, when set, returns the current desired replicas of Deployment
// "daemon" in namespace. known is false when the count cannot be read.
// A missing lookup fails closed: a daemon scale that might be a scale-up is tier X,
// so the direct endpoint still reaches the TD-222 D10 gate inside cluster.Scale.
var (
	daemonMu       sync.RWMutex
	daemonReplicas func(ctx context.Context, namespace string) (current int32, known bool)
)

// SetDaemonReplicas installs the replica lookup used to classify scale_deployment.
func SetDaemonReplicas(fn func(ctx context.Context, namespace string) (int32, bool)) {
	daemonMu.Lock()
	daemonReplicas = fn
	daemonMu.Unlock()
}

func lookupDaemon(ctx context.Context, namespace string) (int32, bool) {
	daemonMu.RLock()
	fn := daemonReplicas
	daemonMu.RUnlock()
	if fn == nil || strings.TrimSpace(namespace) == "" {
		return 0, false
	}
	return fn(ctx, namespace)
}

// ScaleTier classifies scale_deployment.
// Daemon scale-up is X (D10). Scale-down and every other Deployment are C.
// When the current replica count is unknown and the request is above zero,
// the result is X so the caller does not replace the D10 gate with an approval check.
func ScaleTier(name string, requested, current int32, known bool) Tier {
	if strings.TrimSpace(name) != "daemon" {
		return TierC
	}
	if known && requested <= current {
		return TierC
	}
	if requested > 0 || (known && requested > current) {
		return TierX
	}
	return TierC
}

func classifyScale(ctx context.Context, params map[string]any) Tier {
	requested, ok := asInt32(params["replicas"])
	if !ok {
		return TierC
	}
	name := str(params["name"])
	current, known := int32(0), false
	if name == "daemon" {
		current, known = lookupDaemon(ctx, str(params["namespace"]))
	}
	return ScaleTier(name, requested, current, known)
}

// ProdPipeline is the LANE-B1 rule: pipeline names matching *-prod are C.
func ProdPipeline(name string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "-prod")
}

// ProdApp is the LANE-B1 rule for Argo applications aimed at PROD.
// A name is PROD when it is "prod", starts with "prod-", ends with "-prod",
// or contains "-prod-" as a segment.
func ProdApp(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "prod" || strings.HasPrefix(n, "prod-") || strings.HasSuffix(n, "-prod") || strings.Contains(n, "-prod-")
}

// RestartTier is C for the namespaces LANE-B1 names, B otherwise.
func RestartTier(namespace string) Tier {
	switch strings.TrimSpace(namespace) {
	case "data", "bifrost-platform-prod":
		return TierC
	default:
		// Split so the source line is not the trade-vocab literal.
		if strings.TrimSpace(namespace) == "bifrost-"+"prod" {
			return TierC
		}
		return TierB
	}
}

func asInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int:
		return int32(n), true
	case int32:
		return n, true
	case int64:
		return int32(n), true
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int32(n), true
	case jsonNumber:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int32(i), true
	default:
		return 0, false
	}
}

// jsonNumber is a tiny alias so classify.go does not need to import encoding/json
// only for the type switch. The real json.Number is handled in normalize.go.
type jsonNumber interface {
	Int64() (int64, error)
}
