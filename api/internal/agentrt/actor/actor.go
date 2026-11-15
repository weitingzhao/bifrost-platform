// Package actor is the base that says who a write is made as: the writer of
// a request (an actor and the token behind it), the hold of a thread's session
// to the token that first wrote as it (TD-370), a thread's registration on its
// first write, and the idempotency keys a writer's requests are answered by.
// Every domain's write routes call it before they write. It imports the event
// log and no domain: what only a domain knows (an external agent's binding,
// the day's limit of new threads) the domain registers here.
package actor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// ErrConflict: an idempotency key reused with a different request.
var ErrConflict = errors.New("store: idempotency key reused with a different request")

// TestHook, set only by tests, runs at named points so a test can hold a
// call there and observe or race it. It is loaded atomically: a test may
// remove it while a call it staged on another goroutine still passes a hook
// point.
var TestHook atomic.Pointer[func(point, arg string)]

// Hook runs the test hook at a named point.
func Hook(point, arg string) {
	if f := TestHook.Load(); f != nil {
		(*f)(point, arg)
	}
}

// agentHolder is the hold of an external agent's token to its actor, which
// the intake domain knows and registers (RegisterAgentHolder).
var agentHolder atomic.Pointer[func(context.Context, pgx.Tx, Writer) error]

// RegisterAgentHolder installs the check of an external agent's writer. It
// is called once, from the init of the package that owns intake clients.
func RegisterAgentHolder(f func(context.Context, pgx.Tx, Writer) error) { agentHolder.Store(&f) }

// holds is the hold CheckWriter checks a registered thread or agent by: a
// thread's session is its first token's (HoldsToken); an external agent's
// token is the one bound to it and not revoked, which the registered holder
// checks. With no holder registered an agent holds nothing: it is refused,
// not let through.
func holds(ctx context.Context, tx pgx.Tx, w Writer) error {
	if KindOf(w.Actor) != "agent" {
		return HoldsToken(ctx, tx, w)
	}
	f := agentHolder.Load()
	if f == nil {
		return fmt.Errorf("%w: no check of external agents is registered, so %s may not act", ErrNotYours, w.Actor)
	}
	return (*f)(ctx, tx, w)
}

// threadLimit reads the day's limit of new threads where a rule may set it
// (RegisterThreadLimit); ThreadsPerDay when nothing is registered.
var threadLimit atomic.Pointer[func(context.Context, pgx.Tx) (int, error)]

// RegisterThreadLimit installs the reader of the day's limit of new threads.
func RegisterThreadLimit(f func(context.Context, pgx.Tx) (int, error)) { threadLimit.Store(&f) }

func threadLimitIn(ctx context.Context, tx pgx.Tx) (int, error) {
	if f := threadLimit.Load(); f != nil {
		return (*f)(ctx, tx)
	}
	return ThreadsPerDay, nil
}

// threadSession is what a thread names itself by: the X-Bifrost-Session
// header the questions routes already take.
var threadSession = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// IsSession says whether s is a session id.
func IsSession(s string) bool { return threadSession.MatchString(s) }

// ThreadActor is the actor of the thread with that session id.
func ThreadActor(session string) (string, error) {
	if !threadSession.MatchString(session) {
		return "", eventlog.Invalid("X-Bifrost-Session %q is not a session id", session)
	}
	return "thread:" + session, nil
}

// Writer is who a write is recorded as (Actor) and the token that made it
// (Token, the principal's name). A thread names itself, so its registration
// keeps the token that first wrote as it (review 175 B3), and only that
// token may write as it or read its mail afterwards (TD-370). Relayed is a
// write the runtime makes for the actor from what it sent before (a brief
// answering its mail), not a request: no token is held to the session.
type Writer struct {
	Actor, Token string
	Relayed      bool
	// Host is the host a host-bound writer is bound to: a collector,
	// thread:collector@<Host> (R5:5.5-12 §1.4). Its external work carries a
	// session identity and records the host; any other writer's has neither.
	Host string
}

// ErrNotYours: a thread's session is another token's (TD-370); nothing was
// written or read.
var ErrNotYours = errors.New("store: the session is another token's")

// registeredBy starts the note of a thread's registration, which names the
// token after it; registrations before the token had a field of its own
// (2026-10-19) are read by it.
const registeredBy = "first write of a thread, by token "

