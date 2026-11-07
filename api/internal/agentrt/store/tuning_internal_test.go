package store

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// TestTuningDefaults: each registry key the store reads defaults to the
// constant it replaced, so without a tuning rule nothing changes.
func TestTuningDefaults(t *testing.T) {
	t.Parallel()
	var none *rules.Tuning
	for name, want := range map[string]time.Duration{
		rules.TuneLeaseTTL:             LeaseTTL,
		rules.TuneRetryBackoff:         retryBackoff,
		rules.TuneNudgeAfter:           NudgeAfter,
		rules.TuneUnknownResetCooldown: unknownResetCooldown,
	} {
		if got := none.Duration(name); got != want {
			t.Errorf("%s defaults to %v; the code's is %v", name, got, want)
		}
	}
	for name, want := range map[string]int{
		rules.TuneAssessPerPass:    AssessPerPass,
		rules.TuneFailuresToAlert:  FailuresToAlert,
		rules.TuneThreadsPerDay:    ThreadsPerDay,
		rules.TuneNudgesPerPass:    nudgesPerPass,
		rules.TuneGateAttemptsEach: gateAttemptsEach,
	} {
		if got := none.Int(name); got != want {
			t.Errorf("%s defaults to %d; the code's is %d", name, got, want)
		}
	}
}

// TestTuningWaits: a run read under a tuning rule waits its retry_backoff_s
// after a first failure, doubling; a refusal naming no reset cools for the
// rule's unknown_reset_cooldown_s.
func TestTuningWaits(t *testing.T) {
	t.Parallel()
	run := failureRun{first: 10 * time.Second}
	if run.backoff(1) != 10*time.Second || run.backoff(3) != 40*time.Second || (failureRun{}).backoff(1) != retryBackoff {
		t.Fatalf("backoff %v %v %v", run.backoff(1), run.backoff(3), (failureRun{}).backoff(1))
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	refused := PoolReading{Pool: "codex:acct:five_hour", Limited: true}
	if got := cooldownUntilWith(refused, now, time.Hour); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("cooled until %v", got)
	}
	if got := cooldownUntil(refused, now); !got.Equal(now.Add(unknownResetCooldown)) {
		t.Fatalf("by default cooled until %v", got)
	}
}
