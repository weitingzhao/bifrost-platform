package main

import (
	"bytes"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgpreflight"
)

func TestWriteResultsExitCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, reason string
		passed       bool
		status       string
		code         int
	}{
		{"healthy", "STG service JSON status=ok", true, "PASS", 0},
		{"unhealthy", "STG health classified failed: reason=status http_status=200", false, "FAIL", 1},
		{"unreadable", "STG health classified unknown: reason=timeout http_status=0", false, "FAIL", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := []stgpreflight.Result{
				{Name: "workers_identity", Passed: true, Reason: "workers"},
				{Name: "health", Passed: tc.passed, Reason: tc.reason},
				{Name: "exact_ci", Passed: true, Reason: "exact"},
			}
			var out bytes.Buffer
			code := writeResults(&out, results)
			want := "PASS workers_identity: workers\n" + tc.status + " health: " + tc.reason + "\nPASS exact_ci: exact\n"
			if code != tc.code || out.String() != want {
				t.Fatalf("exit=%d output=%q; want exit=%d output=%q", code, out.String(), tc.code, want)
			}
		})
	}
}
