// Package uicheck keeps bifrost-ui's stable pin (R0E, CI line 2026-11-25): the
// newest ui commit the console passed with, which CI and so the releases
// clone instead of ui's main. A new ui main head is checked by a full run of
// bifrost-ci-platform at platform's main head with that ui; green moves the
// pin, red leaves it and records the commit and the run as blocked. The
// console's tests then never go red because ui moved (Foundation's s7), and
// no consumer changes code: what moves is a ConfigMap the CI reads (the
// platform's state ConfigMap platform-state-ui-stable).
//
// The controller runs the loop (verification/assembly wireUICheck) and lends
// it the reads and writes below; a nil one runs nothing.
package uicheck

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Every is how often the loop looks at ui's main.
const Every = time.Minute

// Pin is what the stable pin holds: the ui commit, its package.json version,
// the run that passed it and when; Blocked* name the newest ui commit a check
// failed, its run and when (empty when the newest check passed).
type Pin struct {
	Commit, Version, Run, At             string
	BlockedCommit, BlockedRun, BlockedAt string
}

// ErrNoRun is Status's answer for a run the cluster no longer holds.
var ErrNoRun = errors.New("the check's run is gone")

// Check is the loop's state and what it reads and writes.
type Check struct {
	UIHead       func(ctx context.Context) (string, error)
	PlatformHead func(ctx context.Context) (string, error)
	Version      func(ctx context.Context, commit string) (string, error)
	Pin          func(ctx context.Context) (Pin, bool, error)
	SetPin       func(ctx context.Context, p Pin) error
	Ensure       func(ctx context.Context, platform, ui string) (string, error)
	// Status is Succeeded, Failed, or anything else while the run goes on.
	Status func(ctx context.Context, run string) (string, error)
	Now    func() time.Time
	Logf   func(format string, args ...any)

	mu      sync.Mutex
	ui, run string // the ui commit under check and its run
}

func (c *Check) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Check) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Pass is one look: it starts a check of a new ui head, or reads the running
// one and moves the pin or records the block. It returns what it did, "" for
// nothing.
func (c *Check) Pass(ctx context.Context) (string, error) {
	if c == nil || c.UIHead == nil || c.PlatformHead == nil || c.Pin == nil || c.SetPin == nil || c.Ensure == nil || c.Status == nil {
		return "", nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	head, err := c.UIHead(ctx)
	if err != nil || head == "" {
		return "", fmt.Errorf("ui main: %v", err)
	}
	pin, _, err := c.Pin(ctx)
	if err != nil {
		return "", fmt.Errorf("ui pin: %w", err)
	}
	if pin.Commit == head {
		c.ui, c.run = "", ""
		return "", nil
	}
	if pin.BlockedCommit == head && c.ui != head {
		// Checked and red; ui's main has not moved since. Nothing to do
		// until it does (a restarted controller does not run it again).
		return "", nil
	}
	if c.ui != head {
		platform, err := c.PlatformHead(ctx)
		if err != nil || platform == "" {
			return "", fmt.Errorf("platform main: %v", err)
		}
		run, err := c.Ensure(ctx, platform, head)
		if err != nil {
			return "", fmt.Errorf("ui check of %s: %w", short(head), err)
		}
		c.ui, c.run = head, run
		c.logf("agentrt-ui-check: %s checks ui %s with platform %s", run, short(head), short(platform))
		return "started " + run, nil
	}
	status, err := c.Status(ctx, c.run)
	if errors.Is(err, ErrNoRun) {
		c.ui, c.run = "", "" // started again on the next pass
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("ui check %s: %w", c.run, err)
	}
	switch status {
	case "Succeeded":
		version := ""
		if c.Version != nil {
			version, _ = c.Version(ctx, head) // the pin moves without one
		}
		next := Pin{Commit: head, Version: version, Run: c.run, At: c.now().UTC().Format(time.RFC3339)}
		if err := c.SetPin(ctx, next); err != nil {
			return "", fmt.Errorf("ui pin: %w", err)
		}
		c.logf("agentrt-ui-check: ui stable moves to %s (%s), checked by %s", short(head), version, c.run)
		c.ui, c.run = "", ""
		return "moved " + short(head), nil
	case "Failed":
		next := pin
		next.BlockedCommit, next.BlockedRun, next.BlockedAt = head, c.run, c.now().UTC().Format(time.RFC3339)
		if err := c.SetPin(ctx, next); err != nil {
			return "", fmt.Errorf("ui pin: %w", err)
		}
		c.logf("agentrt-ui-check: ui %s kept off: %s failed; stable stays at %s", short(head), c.run, short(pin.Commit))
		run := c.run
		c.ui, c.run = "", ""
		return "blocked " + run, nil
	}
	return "", nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
