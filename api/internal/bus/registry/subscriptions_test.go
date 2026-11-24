package registry

import (
	"strings"
	"testing"
)

// 2026-11-16 (the coordinator for the Owner): a subscription row decides who
// is told and never grants anything. Each refusal below is a row that tries.
func TestCheckSubscriptionsRefuses(t *testing.T) {
	tenants := map[string]Tenant{
		"rocket": {Name: "rocket", Channels: map[string]Channel{"ci.run.finished": {}, "seat.mail.arrived": {}}},
		"demo":   {Name: "demo", Channels: map[string]Channel{"demo.thing.happened": {Exports: []string{"rocket"}}}},
	}
	tier := func(id string) (string, bool) {
		switch id {
		case "rollout_restart_deployment":
			return "B", true
		case "scale_deployment":
			return "C", true
		}
		return "", false
	}
	row := func(r Subscription) map[string]Subscriptions {
		if r.Guarantee == "" {
			r.Guarantee = "durable"
		}
		if r.By == "" {
			r.By = ByDispatcher
		}
		return map[string]Subscriptions{"rocket": {Tenant: "rocket", Rows: []Subscription{r}}}
	}
	ok := []Subscription{
		{ID: "a", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci"},
		{ID: "b", On: []string{"demo.thing.happened"}, Deliver: DeliverWake, To: "subject"},
		{ID: "c", On: []string{"ci.run.finished"}, Deliver: DeliverAction, Action: "rollout_restart_deployment"},
		{ID: "d", On: []string{"ci.run.finished"}, Deliver: DeliverAction, Action: "scale_deployment", Params: map[string]string{"mode": "request"}},
		// 2026-11-17: a refused landing's wake carries its reason (the land
		// task's refusal result); the positive control of the two below.
		{ID: "e", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"detail": "task-result:land/refusal"}},
		// 2026-11-17: the tripwire row, several names and a required one.
		{ID: "f", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"detail": "task-result:land/tripwire,commit,branch", "require": "tripwire"}},
		// 2026-11-17: a new card pages the Owner through the bus; the
		// positive control of the two notify refusals below.
		{ID: "g", On: []string{"ci.run.finished"}, Deliver: DeliverNotify, To: NotifyTo, Params: map[string]string{"title": "run {subject}"}},
		// 2026-11-17: the open regressions read no run, so any topic may ask for them, and a wake may go to whom the event acts for.
		{ID: "h", On: []string{"seat.mail.arrived"}, Deliver: DeliverWake, To: "requester", Params: map[string]string{"detail": DetailOpenRegressions, "require": "open"}},
	}
	for _, r := range ok {
		if err := CheckSubscriptions(tenants, row(r), tier); err != nil {
			t.Errorf("positive control %s: %v", r.ID, err)
		}
	}
	refused := map[string]Subscription{
		"is neither in":                    {ID: "x", On: []string{"git.main.advanced"}, Deliver: DeliverWake, To: "seat:ci"},
		"not in the catalog":               {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverAction, Action: "drop_everything"},
		"needs an approval":                {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverAction, Action: "scale_deployment"},
		"wake to":                          {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "everyone"},
		"deliver is":                       {ID: "x", On: []string{"ci.run.finished"}, Deliver: "run"},
		"is task-result:":                  {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"detail": "log:land"}},
		"not one of":                       {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"detail": "task-result:land/commit", "require": "tripwire"}},
		"notify goes to":                   {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverNotify, To: "seat:ci", Params: map[string]string{"title": "t"}},
		"needs params.title":               {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverNotify, To: NotifyTo},
		"not one of params.detail's names": {ID: "x", On: []string{"seat.mail.arrived"}, Deliver: DeliverWake, To: "requester", Params: map[string]string{"detail": DetailOpenRegressions, "require": "tripwire"}},
		"names no run":                     {ID: "x", On: []string{"seat.mail.arrived"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"detail": "task-result:land/refusal"}},
		// 2026-11-18, OWNER_LAW #93: params.ask_verdict names a kind of problem; any other word is refused.
		"params.ask_verdict": {ID: "x", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci", Params: map[string]string{"ask_verdict": "maybe"}},
	}
	for want, r := range refused {
		err := CheckSubscriptions(tenants, row(r), tier)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v", want, err)
		}
	}
	// demo exports its type to rocket, not the other way round.
	foreign := map[string]Subscriptions{"demo": {Tenant: "demo", Rows: []Subscription{{ID: "y", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:x", Guarantee: "durable", By: ByDispatcher}}}}
	if err := CheckSubscriptions(tenants, foreign, tier); err == nil {
		t.Error("demo subscribed to rocket's ci.run.finished, which rocket does not export to it")
	}
}

// by: webhook is only for a wake on alert.rule.fired (TD-588, 2026-11-20):
// the alert webhook delivers those rows itself; nothing else would.
func TestWebhookRowsAreAlertWakesOnly(t *testing.T) {
	tenants, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	tier := func(string) (string, bool) { return "", false }
	one := func(r Subscription) map[string]Subscriptions {
		r.Guarantee, r.By = "durable", ByWebhook
		return map[string]Subscriptions{"rocket": {Tenant: "rocket", Rows: []Subscription{r}}}
	}
	// Positive control: an alert wake by the webhook is taken.
	if err := CheckSubscriptions(tenants, one(Subscription{ID: "w", On: []string{"alert.rule.fired"}, Deliver: DeliverWake, To: "seat:ci"}), tier); err != nil {
		t.Fatalf("an alert wake by webhook was refused: %v", err)
	}
	for name, r := range map[string]Subscription{
		"another topic": {ID: "w", On: []string{"ci.run.finished"}, Deliver: DeliverWake, To: "seat:ci"},
		"a notify":      {ID: "w", On: []string{"alert.rule.fired"}, Deliver: DeliverNotify, To: NotifyTo, Params: map[string]string{"title": "t"}},
	} {
		if err := CheckSubscriptions(tenants, one(r), tier); err == nil || !strings.Contains(err.Error(), "by webhook") {
			t.Errorf("%s by webhook: err = %v", name, err)
		}
	}
}
