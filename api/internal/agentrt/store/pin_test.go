package store_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Executor pins (W-31 round 4, core blocks C-0 and C-1;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.1–§2.3, §6 T3, T4, T10,
// T11, §7.1).

const (
	coordinatorSeat = "seat:coordinator"
	externalHost    = "coordinator-mbp"
)

// pinEnvelope is the dev tenant's standing envelope; withHosts adds the
// external host entry of the coordinator's Mac.
func pinEnvelope(version int, direction string, withHosts bool) string {
	body := `tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude, exec:codex]
hosts: [h1, coordinator-mbp]
`
	if withHosts {
		body += `external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform]
    roles: [implement]
`
	}
	return ruleText("standing-envelope:dev", version, direction, since, until, body)
}

// §2.2: what builds a task anywhere but the tasks route has no pin to set.
// TaskProposal (goals, briefs, plans, imports) and Task (the controller's
// checks and gates) export no pin and no session: only PinnedTask does.
func TestOnlyPinnedTaskCarriesAPin(t *testing.T) {
	t.Parallel()
	for _, v := range []any{store.TaskProposal{}, store.Task{}} {
		ty := reflect.TypeOf(v)
		for i := range ty.NumField() {
			f := ty.Field(i)
			if f.IsExported() && (strings.Contains(strings.ToLower(f.Name), "pin") || strings.Contains(strings.ToLower(f.Name), "session")) {
				t.Errorf("%s exports %s", ty.Name(), f.Name)
			}
		}
	}
	if _, ok := reflect.TypeOf(store.PinnedTask{}).FieldByName("ExecutorPin"); !ok {
		t.Fatal("PinnedTask has no ExecutorPin")
	}
}
