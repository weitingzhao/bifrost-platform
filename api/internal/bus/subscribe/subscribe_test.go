package subscribe

import (
	"context"
	"errors"
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
	type sent struct {
		key, to, body string
		cause         int64
	}
	var got []sent
	send := func(_ context.Context, key, to, body string, cause int64) error {
		got = append(got, sent{key, to, body, cause})
		return nil
	}

	if _, err := Deliver(context.Background(), row, ev, read, nil, send, nil, ""); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].to != "thread:local_ab850773-fe26-40fa-ac01-b3b590b73164" || got[0].key != "sub:verdict-red-to-committer:ev-7" ||
		!strings.Contains(got[0].body, ev.Ref) {
		t.Fatalf("sent %+v", got)
	}
	// The bus's own contract (2026-11-17, the coordinator: what a subscriber
	// sends because of an event names that event as its cause; none of a
	// day's 2,464 events did): the wake carries the event's ledger number.
	if got[0].cause != 7 {
		t.Fatalf("the wake for ev-7 names cause %d", got[0].cause)
	}

	green := ev
	green.Change = "green"
	if _, err := Deliver(context.Background(), row, green, read, nil, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("a green verdict woke someone: %+v %v", got, err)
	}
	noTrailer := func(context.Context, string, string) (string, bool, error) { return "feat: x\n", true, nil }
	if _, err := Deliver(context.Background(), row, ev, noTrailer, nil, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("a commit without a session woke someone: %+v %v", got, err)
	}
	broken := ev
	broken.Ref = "/api/v1/units/plan"
	if _, err := Deliver(context.Background(), row, broken, read, nil, send, nil, ""); err == nil {
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
	send := func(_ context.Context, _ string, t, b string, _ int64) error { to, body = t, b; return nil }
	unknown := func(context.Context, string) (bool, error) { return false, nil }
	out, err := Deliver(context.Background(), row, ev, nil, nil, send, unknown, "thread:local_duty")
	if err != nil || out != Rerouted || to != "thread:local_duty" || !strings.Contains(body, "thread:local_gone") {
		t.Fatalf("out=%s to=%q body=%q err=%v", out, to, body, err)
	}
	to = ""
	out, err = Deliver(context.Background(), row, ev, nil, nil, send, unknown, "")
	if err != nil || out != Undeliverable || to != "" {
		t.Fatalf("with nowhere to tell: out=%s to=%q err=%v", out, to, err)
	}
	knownOK := func(context.Context, string) (bool, error) { return true, nil }
	if out, _ := Deliver(context.Background(), row, ev, nil, nil, send, knownOK, "thread:local_duty"); out != Delivered || to != "thread:local_gone" {
		t.Fatalf("known recipient: out=%s to=%q", out, to)
	}
}

// 2026-11-17, the CI line: a refused landing was seen only in a Pod's log.
// The landing run's ci.run.finished (any change but succeeded) wakes the
// pusher; its ref names no repository, so the row's repos are searched.
func TestRefusedLandingWakesThePusher(t *testing.T) {
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	var row registry.Subscription
	for _, r := range subs["rocket"].Rows {
		if r.ID == "land-refused-to-pusher" {
			row = r
		}
	}
	sha := strings.Repeat("d", 40)
	ev := Event{ID: "ev-1", Type: "ci.run.finished", Subject: "land-2gmqc@" + sha, Ref: "/api/v1/delivery/runs/land-2gmqc", Change: "failed"}
	var asked []string
	read := func(_ context.Context, repo, commit string) (string, bool, error) {
		asked = append(asked, repo)
		if repo != "bifrost-trade-infra" || commit != sha {
			return "", false, nil
		}
		return "fix: x\n\nClaude-Session: local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff\n", true, nil
	}
	var to string
	send := func(_ context.Context, _ string, t, _ string, _ int64) error { to = t; return nil }
	out, err := Deliver(context.Background(), row, ev, read, nil, send, nil, "")
	if err != nil || out != Delivered || to != "thread:local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff" || len(asked) != 2 {
		t.Fatalf("out=%s to=%q asked=%v err=%v", out, to, asked, err)
	}
	ok := ev
	ok.Change = "succeeded"
	if out, _ := Deliver(context.Background(), row, ok, read, nil, send, nil, ""); out != Skipped {
		t.Fatalf("a landing that landed woke someone: %s", out)
	}
}

