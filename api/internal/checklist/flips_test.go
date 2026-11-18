package checklist

import (
	"context"
	"errors"
	"testing"
)

// Origin: 2026-11-18, the list of problem channels (L10): a probe that turned
// red only wrote /checklist/signals, so nobody saw it without opening the page.
func TestFlipsAreSaidOnceAndSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_DIR", dir)
	var said []string
	var fail error
	newHandler := func() *Handler {
		h := NewHandler(dir, nil)
		h.OnFlip(func(_ context.Context, w Watched) error {
			if fail != nil {
				return fail
			}
			said = append(said, w.Subject+"="+w.Signal)
			return nil
		})
		return h
	}
	run := func(h *Handler, sig string) []string {
		said = nil
		h.sayFlips(t.Context(), []Watched{{WatchEnv, "prod/redis", sig, ""}, {WatchPlugin, "market-data", SignalDegraded, ""}})
		return said
	}
	h := newHandler()
	if got := run(h, SignalOK); len(got) != 1 || got[0] != "market-data=degraded" {
		t.Fatalf("first run says only what is unhealthy: %v", got)
	}
	if got := run(h, SignalOK); len(got) != 0 {
		t.Fatalf("no change, nothing said: %v", got)
	}
	if got := run(h, SignalUnknown); len(got) != 0 {
		t.Fatalf("an unread probe is no flip: %v", got)
	}
	fail = errors.New("bus down")
	run(h, SignalFail)
	fail = nil
	h = newHandler() // a restart
	if got := run(h, SignalFail); len(got) != 1 || got[0] != "prod/redis=fail" {
		t.Fatalf("a flip that could not be said is said after the restart: %v", got)
	}
	if got := run(h, SignalFail); len(got) != 0 {
		t.Fatalf("said once: %v", got)
	}
	if got := run(h, SignalOK); len(got) != 1 || got[0] != "prod/redis=ok" {
		t.Fatalf("recovery is said: %v", got)
	}
}

func TestWatchedItemsLeavePluginItemsToThePlugins(t *testing.T) {
	p := &Prober{Env: "prod"}
	got := p.watchedItems([]ItemSignal{{ItemID: "redis", Signal: SignalOK}, {ItemID: "massive-polygon", Signal: SignalFail}})
	if len(got) != 1 || got[0].Subject != "prod/redis" {
		t.Fatalf("%+v", got)
	}
}
