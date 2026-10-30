package server

import (
	"context"

	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: question-store notification environment, no listener or network.
func TestStgCardAdapterOwnerIdentity(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	for _, principal := range []string{"admin", "owner", "renamed-administrator"} {
		for _, role := range []string{"admin", "operator", "viewer"} {
			t.Run(principal+"/"+role, func(t *testing.T) {
				ctx := context.Background()
				rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
				q := rt.landingCardQuestions()
				tr := store.CardTrigger{Family: "stg", Kind: store.CardSTGBlocked, Seq: 1, Block: strings.Repeat("a", 16), Choices: []string{"ack", "remind_later"}}
				req := store.LandingCardRequest("bifrost-platform", tr, "STG decision")
				id, _, err := q.AskRaw(ctx, strings.Repeat("a", 32), req)
				must708(t, err)
				res := rt.svc.Answer(ctx, "owner-console", principal, role, id, []byte(`{"option":"a"}`))
				want := http.StatusOK
				if role != "admin" {
					want = http.StatusForbidden
				}
				if res.Code != want {
					t.Fatalf("answer role=%s principal=%s: %d, want %d", role, principal, res.Code, want)
				}
				fact, found, err := q.Lookup(ctx, id)
				must708(t, err)
				if !found || fact.OwnerAnswer != (role == "admin") {
					t.Fatalf("untrusted adapter identity: %+v", fact)
				}
				if role == "admin" && (fact.AnsweredBy != principal || fact.Channel != "console") {
					t.Fatal("lost audit principal", fact)
				}
				if role != "admin" && fact.Status != "open" {
					t.Fatal("non-Owner answered", fact)
				}
				keyed, _, found, err := q.LookupKey(ctx, strings.Repeat("a", 32), req)
				must708(t, err)
				if !found || keyed.OwnerAnswer != fact.OwnerAnswer || keyed.AnsweredBy != fact.AnsweredBy {
					t.Fatal("key lookup changed authorization", keyed)
				}
				raw, found, err := rt.svc.Lookup(id)
				must708(t, err)
				if !found || raw.Answer != nil && raw.Answer.Approval {
					t.Fatal("answer became approval")
				}
			})
		}
	}
	// A rule may have the same display name as an administrator; its channel
	// still cannot become Owner authorization at either adapter entry.
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	q := rt.landingCardQuestions()
	tr := store.CardTrigger{Family: "stg", Kind: store.CardSTGBlocked, Seq: 1, Block: strings.Repeat("a", 16), Choices: []string{"ack", "remind_later"}}
	req := store.LandingCardRequest("bifrost-platform", tr, "STG decision")
	id, _, err := q.AskRaw(context.Background(), strings.Repeat("b", 32), req)
	must708(t, err)
	res := rt.svc.AnswerAsRule(context.Background(), agentRuntimeRequester, "admin", id, "a", time.Now().Add(time.Hour), "ack only")
	if res.Code != http.StatusOK {
		t.Fatal(res)
	}
	fact, _, err := q.Lookup(context.Background(), id)
	must708(t, err)
	keyed, _, _, err := q.LookupKey(context.Background(), strings.Repeat("b", 32), req)
	must708(t, err)
	if fact.OwnerAnswer || keyed.OwnerAnswer || fact.Channel != "rule" {
		t.Fatal("rule became Owner", fact, keyed)
	}
}
