package runhooks

import (
	"context"
	"errors"
	"testing"
)

// 2026-11-16: the shell the CI line asked for (landingci FullRuns woken by
// ci.run.finished): a hook sees only its prefix, gets the run and commit,
// and is started whether it registers before or after the bus.
func TestHooksSeeTheirRunsAndStartEitherWay(t *testing.T) {
	var got []string
	h := Hook{Name: "landingci-full", Prefix: "ci-platform-", Handle: func(_ context.Context, run, sha string) error {
		got = append(got, run+"@"+sha)
		return nil
	}}
	_ = Deliver(context.Background(), h, "ci-platform-x9@abc")
	_ = Deliver(context.Background(), h, "bifrost-deliver-platform-1@abc")
	_ = Deliver(context.Background(), h, "broken")
	if len(got) != 1 || got[0] != "ci-platform-x9@abc" {
		t.Fatalf("got %v", got)
	}

	var started []string
	var r Registry
	if err := r.Register(h); err != nil {
		t.Fatal(err)
	}
	if errs := r.Start(func(h Hook) error { started = append(started, h.Consumer()); return nil }); len(errs) != 0 {
		t.Fatal(errs)
	}
	late := Hook{Name: "late", Prefix: "x", Handle: h.Handle}
	if err := r.Register(late); err != nil {
		t.Fatal(err)
	}
	if len(started) != 2 || started[0] != "run-landingci-full" || started[1] != "run-late" {
		t.Fatalf("started %v", started)
	}
	if err := r.Register(late); err == nil {
		t.Fatal("a hook registered twice was taken")
	}
	failing := Hook{Name: "f", Prefix: "x", Handle: func(context.Context, string, string) error { return errors.New("again") }}
	if err := Deliver(context.Background(), failing, "x1@s"); err == nil {
		t.Fatal("a handler's error was swallowed: the event must be asked again")
	}
}
