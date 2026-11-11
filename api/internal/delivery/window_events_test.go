package delivery

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionWindowEvents(t *testing.T) {
	t.Parallel()
	var events []WindowEvent
	ctx := windowContext(context.Background(), func(_ context.Context, e WindowEvent) { events = append(events, e) })
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	input := windowInput{Who: "operator@host", What: "bifrost-platform,bifrost-ui", Env: "prod", Reason: "release"}
	first, err := putReleaseWindow(ctx, cs, "cicd", input, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = putReleaseWindow(ctx, cs, "cicd", input, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("renewal emitted an event: %+v", events)
	}
	if err = deleteReleaseWindow(ctx, cs, "cicd", input.Who, false, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "held" || events[1].Action != "released" {
		t.Fatalf("events: %+v", events)
	}
	for _, e := range events {
		if e.Who != input.Who || e.What != input.What || e.Env != input.Env || e.Reason != input.Reason || e.StartedAt != first.StartedAt {
			t.Fatalf("identity or start lost: %+v", e)
		}
	}
	if events[1].EndedAt != now.Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatal(events[1])
	}
	before := len(events)
	if _, _, err = getReleaseWindow(ctx, cs, "cicd", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(events) != before {
		t.Fatal("released hold was also expired", events)
	}
}

func TestWindowRefusalAndCrashExpiryEvents(t *testing.T) {
	t.Parallel()
	var events []WindowEvent
	ctx := windowContext(context.Background(), func(_ context.Context, e WindowEvent) { events = append(events, e) })
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	first, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "holder", What: "bifrost-platform", TTLMinutes: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "other", What: "bifrost-ui"}, now)
	if _, ok := err.(*WindowHeld); !ok {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Action != "refused" || events[1].Who != "other" || events[1].Holder != "holder" {
		t.Fatal(events)
	}
	if _, live, err := getReleaseWindow(ctx, cs, "cicd", now.Add(time.Hour)); err != nil || live {
		t.Fatal(live, err)
	}
	if len(events) != 3 || events[2].Action != "expired" || events[2].EndedAt != first.ExpiresAt || events[2].StartedAt != first.StartedAt {
		t.Fatal(events)
	}
}

func TestSTGWindowEvents(t *testing.T) {
	t.Parallel()
	a, _ := stgWindowCAS(t)
	var events []WindowEvent
	a.WindowRecorder = func(_ context.Context, e WindowEvent) { events = append(events, e) }
	ctx := context.Background()
	held, err := a.TakeSTGWindow(ctx, STGWindow{}, "stgrel-bifrost-platform-1-1", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	held, err = a.RenewSTGWindow(ctx, held)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatal(events)
	}
	if err = a.EndSTGWindow(ctx, held); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "held" || events[1].Action != "released" || events[0].Who != events[1].Who || events[0].StartedAt != events[1].StartedAt || events[1].Env != "stg" {
		t.Fatal(events)
	}
}