// 2026-11-17: a refused landing woke the pusher with only "land-… failed";
// why it was refused was in the pod's log (two pushes refused overnight, the
// line learned it an hour later). The land task now writes its refusal as a
// result (CI line, 08:53) and a row's params.detail adds it to the wake; a
// detail that cannot be read is said, and the wake still goes.
func TestRefusedLandingWakeCarriesTheReason(t *testing.T) {
	sha := strings.Repeat("a", 40)
	row := registry.Subscription{ID: "land-refused-to-pusher", On: []string{"ci.run.finished"}, Match: "land-", Change: "!succeeded",
		Deliver: registry.DeliverWake, To: "commit.session", Repos: []string{"bifrost-platform"},
		Params: map[string]string{"detail": "task-result:land/refusal"}, Guarantee: "durable", By: registry.ByDispatcher}
	ev := Event{ID: "ev-9", Type: "ci.run.finished", Subject: "land-k88n5@" + sha, Ref: "/api/v1/delivery/runs/land-k88n5", Change: "failed"}
	read := func(context.Context, string, string) (string, bool, error) {
		return "x\n\nClaude-Session: local_pusher\n", true, nil
	}
	var got []string
	send := func(_ context.Context, _, _, body string, _ int64) error { got = append(got, body); return nil }
	var asked string
	detail := func(_ context.Context, r registry.Subscription, e Event) (map[string]string, error) {
		asked = RunOf(e) + " " + r.Params["detail"]
		return map[string]string{"refusal": "refused: commit bc6b49b22fa9 has no Change-Id trailer"}, nil
	}
	if _, err := Deliver(context.Background(), row, ev, read, detail, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("delivered %d, %v", len(got), err)
	}
	if asked != "land-k88n5 task-result:land/refusal" {
		t.Fatalf("detail asked for %q", asked)
	}
	if !strings.Contains(got[0], "Why: refused: commit bc6b49b22fa9 has no Change-Id trailer") {
		t.Fatalf("the reason is not in the wake: %s", got[0])
	}
	broken := func(context.Context, registry.Subscription, Event) (map[string]string, error) {
		return nil, errors.New("cluster down")
	}
	got = nil
	ev.ID = "ev-10"
	if _, err := Deliver(context.Background(), row, ev, read, broken, send, nil, ""); err != nil || len(got) != 1 {
		t.Fatalf("an unreadable detail held the wake back: %d, %v", len(got), err)
	}
	if !strings.Contains(got[0], "could not be read: cluster down") {
		t.Fatalf("an unreadable detail is not said: %s", got[0])
	}
	// Positive control: a row without params.detail never asks.
	row.Params = nil
	got, asked = nil, ""
	ev.ID = "ev-11"
	if _, err := Deliver(context.Background(), row, ev, read, detail, send, nil, ""); err != nil || asked != "" || strings.Contains(got[0], "Why:") {
		t.Fatalf("a row without detail asked %q: %v %v", asked, got, err)
	}
}

