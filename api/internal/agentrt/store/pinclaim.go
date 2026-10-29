package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
// envelope's entry says which executor and provider run a pinned task.
type PinnedClaimRequest struct {
	HostID, TaskID, Key, Token string
	FreeSlots                  int
	Disk                       *HostDisk
	DiskFloor                  int64
}

// ClaimPinned leases the pinned task r names to the external host, or says
// why not. The key is the host's (host:<id>|claim_pinned|<key>), the
// request's digest its task: the same key and task get the first answer
// again, whatever has happened to the attempt since (the host reads it back);
// the same key with another task is ErrConflict; two calls with one key at
// once lease once. A refusal stores nothing.
func ClaimPinned(ctx context.Context, db DB, r PinnedClaimRequest) (map[string]any, error) {
	return claimPinned(ctx, db, r, LeaseTTL)
}

func claimPinned(ctx context.Context, db DB, r PinnedClaimRequest, ttl time.Duration) (map[string]any, error) {
	switch {
	case r.Key == "":
		return nil, invalid("an idempotency key is required")
	case !taskIDRE.MatchString(r.TaskID):
		return nil, invalid("%q is not a task id", r.TaskID)
	case r.HostID == "":
		return nil, invalid("a host is required")
	}
	req := map[string]any{"task_id": r.TaskID}
	pre := func(ctx context.Context) error {
		return registerPinnedHost(ctx, db, r)
	}
	op := func(tx pgx.Tx) (map[string]any, error) {
		var pin string
		authorize := func(ctx context.Context, tx pgx.Tx, cr ClaimRequest) (ClaimRequest, error) {
			h, p, err := pinnedEntry(ctx, tx, r.HostID, r.TaskID)
			if err != nil {
				return cr, err
			}
			pin = p
			cr.Tools = []Tool{{Executor: h.Executor, Provider: h.Provider}}
			return cr, nil
		}
		cr := ClaimRequest{HostID: r.HostID, FreeSlots: r.FreeSlots, Disk: r.Disk, DiskFloor: r.DiskFloor}
		src := func(ctx context.Context, tx pgx.Tx, after *candidate) ([]candidate, error) {
			return pinnedSource(r.TaskID, pin)(ctx, tx, after)
		}
		cl, why, err := claimIn(ctx, tx, cr, src, claimOpts{ttl: ttl, effects: dispatched, authorize: authorize}, true)
		if err != nil {
			return nil, err
		}
		if cl == nil {
			return nil, fmt.Errorf("%w: %s", ErrNoLease, why)
		}
		return cl.pinnedDoc(), nil
	}
	out, err := idempotent(ctx, db, hostActor(r.HostID), "claim_pinned", r.Key, req, pre, op)
	if err == nil || refusedClaim(err) {
		return out, err
	}
	// A commit that failed may have landed (TD-399): the key says.
	if back, found := readBackKey(ctx, db, hostActor(r.HostID), "claim_pinned", r.Key, req); found {
		return back, nil
	}
	return nil, err
}

