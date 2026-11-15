package actor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// 2026-11-14, when the writer's checks left the store: the hold of an
// external agent's token is the intake domain's and reaches this package by
// registration. A binary where nothing registered it must refuse an agent,
// not let it through unchecked. The transaction is nil on purpose: the
// refusal comes before anything is read.
func TestAnAgentIsRefusedWithoutARegisteredHolder(t *testing.T) {
	saved := agentHolder.Swap(nil)
	defer agentHolder.Store(saved)
	err := holds(context.Background(), nil, Writer{Actor: "agent:grok-owner", Token: "tok"})
	if !errors.Is(err, ErrNotYours) || !strings.Contains(err.Error(), "no check of external agents is registered") {
		t.Fatalf("an agent with no holder registered: %v", err)
	}
}

// The positive control of the test above: with a holder registered, it is
// what decides, and it is asked about the writer given.
func TestAnAgentIsHeldByTheRegisteredHolder(t *testing.T) {
	saved := agentHolder.Load()
	defer agentHolder.Store(saved)
	refused := errors.New("the intake domain says no")
	var asked Writer
	RegisterAgentHolder(func(_ context.Context, _ pgx.Tx, w Writer) error { asked = w; return refused })
	if err := holds(context.Background(), nil, Writer{Actor: "agent:grok-owner", Token: "tok"}); !errors.Is(err, refused) || asked.Token != "tok" {
		t.Fatalf("the registered holder did not decide: %v, asked %+v", err, asked)
	}
}

// The day's limit of new threads is ThreadsPerDay until a domain registers
// where a rule sets it; a registered reader's answer is the limit.
func TestThreadLimitIsTheDefaultUntilRegistered(t *testing.T) {
	saved := threadLimit.Swap(nil)
	defer threadLimit.Store(saved)
	if n, err := threadLimitIn(context.Background(), nil); err != nil || n != ThreadsPerDay {
		t.Fatalf("the limit with nothing registered: %d, %v", n, err)
	}
	RegisterThreadLimit(func(context.Context, pgx.Tx) (int, error) { return 7, nil })
	if n, err := threadLimitIn(context.Background(), nil); err != nil || n != 7 {
		t.Fatalf("the registered limit: %d, %v", n, err)
	}
}

// An actor's name says its kind; a name that says none is no actor's.
func TestKindOf(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{"owner": "owner", "controller": "controller", "thread:s1": "thread", "seat:planner": "seat",
		"host:agent-06": "host", "exec:codex": "executor", "agent:dots": "agent", "somebody": "", "": ""} {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", name, got, want)
		}
	}
	if _, err := ThreadActor("has space"); err == nil {
		t.Error("a session id with a space was taken")
	}
	if a, err := ThreadActor("local_abc-1"); err != nil || a != "thread:local_abc-1" {
		t.Errorf("ThreadActor: %q, %v", a, err)
	}
}
