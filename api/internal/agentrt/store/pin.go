package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Executor pins (W-31 round 4, core blocks C-0 and C-1; bifrost-trade-infra
// agent-config/work/multi-agent/DESIGN-coordinator-landing-v3-2026-10-29.md
// §2.1–§2.3, §7.1). A task pinned to the external host is leased only there
// (claim.go reads no pinned task for an ordinary host; the database holds
// both ways, 0010). Who may set a pin is decided here, in the write's
// transaction, after the writer is checked (ensureWriter): only a proposal
// over the tasks route, by a seat writing with its own token, never relayed,
// when a signed standing envelope in force for the mission's tenant has an
// external_hosts entry for the pin, of an implementation with a branch
// effect, no subject, in a repository and role the entry lists. The entry's
// proposer is read and kept but no longer has to be the writer (cleanup wave
// 1, 2026-11-11). The X-Bifrost-Session header the proposal came with is
// recorded as via_session and grants nothing (§2.3). Plans, imports, the controller's
// checks and gates, goals, briefs and relayed writes build TaskProposal and
// Task, which have no pin to set: only PinnedTask has.

// ExecutorPins are the pins there are (0010 task_executor_pin_check).
var ExecutorPins = []string{rules.PinCoordinator}

// ErrPinNotPermitted: a pinned task the writer may not propose; nothing of
// the batch was written. It is an ErrInvalid.
var ErrPinNotPermitted = fmt.Errorf("%w: pin_not_permitted", ErrInvalid)

func pinRefused(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrPinNotPermitted, fmt.Sprintf(format, args...))
}

// PinnedTask is a task of a POST /missions/{id}/tasks batch with its
// executor pin ("" for a task of the batch that has none).
type PinnedTask struct {
	TaskProposal
	ExecutorPin string
	// Providers are the model providers whose work the commit holds, as
	// the proposer declares them (§2.8, F4 A): every provider must be
	// permitted by the host entry. Its spec is the platform's
	// (ImplementSpec), whatever SpecArtifact says.
	Providers []string
	// Executor is optional for legacy callers; when given, the host must permit it.
	Executor string
	// PinReason is why an implement task is pinned to the coordinator
	// (pinreason.go), recorded in its task_proposed body; "" for none.
	PinReason string
}

// ErrMixedSource: a pinned task declares no provider, or one besides its
// entry's (§2.8, F4 A: the first version takes only the coordinator's own
// work); nothing of the batch was written. It is an ErrInvalid.
var ErrMixedSource = fmt.Errorf("%w: mixed_source_not_supported", ErrInvalid)

// pinning is what a pinned batch adds to its tasks: each one's pin, by ref,
// and the session header it came with.
type pinning struct {
	executors  map[string]string
	pins       map[string]string
	providers  map[string][]string
	reasons    map[string]string
	viaSession string
}

// pinWriter is what a writer must be to set a pin, whatever the rules say:
// its own request (not relayed), a seat, writing with the seat's own token
// (the token's name is the actor, from the platform's token table, never
// from a header). A pinned answer is given again only to such a writer.
func pinWriter(w Writer) error {
	switch {
	case w.Relayed:
		return pinRefused("a write relayed for %s sets no pin", w.Actor)
	case kindOf(w.Actor) != "seat":
		return pinRefused("%s is not a seat: only a seat sets a pin", w.Actor)
	case w.Token != w.Actor:
		return pinRefused("%s writes with the token %q, not its own", w.Actor, w.Token)
	}
	return nil
}

