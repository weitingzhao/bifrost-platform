package store

import (
	"strings"
	"testing"
	"time"
)

// Every policy_alert names its artifact but repeated_failure, whose event's
// attempt is what it is about (TD-381).
func TestPolicyAlertNamesItsArtifact(t *testing.T) {
	t.Parallel()
	sha := "sha256:" + strings.Repeat("ab", 32)
	for _, c := range []struct {
		body map[string]any
		ok   bool
	}{
		{map[string]any{"rule": "repeated_failure", "source": "attempt", "phrase": repeatedFailurePhrase}, true},
		{map[string]any{"rule": "local_path", "source": "review", "phrase": localPathPhrase, "artifact_id": sha}, true},
		{map[string]any{"rule": "local_path", "source": "review", "phrase": localPathPhrase}, false},
		{map[string]any{"rule": "approval_claim", "source": "message", "phrase": "Owner 已批准"}, false},
		{map[string]any{"rule": "workspace_ref", "source": "report", "phrase": workspaceRefPhrase}, false},
	} {
		if err := checkBody("policy_alert", c.body); (err == nil) != c.ok {
			t.Errorf("%v: %v; want ok %v", c.body, err, c.ok)
		}
	}
}

// The waits after one, two and three failures in a row, and what holds an
// executor back.
func TestBackoff(t *testing.T) {
	t.Parallel()
	for n, want := range map[int]time.Duration{0: 0, 1: 30 * time.Second, 2: time.Minute, 3: 2 * time.Minute} {
		if got := backoff(n); got != want {
			t.Errorf("backoff(%d) = %v; want %v", n, got, want)
		}
	}
	now := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	run := failureRun{now: now, by: map[string]runOf{
		"exec:cursor": {n: 1, last: now.Add(-29 * time.Second)},
		"exec:codex":  {n: 2, last: now.Add(-61 * time.Second)},
		"exec:claude": {n: failuresToStop, last: now.Add(-24 * time.Hour)},
	}}
	for executor, held := range map[string]bool{"exec:cursor": true, "exec:codex": false, "exec:claude": true, "exec:deterministic": false} {
		if why := run.holdsBack(executor); (why != "") != held {
			t.Errorf("%s held back %q; want held %v", executor, why, held)
		}
	}
}

// After one more round past the withdrawals (TD-395): an executor withdrawn
// before it is offered the task at once and withdrawn again by one more
// failure; one that was not withdrawn before it keeps its run as it was.
func TestHoldPastTheWithdrawals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC)
	run := failureRun{now: now, lifted: true, by: map[string]runOf{
		"exec:codex":  {n: failuresToStop, last: now.Add(-time.Hour)},
		"exec:cursor": {n: failuresToStop + 1, since: 1, last: now.Add(-time.Hour)},
		"exec:claude": {n: 2, since: 2, last: now.Add(-time.Hour)},
		"exec:other":  {n: failuresToStop, since: 1, last: now.Add(-time.Hour)},
	}}
	for executor, want := range map[string]bool{"exec:codex": false, "exec:cursor": true, "exec:claude": false, "exec:other": true} {
		if why, withdrawn := run.hold(executor); withdrawn != want || (why != "") != want {
			t.Errorf("%s: %q withdrawn %v; want withdrawn %v", executor, why, withdrawn, want)
		}
	}
	run.by["exec:claude"] = runOf{n: 1, since: 1, last: now.Add(-10 * time.Second)}
	if why, withdrawn := run.hold("exec:claude"); withdrawn || why == "" {
		t.Errorf("a first failure after the round: %q withdrawn %v; want a wait", why, withdrawn)
	}
}