// 2026-11-17 (the coordinator's tripwire, CI line): a landing that touched
// the paths deciding whether the release loop may ship to PROD writes them
// as the land task's result tripwire; the CI line and the coordinator are
// woken with the paths, the commit, the change ids and the branch. A landing
// that touched none wakes nobody (params.require); one whose detail cannot be
// read still wakes, saying so.
func TestTripwireWakesOnlyWhenTouched(t *testing.T) {
	sha := strings.Repeat("b", 40)
	row := registry.Subscription{ID: "land-tripwire-to-ci", On: []string{"ci.run.finished"}, Match: "land-",
		Deliver: registry.DeliverWake, To: "thread:local_ci",
		Params:    map[string]string{"detail": "task-result:land/tripwire,commit,change-ids,branch", "require": "tripwire"},
		Guarantee: "durable", By: registry.ByDispatcher}
	ev := Event{ID: "ev-20", Type: "ci.run.finished", Subject: "land-x1@" + sha, Ref: "/r", Change: "succeeded"}
	var got []string
	send := func(_ context.Context, _, _, body string, _ int64) error { got = append(got, body); return nil }
	touched := func(context.Context, registry.Subscription, Event) (map[string]string, error) {
		return map[string]string{"tripwire": "api/internal/headrelease/wakes.go", "commit": sha, "change-ids": "Iabc", "branch": "refs/heads/ready/x"}, nil
	}
	if out, err := Deliver(context.Background(), row, ev, nil, touched, send, nil, ""); err != nil || out != Delivered || len(got) != 1 {
		t.Fatalf("touched: %v %v %d", out, err, len(got))
	}
	if !strings.Contains(got[0], "Detail: tripwire=api/internal/headrelease/wakes.go; commit="+sha+"; change-ids=Iabc; branch=refs/heads/ready/x") {
		t.Fatalf("the wake does not carry the detail: %s", got[0])
	}
	untouched := func(context.Context, registry.Subscription, Event) (map[string]string, error) {
		return map[string]string{"commit": sha, "branch": "refs/heads/ready/y"}, nil
	}
	got = nil
	ev.ID = "ev-21"
	if out, err := Deliver(context.Background(), row, ev, nil, untouched, send, nil, ""); err != nil || out != Skipped || len(got) != 0 {
		t.Fatalf("untouched woke: %v %v %v", out, err, got)
	}
	broken := func(context.Context, registry.Subscription, Event) (map[string]string, error) {
		return nil, errors.New("cluster down")
	}
	ev.ID = "ev-22"
	if out, err := Deliver(context.Background(), row, ev, nil, broken, send, nil, ""); err != nil || out != Delivered || !strings.Contains(got[0], "could not be read") {
		t.Fatalf("an unreadable tripwire did not wake: %v %v %v", out, err, got)
	}
}

// 2026-11-17, the coordinator: approval.card.created was heard by no one on
// the bus; the approvals service pushed to the Owner's phone directly. A
// notify row pushes once per event, its title and link filled from the
// event, and a relay failure asks for the event again.
func TestNotifyFillsThePushFromTheEvent(t *testing.T) {
	row := registry.Subscription{ID: "card-created-to-owner", On: []string{"approval.card.created"}, Deliver: registry.DeliverNotify, To: registry.NotifyTo,
		Params: map[string]string{"title": "Bus: card {subject} waits", "click": "http://console/#cards?id={ref_id}"}, Guarantee: "durable", By: registry.ByDispatcher}
	ev := Event{ID: "ev-9", Type: "approval.card.created", Subject: "#612", Ref: "/api/v1/approvals/appr_abc"}
	var got []string
	push := func(_ context.Context, key, title, body, click string) error {
		got = append(got, key, title, click, body)
		return nil
	}
	out, err := Notify(context.Background(), row, ev, push)
	if err != nil || out != Delivered || len(got) != 4 {
		t.Fatalf("out=%s err=%v got=%v", out, err, got)
	}
	if got[0] != "sub:card-created-to-owner:ev-9" || got[1] != "Bus: card #612 waits" || got[2] != "http://console/#cards?id=appr_abc" || !strings.Contains(got[3], "ev-9") {
		t.Fatalf("push %q", got)
	}
	// Not its event, or a wake row: nothing pushed.
	got = nil
	if out, _ := Notify(context.Background(), row, Event{ID: "ev-10", Type: "approval.card.decided"}, push); out != Skipped || got != nil {
		t.Fatalf("another type pushed: %s %v", out, got)
	}
	wake := row
	wake.Deliver = registry.DeliverWake
	if out, _ := Notify(context.Background(), wake, ev, push); out != Skipped || got != nil {
		t.Fatalf("a wake row pushed: %s %v", out, got)
	}
	// The relay failing is an error: the consumer asks for the event again.
	if _, err := Notify(context.Background(), row, ev, func(context.Context, string, string, string, string) error { return errors.New("relay down") }); err == nil {
		t.Fatal("a failed push acknowledged the event")
	}
}
