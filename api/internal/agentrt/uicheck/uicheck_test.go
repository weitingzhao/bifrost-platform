package uicheck_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/uicheck"
)

// Origin: R0E (Foundation, 11-24; CI line 11-25). ui's main moved the console's
// CI with it, and a ui change that broke the console went red on the next
// platform commit (TD-588's two false regressions). The stable pin moves only
// when the console passes with the new ui; a red check leaves it.
type world struct {
	ui, platform string
	pin          uicheck.Pin
	has          bool
	status       map[string]string
	ensured      []string
	gone         map[string]bool
}

func (w *world) check() *uicheck.Check {
	return &uicheck.Check{
		UIHead:       func(context.Context) (string, error) { return w.ui, nil },
		PlatformHead: func(context.Context) (string, error) { return w.platform, nil },
		Version:      func(_ context.Context, c string) (string, error) { return "0.72." + c[:1], nil },
		Pin:          func(context.Context) (uicheck.Pin, bool, error) { return w.pin, w.has, nil },
		SetPin:       func(_ context.Context, p uicheck.Pin) error { w.pin, w.has = p, true; return nil },
		Ensure: func(_ context.Context, platform, ui string) (string, error) {
			name := "ui-check-" + ui[:12] + "-" + platform[:8]
			w.ensured = append(w.ensured, name)
			return name, nil
		},
		Status: func(_ context.Context, run string) (string, error) {
			if w.gone[run] {
				return "", uicheck.ErrNoRun
			}
			return w.status[run], nil
		},
		Now: func() time.Time { return time.Date(2026, 11, 25, 19, 0, 0, 0, time.UTC) },
	}
}

const (
	uiA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	uiB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plt = "1111111111111111111111111111111111111111"
)

func pass(t *testing.T, c *uicheck.Check) string {
	t.Helper()
	did, err := c.Pass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return did
}

func TestAGreenCheckMovesThePinAndARedOneLeavesIt(t *testing.T) {
	w := &world{ui: uiA, platform: plt, pin: uicheck.Pin{Commit: uiB}, has: true, status: map[string]string{}}
	c := w.check()
	if did := pass(t, c); !strings.HasPrefix(did, "started ") || len(w.ensured) != 1 {
		t.Fatalf("a new ui head starts one check: %q %v", did, w.ensured)
	}
	run := w.ensured[0]
	if did := pass(t, c); did != "" || w.pin.Commit != uiB {
		t.Fatalf("a running check moves nothing: %q %+v", did, w.pin)
	}
	w.status[run] = "Failed"
	if did := pass(t, c); did != "blocked "+run || w.pin.Commit != uiB || w.pin.BlockedCommit != uiA || w.pin.BlockedRun != run {
		t.Fatalf("a red check leaves the pin and records the block: %q %+v", did, w.pin)
	}
	// Nothing again until ui's main moves, also after a restart.
	if did := pass(t, c); did != "" || len(w.ensured) != 1 {
		t.Fatalf("a blocked head is not checked again: %q %v", did, w.ensured)
	}
	if did := pass(t, w.check()); did != "" || len(w.ensured) != 1 {
		t.Fatalf("a restarted loop does not check a blocked head again: %q %v", did, w.ensured)
	}
	// Positive control: ui moves on and the console passes with it.
	w.ui = uiB[:39] + "c"
	pass(t, c)
	run = w.ensured[len(w.ensured)-1]
	w.status[run] = "Succeeded"
	if did := pass(t, c); !strings.HasPrefix(did, "moved ") || w.pin.Commit != w.ui || w.pin.Run != run || w.pin.Version != "0.72.b" || w.pin.At == "" {
		t.Fatalf("a green check moves the pin: %q %+v", did, w.pin)
	}
	if did := pass(t, c); did != "" {
		t.Fatalf("a pinned head is left alone: %q", did)
	}
}

func TestTheFirstCheckRunsWithNoPinAndAGoneRunIsStartedAgain(t *testing.T) {
	w := &world{ui: uiA, platform: plt, status: map[string]string{}, gone: map[string]bool{}}
	c := w.check()
	pass(t, c)
	w.gone[w.ensured[0]] = true
	if did := pass(t, c); did != "" {
		t.Fatalf("a gone run: %q", did)
	}
	if pass(t, c); len(w.ensured) != 2 {
		t.Fatalf("a gone run is started again: %v", w.ensured)
	}
}

func TestAnUnwiredCheckDoesNothing(t *testing.T) {
	var c *uicheck.Check
	if did, err := c.Pass(context.Background()); did != "" || err != nil {
		t.Fatal(did, err)
	}
}

// Origin: R0E de-duplication (2026-11-25): the ui push's own platform run is
// taken as the check; a red one of those may be a flake, so the commit is
// checked again with a run of the loop's own before it is kept off the pin.
func TestARedPushRunIsCheckedAgainBeforeTheCommitIsBlocked(t *testing.T) {
	w := &world{ui: uiA, platform: plt, pin: uicheck.Pin{Commit: uiB}, has: true, status: map[string]string{}}
	c := w.check()
	pushed := true
	c.Ensure = func(_ context.Context, platform, ui string) (string, error) {
		if pushed {
			w.ensured = append(w.ensured, "ci-platform-push")
			return "ci-platform-push", nil
		}
		name := "ui-check-" + ui[:12] + "-" + platform[:8]
		w.ensured = append(w.ensured, name)
		return name, nil
	}
	c.Own = func(run string) bool { return strings.HasPrefix(run, "ui-check-") }
	pass(t, c)
	w.status["ci-platform-push"] = "Failed"
	pushed = false
	if did := pass(t, c); did != "" || w.pin.BlockedCommit != "" {
		t.Fatalf("a red push run blocks nothing: %q %+v", did, w.pin)
	}
	pass(t, c)
	own := w.ensured[len(w.ensured)-1]
	if !strings.HasPrefix(own, "ui-check-") {
		t.Fatalf("the commit is checked again with an own run: %v", w.ensured)
	}
	// Positive control: the own run's red does block.
	w.status[own] = "Failed"
	if did := pass(t, c); did != "blocked "+own || w.pin.BlockedCommit != uiA {
		t.Fatalf("an own red run blocks: %q %+v", did, w.pin)
	}
}
