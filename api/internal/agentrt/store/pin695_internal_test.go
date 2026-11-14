package store

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Exercise the same route calculation the claim rechecks, with different
// executors and equal providers so executor equality cannot mask the defect.
func Test695PinnedReviewProviderIndependence(t *testing.T) {
	t.Parallel()
	cp := capabilities{candidates: map[string][]jobCandidate{"reviewer": {
		{id: "reviewer/exec:codex/openai/*", executor: "exec:codex", provider: "openai", pattern: "*"},
		{id: "reviewer/exec:claude/anthropic/*", executor: "exec:claude", provider: "anthropic", pattern: "*"},
	}}}
	c := candidate{role: "review", subject: "subject", subjectPin: rules.PinCoordinator}
	for _, provider := range []string{"anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			in := cp.input(c, []priorAttempt{{id: "subject-attempt", executor: "exec:claude", provider: &provider}})
			route, _ := in.decide()
			if provider == "openai" {
				if len(route) != 0 {
					t.Fatalf("forged openai subject routes to %v", route)
				}
			} else if !slices.Equal(route, []string{"reviewer/exec:codex/openai/*"}) {
				t.Fatalf("independent control routes to %v", route)
			}
		})
	}
}

// Capture what pinnedSpec really writes, preserving every command and argv
// position. A same-length list of ["true"] must fail this check.
func Test695PinnedSpecFullVerify(t *testing.T) {
	t.Parallel()
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
		t.Run(repo, func(t *testing.T) {
			t.Parallel()
			var body []byte
			tx := &reviewTx{exec: func(sql string, args []any) error {
				if strings.Contains(sql, "INSERT INTO agentrt.artifact_blob") {
					body = slices.Clone(args[1].([]byte))
				}
				return nil
			}}
			if _, err := pinnedSpec(context.Background(), tx, TaskProposal{Title: "fixture", Repo: repo}); err != nil {
				t.Fatal(err)
			}
			var spec struct{ Verify [][]string }
			if err := json.Unmarshal(body, &spec); err != nil {
				t.Fatal(err)
			}
			want := Repos[repo].Verify
			if len(want) == 0 || !slices.EqualFunc(spec.Verify, want, slices.Equal) {
				t.Fatalf("Verify = %q; want %q", spec.Verify, want)
			}
		})
	}
}
