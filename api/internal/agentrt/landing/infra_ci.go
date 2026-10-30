package landing

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// InfraCIName identifies one immutable preparation, independent of workers
// and restarts. Platform keeps CIName's existing operation/try identity.
func InfraCIName(revision, base, platform string, generation int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("bifrost-trade-infra|%s|%s|%s|%d", revision, base, platform, generation)))
	return fmt.Sprintf("landing-ci-infra-%x-%d", sum[:12], generation)
}

// CIRecord preserves the platform record shape; infra additionally records
// the selected object's UID and all six pipeline results on ci_seen.
func CIRecord(op string, ci CIRun) map[string]any {
	r := map[string]any{"v": Version, "kind": "ci_seen", "op": op, "run": ci.Run,
		"status": ci.Status, "R": ci.R, "created_at": ci.CreatedAt.Format(time.RFC3339Nano)}
	if ci.Results != nil {
		values := map[string]any{}
		for name, value := range ci.Results {
			values[name] = value
		}
		r["uid"], r["results"] = ci.UID, values
	}
	return r
}

// CIChanged includes infra's object identity/results even when Kubernetes
// reused a name and creation second. Legacy platform comparisons stay intact.
func CIChanged(prior Record, ci CIRun) bool {
	if prior.String("run") != ci.Run || prior.String("status") != ci.Status || !prior.Time("created_at").Equal(ci.CreatedAt) {
		return true
	}
	if ci.Results == nil {
		return false
	}
	values, ok := prior["results"].(map[string]any)
	if !ok || prior.String("uid") != ci.UID || len(values) != len(ci.Results) {
		return true
	}
	for name, value := range ci.Results {
		if values[name] != value {
			return true
		}
	}
	return false
}

func validCIResults(v any) bool {
	values, ok := v.(map[string]any)
	if !ok || len(values) > 6 {
		return false
	}
	for name, value := range values {
		known := false
		for _, result := range LandingRepos["bifrost-trade-infra"].CIResults {
			known = known || result == name
		}
		s, text := value.(string)
		if !known || !text || len(s) > 256 {
			return false
		}
	}
	return true
}
