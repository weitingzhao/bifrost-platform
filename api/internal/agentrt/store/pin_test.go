package store_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
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

// An envelope's external host entry names what is there: never a host
// registered as another kind, nor a seat or executor not registered. A host
// not registered yet may be named. Refusals first, each one change from the
// envelope that signs, which comes last.
func TestEnvelopeEntryNamesWhatIsThere(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": []string{coordinatorSeat}, "note": "test: the coordinator's seat"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'seat', 'Coordinator seat')`,
			coordinatorSeat)
		return err
	}))
	s := signer{t, rptest.NewKey(t)}
	text := strings.Replace(pinEnvelope(1, rules.Initial, true), "hosts: [h1, coordinator-mbp]", "hosts: [h1]", 1)
	for name, edit := range map[string][3]string{
		"an ordinary host":      {"host: coordinator-mbp", "host: h2", "registered as a host of another kind"},
		"a seat not registered": {"proposer: seat:coordinator", "proposer: seat:nobody", "not a registered seat"},
		"an executor not there": {"executor: exec:claude", "executor: exec:gemini", "not registered"},
	} {
		_, err := s.sign(w, strings.Replace(text, edit[0], edit[1], 1))
		if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), edit[2]) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok(t, firstErr(s.sign(w, text)))
}
