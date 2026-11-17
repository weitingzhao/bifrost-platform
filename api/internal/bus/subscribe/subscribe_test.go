package subscribe

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// 2026-11-16: the first rows of the subscription table. A red verdict wakes
// the seat that made the commit (its Claude-Session trailer); a green one,
// or a commit without the trailer, wakes nobody; one event is one message
// however often it is delivered.
func TestVerdictRedWakesTheCommitter(t *testing.T) {
	sha := strings.Repeat("a", 40)
	row := registry.Subscription{ID: "verdict-red-to-committer", On: []string{"ci.verdict.changed"}, Change: "red",
		Deliver: registry.DeliverWake, To: "commit.session", Guarantee: "durable", By: registry.ByDispatcher}
	ev := Event{ID: "ev-7", Type: "ci.verdict.changed", Subject: strings.Repeat("c", 40) + "@" + sha + ":ci/full",
		Ref: "/api/v1/units/plan?repo=bifrost-platform&commit=" + sha, Change: "red"}
	read := func(_ context.Context, repo, commit string) (string, bool, error) {
		if repo != "bifrost-platform" || commit != sha {
			t.Fatalf("read %s@%s", repo, commit)
		}
		return "feat: x\n\nClaude-Session: local_ab850773-fe26-40fa-ac01-b3b590b73164\nWork: LANE-x\n", true, nil
	}
	type sent struct{ key, to, body string }
	var got []sent
	send := func(_ context.Context, key, to, body string) error {
		got = append(got, sent{key, to, body})
		return nil
	}

	if _, err := Deliver(context.Background(), row, ev, read, send, nil, ""); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].to != "thread:local_ab850773-fe26-40fa-ac01-b3b590b73164" || got[0].key != "sub:verdict-red-to-committer:ev-7" ||
		!strings.Contains(got[0].body, ev.Ref) {
		t.Fatalf("sent %+v", got)
	}

	green := ev
	green.Change = "green"
	if _, err := Deliver(context.Background(), row, green, read, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("a green verdict woke someone: %+v %v", got, err)
	}
	noTrailer := func(context.Context, string, string) (string, bool, error) { return "feat: x\n", true, nil }
	if _, err := Deliver(context.Background(), row, ev, noTrailer, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("a commit without a session woke someone: %+v %v", got, err)
	}
	broken := ev
	broken.Ref = "/api/v1/units/plan"
	if _, err := Deliver(context.Background(), row, broken, read, send, nil, ""); err == nil {
		t.Fatal("an event naming no repo was taken as delivered")
	}
}

func TestRecipientForms(t *testing.T) {
	ctx := context.Background()
	ev := Event{Subject: "thread:local_x", Type: "seat.mail.arrived"}
	for to, want := range map[string]string{"subject": "thread:local_x", "seat:ci": "seat:ci"} {
		got, err := Recipient(ctx, registry.Subscription{ID: "r", To: to}, ev, nil)
		if err != nil || got != want {
			t.Errorf("to %s: %q %v", to, got, err)
		}
	}
	if _, err := Recipient(ctx, registry.Subscription{ID: "r", To: "ref.requester"}, ev, nil); err == nil {
		t.Error("ref.requester is delivered by the approvals service today, not resolved here")
	}
}

// Only the rows the dispatcher delivers get a consumer; the others are
// delivered by the code their by names until they are switched.
func TestRowsAreTheDispatchers(t *testing.T) {
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	rows := Rows(subs, "rocket")
	if len(rows) == 0 {
		t.Fatal("no row for the dispatcher")
	}
	for _, r := range rows {
		if r.By != registry.ByDispatcher {
			t.Errorf("%s is delivered by %s", r.ID, r.By)
		}
	}
}

// 2026-11-16 (the Owner: users come and go by configuration): a tenant's
// rows are dispatched because its file is there, a row limited to STG is not
// dispatched in PROD, and when a row or a tenant leaves, its consumer is
// named stale so the dispatcher removes it.
func TestTenantsComeAndGoByTheirFiles(t *testing.T) {
	row := func(id string, envs ...string) registry.Subscription {
		return registry.Subscription{ID: id, On: []string{"git.main.advanced"}, Deliver: registry.DeliverWake, To: "seat:x",
			Guarantee: "durable", By: registry.ByDispatcher, Envs: envs}
	}
	subs := map[string]registry.Subscriptions{
		"rocket": {Tenant: "rocket", Rows: []registry.Subscription{row("verdict-red-to-committer"), {ID: "legacy", By: "approvals.thread-inbox"}}},
		"demo":   {Tenant: "demo", Rows: []registry.Subscription{row("drill", "stg")}},
	}
	names := func(cs []Consumer) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name("rocket"))
		}
		return out
	}
	if got := strings.Join(names(Consumers(subs, "stg")), ","); got != "sub-demo-drill,sub-verdict-red-to-committer" {
		t.Fatalf("stg consumers %s", got)
	}
	if got := strings.Join(names(Consumers(subs, "prod")), ","); got != "sub-verdict-red-to-committer" {
		t.Fatalf("prod consumers %s (the drill is STG only)", got)
	}
	existing := []string{"headrelease", "sub-verdict-red-to-committer", "sub-demo-drill"}
	delete(subs, "demo")
	stale := Stale(existing, Consumers(subs, "stg"), "rocket")
	if strings.Join(stale, ",") != "sub-demo-drill" {
		t.Fatalf("stale %v: the gone tenant's consumer goes, headrelease is not the dispatcher's", stale)
	}
}

// 2026-11-16, the tenant drill on STG: a wake whose recipient was not a
// registered thread there was dropped without a word. It now goes to the
// tenant's undeliverable_to, naming the address; with none it is reported.
func TestUnknownRecipientIsNotDroppedSilently(t *testing.T) {
	row := registry.Subscription{ID: "drill", On: []string{"git.main.advanced"}, Deliver: registry.DeliverWake,
		To: "thread:local_gone", Guarantee: "durable", By: registry.ByDispatcher}
	ev := Event{ID: "ev-9", Type: "git.main.advanced", Subject: "bifrost-platform@" + strings.Repeat("b", 40)}
	var to, body string
	send := func(_ context.Context, _ string, t, b string) error { to, body = t, b; return nil }
	unknown := func(context.Context, string) (bool, error) { return false, nil }
	out, err := Deliver(context.Background(), row, ev, nil, send, unknown, "thread:local_duty")
	if err != nil || out != Rerouted || to != "thread:local_duty" || !strings.Contains(body, "thread:local_gone") {
		t.Fatalf("out=%s to=%q body=%q err=%v", out, to, body, err)
	}
	to = ""
	out, err = Deliver(context.Background(), row, ev, nil, send, unknown, "")
	if err != nil || out != Undeliverable || to != "" {
		t.Fatalf("with nowhere to tell: out=%s to=%q err=%v", out, to, err)
	}
	knownOK := func(context.Context, string) (bool, error) { return true, nil }
	if out, _ := Deliver(context.Background(), row, ev, nil, send, knownOK, "thread:local_duty"); out != Delivered || to != "thread:local_gone" {
		t.Fatalf("known recipient: out=%s to=%q", out, to)
	}
}