// HoldsToken checks the writer of a registered thread against the token its
// registration names: a session is the token's that first wrote as it, so
// another token cannot write as the thread or read its mail (TD-370). A
// relayed write is the runtime's own and is not held.
func HoldsToken(ctx context.Context, tx pgx.Tx, w Writer) error {
	if w.Relayed {
		return nil
	}
	var token, note string
	err := tx.QueryRow(ctx, `SELECT COALESCE(body_json->>'token', ''), COALESCE(body_json->>'note', '') FROM agentrt.event
	  WHERE event_stream_id = 'system' AND event_type = 'actor_registered' AND body_json->'actors' ? $1
	  ORDER BY event_id LIMIT 1`, w.Actor).Scan(&token, &note)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if t, ok := strings.CutPrefix(note, registeredBy); token == "" && ok {
		token = t
	}
	switch {
	case token == "":
		return fmt.Errorf("%w: %s names no token that registered it, so no token may act as it; a new session can", ErrNotYours, w.Actor)
	case token != w.Token:
		return fmt.Errorf("%w: %s was registered by another token; a thread writes and reads its mail with the token it first wrote with",
			ErrNotYours, w.Actor)
	}
	return nil
}

// HoldsSessionTx is the check a thread's write makes (CheckWriter: not
// retired, of its kind, its session held to its token), for a read of its
// mail or a replay of its key, which make no write: a thread not registered
// yet holds nothing here, nor does any other actor. It takes the thread's
// writer lock first and keeps it to the end of the transaction, so a first
// write registering the session cannot commit between this check and the
// read after it (review 320 A2, A3): the read sees the session as it was
// checked. Its callers take no lock a writer waits on while holding the
// writer's lock: a key's lock comes before it, as in a write.
func HoldsSessionTx(ctx context.Context, tx pgx.Tx, w Writer) error {
	if k := KindOf(w.Actor); k != "thread" && k != "agent" {
		return nil
	}
	if err := Lock(ctx, tx, w.Actor); err != nil {
		return err
	}
	_, err := CheckWriter(ctx, tx, w)
	Hook("held", w.Actor)
	return err
}

// KindOf is the kind of actor a name says it is ("" when it says none).
func KindOf(actor string) string {
	switch {
	case actor == "owner", actor == "controller":
		return actor
	case strings.HasPrefix(actor, "thread:"):
		return "thread"
	case strings.HasPrefix(actor, "seat:"):
		return "seat"
	case strings.HasPrefix(actor, "host:"):
		return "host"
	case strings.HasPrefix(actor, "exec:"):
		return "executor"
	case strings.HasPrefix(actor, "agent:"):
		return "agent" // an external agent bringing issues (0009; holdsWriter, intake.go)
	}
	return ""
}

// ThreadsPerDay bounds how many threads proposals and uploads register in a
// day: a session is what a caller says it is, so without a bound any token
// could grow the actor table and the system stream without end (review 175
// B3). Controls are not bounded: anyone authenticated may stop or pause
// (v5 §5.2), and a new session must not find that taken by proposals
// (review 176 part 2 A1); each control is a row of its own anyway.
const ThreadsPerDay = 200

// CheckWriter checks a writer without writing: its actor is of the kind its
// name says and not retired, and a thread's writer holds its session
// (HoldsToken; ErrNotYours). It reports whether the actor is registered; a
// thread that is not is fine (its first write registers it), any other
// actor that is not is ErrInvalid.
func CheckWriter(ctx context.Context, tx pgx.Tx, w Writer) (bool, error) {
	want := KindOf(w.Actor)
	if want == "" {
		return false, eventlog.Invalid("%q is not an actor name", w.Actor)
	}
	var kind string
	var retired bool
	err := tx.QueryRow(ctx, `SELECT actor_kind, retired_at IS NOT NULL FROM agentrt.actor WHERE actor_id = $1`, w.Actor).Scan(&kind, &retired)
	switch {
	case err == nil && retired:
		return true, eventlog.Invalid("actor %s is retired", w.Actor)
	case err == nil && kind != want:
		return true, eventlog.Invalid("actor %s is a %s, not a %s", w.Actor, kind, want)
	case err == nil && (want == "thread" || want == "agent"):
		return true, holds(ctx, tx, w)
	case err == nil:
		return true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	case want != "thread":
		return false, eventlog.Invalid("actor %s is not registered", w.Actor)
	}
	return false, nil
}

