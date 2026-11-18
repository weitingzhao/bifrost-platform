package ownerq

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, channel W03: wait_for_answer read the question every two
// seconds and the two owner.question topics were registered but never written.
func TestAQuestionsLifeReachesTheBusUnderItsContract(t *testing.T) {
	want := map[string]string{"question.create": TypeAsked, "question.answer": TypeAnswered, "question.withdraw": TypeAnswered,
		"question.expire": TypeAnswered, "question.notify": "", "question.ack": "", "question.store": ""}
	for action, typ := range want {
		if got := TypeOf(action); got != typ {
			t.Errorf("%s: %q, want %q", action, got, typ)
		}
	}
	q := Question{Type: TypeAnswered, ID: "q_0123456789abcdef", Status: "expired"}
	if !Valid(q) || Valid(Question{Type: TypeAnswered, ID: "Q12", Status: "answered"}) {
		t.Fatal("Valid")
	}
	if err := eventlog.CheckBody(q.Type, Body(q)); err != nil {
		t.Fatal(err)
	}
	bad := Body(q)
	bad["subject"] = "q_ffffffffffffffff"
	if eventlog.CheckBody(q.Type, bad) == nil {
		t.Fatal("a body whose subject is not its question was taken")
	}
	for _, typ := range []string{TypeAsked, TypeAnswered} {
		if err := registry.Agree(typ); err != nil {
			t.Error(err)
		}
	}
}
