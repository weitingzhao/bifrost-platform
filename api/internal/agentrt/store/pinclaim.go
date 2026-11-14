package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The exact claim of a pinned task (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.4, §2.6). The external host
// the signed standing envelope names for a pin leases that pin's tasks by
// id, one at a time, and nothing else; an ordinary claim never reads a
// pinned task (claim.go) and the external host never makes one. Only the
// source of the candidate differs from ClaimNext: every other judgement is
// claimIn's, the same code. The host is registered as the external host by
// its first exact claim, and only while a signed envelope in force names it
// (an agentd claim never registers a host an envelope names: kernel.go).

var (
	// ErrExternalHost: the external host called an ordinary claim, or an
	// ordinary registration would make it an ordinary host; nothing was
	// leased or registered.
	ErrExternalHost = errors.New("store: external_host")
	// ErrNotExternal: an exact claim by a host that is not the external
	// host the envelope names for the task's pin; nothing was leased.
	ErrNotExternal = errors.New("store: only the external host the standing envelope names claims a pinned task")
	// ErrNotPinned: an exact claim of a task with no pin; nothing was
	// leased.
	ErrNotPinned = errors.New("store: not_pinned")
	// ErrNoLease: an exact claim that leased nothing, with why (a control,
	// the slots, the budget, the route); nothing was recorded under its key,
	// so the same key tries again.
	ErrNoLease = errors.New("store: no_lease")
)

// PinnedClaimRequest is POST /agentrt/claim/pinned: the host, the task it
// names, the key a retry repeats, its free slots and disk, and the token it
// came with (its host's registration names it). It names no tools: the
// envelope's entry supplies the permitted executors and providers.
type PinnedClaimRequest struct {
	HostID, TaskID, Key, Token string
	FreeSlots                  int
	Disk                       *HostDisk
	DiskFloor                  int64
}

// entryOf is the entry of pin in the signed standing envelope in force for
// tenant, when it names host.
func entryOf(ctx context.Context, tx pgx.Tx, tenant, pin, host string) (rules.ExternalHost, error) {
	if pin == "" {
		return rules.ExternalHost{}, fmt.Errorf("%w: the task has no executor pin", ErrNotPinned)
	}
	env, err := inForce(ctx, tx, rules.KindEnvelope+":"+tenant)
	if err != nil {
		return rules.ExternalHost{}, err
	}
	if env == nil || env.SignedBy == "" {
		return rules.ExternalHost{}, fmt.Errorf("%w: no signed standing-envelope:%s is in force", ErrNotExternal, tenant)
	}
	h, ok := env.Envelope.Pinned(pin)
	if !ok || h.Host != host {
		return rules.ExternalHost{}, fmt.Errorf("%w: %s v%d names no external host %s for pin %s", ErrNotExternal, env.ID, env.Version, host, pin)
	}
	return h, nil
}

// externalNamed says whether a standing envelope in force, signed or not,
// for any tenant names host as an external host: such a host is never
// registered as an ordinary one (kernel.go).
func externalNamed(ctx context.Context, tx pgx.Tx, host string) (bool, error) {
	for _, t := range []string{"dev", "business"} {
		env, err := inForce(ctx, tx, rules.KindEnvelope+":"+t)
		if err != nil {
			return false, err
		}
		if env != nil && slices.ContainsFunc(env.Envelope.ExternalHosts, func(h rules.ExternalHost) bool { return h.Host == host }) {
			return true, nil
		}
	}
	return false, nil
}