// checkPin checks one pinned task of a batch in the write's transaction,
// the writer already ensured: the writer may pin, and the envelope in force
// for the mission's tenant, signed, has the pin's entry, which lists the
// task's repository and role.
func checkPin(ctx context.Context, tx pgx.Tx, w Writer, missionID string, t TaskProposal, pin string, providers []string, executor string) (err error) {
	defer func() {
		if err != nil {
			err = &handinRefusal{task: t.Ref, err: err}
		}
	}()
	if err := pinWriter(w); err != nil {
		return err
	}
	switch {
	case t.Role != "implement" || t.Effect != "branch":
		return pinRefused("task %s: a pinned task implements on a branch, not %s with effect %s", t.Ref, t.Role, t.Effect)
	case t.Subject != "":
		return pinRefused("task %s: a pinned task checks no other task", t.Ref)
	}
	var tenant string
	if err := tx.QueryRow(ctx, `SELECT tenant FROM agentrt.mission WHERE mission_id = $1`, missionID).Scan(&tenant); err != nil {
		return err
	}
	env, err := inForce(ctx, tx, rules.KindEnvelope+":"+tenant)
	if err != nil {
		return err
	}
	if env == nil || env.SignedBy == "" {
		// Only a version the Owner signed holds an external host; an
		// unsigned tightening since stops the pins until he signs again.
		return pinRefused("task %s: no signed standing-envelope:%s is in force", t.Ref, tenant)
	}
	h, ok := env.Envelope.Pinned(pin)
	switch {
	case !ok:
		return pinRefused("task %s: %s v%d names no external host for pin %s", t.Ref, env.ID, env.Version, pin)
	case !slices.Contains(h.Roles, t.Role):
		return pinRefused("task %s: %s v%d lets pin %s have roles %v, not %s", t.Ref, env.ID, env.Version, pin, h.Roles, t.Role)
	case !slices.Contains(h.Repos, t.Repo):
		return pinRefused("task %s: %s v%d lets pin %s have repositories %v, not %q", t.Ref, env.ID, env.Version, pin, h.Repos, t.Repo)
	case len(Repos[t.Repo].Verify) == 0:
		return pinRefused("task %s: repository %q has no checks the platform knows, so no spec of its own", t.Ref, t.Repo)
	case len(providers) == 0:
		if len(h.Executors) == 0 {
			return fmt.Errorf("%w: task %s declares no providers; a pinned task declares [%s]", ErrMixedSource, t.Ref, h.Provider)
		}
		return fmt.Errorf("%w: task %s declares no providers; a pinned task declares a permitted provider", ErrMixedSource, t.Ref)
	}
	if executor != "" && !slices.ContainsFunc(h.Tools(), func(x rules.ExternalExecutor) bool { return x.Executor == executor }) {
		return pinRefused("task %s: executor %s is not permitted", t.Ref, executor)
	}
	for _, pv := range providers {
		if !slices.ContainsFunc(h.Tools(), func(x rules.ExternalExecutor) bool {
			return x.Provider == pv
		}) {
			if len(h.Executors) == 0 {
				return fmt.Errorf("%w: task %s declares provider %q; a pinned task holds %s's work alone, or goes by hand", ErrMixedSource, t.Ref, pv, h.Provider)
			}
			return fmt.Errorf("%w: task %s declares provider %q; a pinned task holds only work from its permitted providers", ErrMixedSource, t.Ref, pv)
		}
	}
	return nil
}

// pinnedSpec is the spec of a pinned task (§2.8): the platform's, made by
// ImplementSpec with the repository's own checks, never one the proposer
// uploaded.
func pinnedSpec(ctx context.Context, tx pgx.Tx, t TaskProposal) (string, error) {
	repo := Repos[t.Repo]
	spec, err := ImplementSpec(t.Title, "Handed in by the coordinator: one commit on the task's base, made before it was handed in. "+
		"The repository's checks run on that commit.", repo, slices.Clone(repo.Verify), HandInChecks(repo, nil))
	if err != nil {
		return "", err
	}
	return PutArtifact(ctx, tx, "controller", "task_spec", "application/json", spec)
}

// badViaSession says why a via_session is refused, "" when it is a
// session id or empty.
func badViaSession(v any) string {
	s, ok := v.(string)
	switch {
	case !ok:
		return "is not a string"
	case s != "" && !threadSession.MatchString(s):
		return fmt.Sprintf("is %q, not a session id", s)
	}
	return ""
}