// refusedClaim is a refusal that stores nothing and needs no read-back.
func refusedClaim(err error) bool {
	for _, e := range []error{ErrNoLease, ErrNotPinned, ErrNotExternal, ErrExternalHost, ErrState, ErrInvalid, ErrConflict, ErrFenced,
		pgx.ErrNoRows} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// readBackKey reads the answer a key stored, under a context of its own,
// once the transaction that may have stored it has ended (it takes the key's
// lock): found is false when there is none, or it cannot be read.
func readBackKey(ctx context.Context, db DB, actor, route, key string, request any) (map[string]any, bool) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, false
	}
	id := actor + "|" + route + "|" + key
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ConfirmLimit)
	defer cancel()
	var out map[string]any
	var found bool
	_ = InTx(rctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(rctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, id); err != nil {
			return err
		}
		var storedSum, stored string
		if err := tx.QueryRow(rctx, `SELECT request_sha256, response_json::text FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
			id).Scan(&storedSum, &stored); err != nil || storedSum != sum {
			return err
		}
		m, err := response([]byte(stored))
		out, found = m, err == nil
		return err
	})
	return out, found
}

// registerPinnedHost serializes a first registration with envelope writers.
// Existing hosts are reauthorized only after claimIn takes host -> capability,
// so this preflight cannot hold capability while waiting for an existing host.
func registerPinnedHost(ctx context.Context, db DB, r PinnedClaimRequest) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742112, hashtext($1))`, r.HostID); err != nil {
			return err
		}
		var kind string
		err := tx.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id = $1`, r.HostID).Scan(&kind)
		if err == nil {
			if kind != HostExternal {
				return fmt.Errorf("%w: host %s is a %s host", ErrNotExternal, r.HostID, kind)
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		if _, _, err = pinnedEntry(ctx, tx, r.HostID, r.TaskID); err != nil {
			return err
		}
		return registerHostIn(ctx, tx, "controller", r.HostID, HostExternal, r.Token, false)
	})
}

// pinnedEntry is read only while holding capabilityLock, up to commit.
func pinnedEntry(ctx context.Context, tx pgx.Tx, host, task string) (rules.ExternalHost, string, error) {
	var pin, tenant, repo, role, state string
	err := tx.QueryRow(ctx, `SELECT COALESCE(t.executor_pin, ''), m.tenant, COALESCE(t.repo, ''), t.role, t.state
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id WHERE t.task_id = $1`, task).
		Scan(&pin, &tenant, &repo, &role, &state)
	if err != nil {
		return rules.ExternalHost{}, pin, err
	}
	h, err := entryOf(ctx, tx, tenant, pin, host)
	if err != nil {
		return h, pin, err
	}
	if !slices.Contains(h.Repos, repo) || !slices.Contains(h.Roles, role) {
		return h, pin, fmt.Errorf("%w: the envelope's entry for %s no longer permits %s/%s", ErrNotExternal, pin, repo, role)
	}
	if state != "ready" {
		return h, pin, fmt.Errorf("%w: task %s is %s, not ready", ErrState, task, state)
	}
	return h, pin, nil
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

// pinnedSource is the one task an exact claim names, as a candidate, while
// it is ready, of an active mission, of an effect dispatched, and pinned
// with pin; else none.
func pinnedSource(taskID, pin string) candidateSource {
	return func(ctx context.Context, tx pgx.Tx, after *candidate) ([]candidate, error) {
		if after != nil {
			return nil, nil
		}
		rows, err := tx.Query(ctx, candidateColumns+`
		  WHERE t.task_id = $1 AND t.state = 'ready' AND m.state = 'active' AND t.effect_class = ANY($2) AND t.executor_pin = $3`,
			taskID, dispatched, pin)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanCandidate)
	}
}

// pinnedDoc is the answer of an exact claim, as its key keeps it.
func (cl *Claim) pinnedDoc() map[string]any {
	doc := map[string]any{"task_id": cl.TaskID, "attempt_id": cl.AttemptID, "attempt_no": cl.AttemptNo, "mission_id": cl.MissionID,
		"role": cl.Role, "repo": cl.Repo, "spec_artifact_id": cl.SpecArtifact, "base_commit": cl.BaseCommit, "work_id": cl.WorkID,
		"pushed_ref": cl.PushedRef, "effect_class": cl.Effect, "lease_epoch": cl.Epoch,
		"lease_expires_at": cl.LeaseExpiresAt.UTC().Format(time.RFC3339Nano), "hard_limit_s": int(cl.HardLimit / time.Second),
		"tool": map[string]any{"executor": cl.Tool.Executor, "provider": cl.Tool.Provider, "model": cl.Tool.Model}}
	if len(cl.Failed) > 0 {
		failed := make([]any, 0, len(cl.Failed))
		for _, f := range cl.Failed {
			failed = append(failed, map[string]any{"method": f.Method, "attempt_no": f.AttemptNo, "artifact_id": f.Evidence})
		}
		doc["failed_checks"] = failed
	}
	return doc
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

// RefuseExternalHost refuses an ordinary agentd call of the external host
// before it writes anything (ErrExternalHost): it claims only by id. A host
// not registered yet is not refused here (RegisterAgentdHost refuses one an
// envelope names).
func RefuseExternalHost(ctx context.Context, db DB, host string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		var kind string
		err := tx.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id = $1`, host).Scan(&kind)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err
		case kind == HostExternal:
			return fmt.Errorf("%w: host %s is the external host; it claims only its pinned tasks, by id", ErrExternalHost, host)
		}
		return nil
	})
}
