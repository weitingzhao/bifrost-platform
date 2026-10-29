package landingci

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// B-01: mutate only the newest run's identity after proving that the same
// legacy entry point selects it. An identity conflict must not revive old green.
func TestReview686LegacyNewestConflict(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{"Failed", "Running", "Unknown", "Succeeded"} {
		for _, conflict := range []string{"repo-label", "duplicate-revision", "conflicting-revision", "inline"} {
			t.Run(status+"/"+conflict, func(t *testing.T) {
				t.Parallel()
				old, newest := greenRun(t, "old-green", at.Add(-time.Minute)), greenRun(t, "newest", at)
				switch status {
				case "Failed":
					setField(t, newest, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
				case "Running":
					setField(t, newest, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
				case "Unknown":
					delete(newest.Object, "status")
				}
				read := func() landing.CIRun {
					t.Helper()
					c, _ := drillService(t, old, newest)
					got, err := c.LandingCI(context.Background(), factSHA1, at.Add(-time.Hour))
					if err != nil {
						t.Fatal(err)
					}
					return got
				}
				if got := read(); got.Run != "newest" || got.Status != status {
					t.Fatal("control", got)
				}
				switch conflict {
				case "repo-label":
					newest.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-trade-infra"})
				case "duplicate-revision", "conflicting-revision":
					value := factSHA1
					if conflict == "conflicting-revision" {
						value = factSHA2
					}
					setField(t, newest, []any{map[string]any{"name": "revision", "value": factSHA1}, map[string]any{"name": "revision", "value": value}}, "spec", "params")
				case "inline":
					setField(t, newest, map[string]any{}, "spec", "pipelineSpec")
				}
				if got := read(); got.Run != "newest" || got.Status != "Unknown" || got.UID != "uid-newest" || !got.CreatedAt.Equal(at) {
					t.Fatalf("identity conflict lost newest evidence: %+v", got)
				}
			})
		}
	}
}

// B-03: each case changes exactly one field in an otherwise valid expired
// legacy record. Legal JSON with an invalid known field type is still unreadable.
func TestReview686MalformedWindowCompatibility(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"who", "what", "env", "pid", "host", "started_at", "expires_at", "reason"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			record := map[string]any{"who": "fixture", "what": "bifrost-platform", "env": "stg", "pid": 123, "host": "fixture-host", "started_at": "1999-12-31T23:55:00Z", "expires_at": "2000-01-01T00:00:00Z", "reason": "fixture"}
			c := &Client{ConfigMap: func(context.Context, string) (map[string]string, bool, error) {
				raw, err := json.Marshal(record)
				return map[string]string{releaseWindowDataKey: string(raw)}, true, err
			}}
			at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			if blocked, err := c.windowBlocked(context.Background(), at); blocked || err != nil {
				t.Fatal("valid expired control", blocked, err)
			}
			if field == "pid" {
				record[field] = "broken"
			} else {
				record[field] = 123
			}
			if blocked, err := c.windowBlocked(context.Background(), at); !blocked || err == nil {
				t.Fatal("malformed known field accepted as free window", blocked, err)
			}
		})
	}
}
