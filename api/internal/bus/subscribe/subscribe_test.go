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
	send := func(_ context.Context, key, to, body string) error { got = append(got, sent{key, to, body}); return nil }

	if err := Deliver(context.Background(), row, ev, read, send); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].to != "thread:local_ab850773-fe26-40fa-ac01-b3b590b73164" || got[0].key != "sub:verdict-red-to-committer:ev-7" ||
		!strings.Contains(got[0].body, ev.Ref) {
		t.Fatalf("sent %+v", got)
	}

	green := ev
	green.Change = "green"
	if err := Deliver(context.Background(), row, green, read, send); err != nil || len(got) != 1 {
		t.Fatalf("a green verdict woke someone: %+v %v", got, err)
	}
	noTrailer := func(context.Context, string, string) (string, bool, error) { return "feat: x\n", true, nil }
	if err := Deliver(context.Background(), row, ev, noTrailer, send); err != nil || len(got) != 1 {
		t.Fatalf("a commit without a session woke someone: %+v %v", got, err)
	}
	broken := ev
	broken.Ref = "/api/v1/units/plan"
	if err := Deliver(context.Background(), row, broken, read, send); err == nil {
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