// Lock serializes the writes of one actor (and its registration). It
// touches no row and no stream, so a transaction may take it before any other
// lock (review 187 R1); taking it again later in the same transaction is a
// no-op.
func Lock(ctx context.Context, tx pgx.Tx, actor string) error {
	if KindOf(actor) == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742107, hashtext($1))`, actor)
	return err
}

// Ensure checks the writer in the caller's transaction and registers a
// thread the first time it writes: actor_registered by the controller,
// naming the token. Every write route calls it before any lock of the rows
// it writes (review 176 A1): its locks are the writer's own, then, for a
// registration, the registration lock and the system stream. bounded holds
// a registration to ThreadsPerDay.
func Ensure(ctx context.Context, tx pgx.Tx, w Writer, bounded bool) error {
	if err := Lock(ctx, tx, w.Actor); err != nil {
		return err
	}
	registered, err := CheckWriter(ctx, tx, w)
	if err != nil || registered {
		return err
	}
	// One registration at a time, so the count below is the count when it
	// commits (review 176 B1).
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742109)`); err != nil {
		return err
	}
	if bounded {
		var recent int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.actor WHERE actor_kind = 'thread'
		  AND created_at > clock_timestamp() - interval '24 hours'`).Scan(&recent); err != nil {
			return err
		}
		Hook("counted", w.Actor)
		var limit int
		if limit, err = threadLimitIn(ctx, tx); err != nil {
			return err
		}
		if recent >= limit {
			return eventlog.Invalid("%d threads were registered in the last day, the most there may be; %s must wait", recent, w.Actor)
		}
	}
	// The registration names the token the session is held to (TD-370); a
	// relayed write holds it to none.
	body := map[string]any{"actors": []string{w.Actor}, "note": registeredBy + w.Token, "token": w.Token}
	if w.Relayed {
		body = map[string]any{"actors": []string{w.Actor}, "note": "first write of a thread, relayed by the runtime (" + w.Token + ")"}
	}
	if _, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "actor_registered", Actor: "controller", Body: body}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'thread', $2)`,
		w.Actor, fmt.Sprintf("Thread %s (first written by token %s)", w.Actor[len("thread:"):], w.Token))
	return err
}

// ActorKind is the kind of a registered actor that is not retired;
// pgx.ErrNoRows when there is none.
func ActorKind(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
	var kind string
	err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1 AND retired_at IS NULL`, actor).Scan(&kind)
	return kind, err
}

// Idempotency keys: a write's key and request, answered once and the same
// way on every retry (the write routes use them).

// IdempotentAs is idempotent for a writer's request: a thread's first
// answer is given again only to the token its session is held to (TD-370),
// checked in each transaction that reads the key; the write itself checks it
// again (ensureWriter).
func IdempotentAs(ctx context.Context, db eventlog.DB, w Writer, route, key string, request any,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, err
	}
	return keyed(ctx, db, w.Actor+"|"+route+"|"+key, route, sum, func(tx pgx.Tx) error { return HoldsSessionTx(ctx, tx, w) }, pre, op)
}

func keyed(ctx context.Context, db eventlog.DB, id, route, sum string, holds func(pgx.Tx) error,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	lookup := func(tx pgx.Tx) (map[string]any, bool, error) {
		if holds != nil {
			if err := holds(tx); err != nil {
				return nil, false, err
			}
		}
		var storedSum, stored string
		err := tx.QueryRow(ctx, `SELECT request_sha256, response_json::text FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
			id).Scan(&storedSum, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if storedSum != sum {
			return nil, true, ErrConflict
		}
		out, err := response([]byte(stored))
		return out, true, err
	}
	var out map[string]any
	var found bool
	if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, found, err = lookup(tx)
		return err
	}); err != nil || found {
		return out, err
	}
	Hook("missed", id)
	if pre != nil {
		if err := pre(ctx); err != nil {
			return nil, err
		}
	}
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, id); err != nil {
			return err
		}
		var err error
		if out, found, err = lookup(tx); err != nil || found {
			return err
		}
		resp, err := op(tx)
		if err != nil {
			return err
		}
		rb, err := canon.Encode(resp)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.idempotency_key (idempotency_key_id, route, request_sha256, response_json, expires_at)
		  VALUES ($1, $2, $3, $4::jsonb, clock_timestamp() + interval '7 days')`, id, route, sum, string(rb)); err != nil {
			return err
		}
		out, err = response(rb)
		return err
	})
	return out, err
}

// response decodes a stored answer the same way on the first call and on a
// retry, so both return identical values.
func response(b []byte) (map[string]any, error) {
	v, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("store: a stored response is not an object")
	}
	return m, nil
}
