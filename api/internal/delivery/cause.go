package delivery

import (
	"context"
	"regexp"
)

// LabelCause names, on a PipelineRun the platform starts, the bus event that
// woke the start: "ev-<ledger event id>" (2026-11-17, with the communication
// layer: no bus event carried a cause, so "who did what because of which
// event" could not be followed). Gate-cluster carries it into the run's
// ci.run.started and ci.run.finished; the release recorder into its
// release.deploy.finished. Same name as gatecluster.LabelCause.
const LabelCause = "bifrost.io/cause"

// LabelCauseEnv is the environment whose ledger holds that event (AGENTRT_ENV
// of the process that started the run): event ids are per ledger, and every
// environment's gate-cluster sees the same runs in cicd. Same name as
// gatecluster.LabelCauseEnv.
const LabelCauseEnv = "bifrost.io/cause-env"

// causeRE is a ledger event's bus id (relay.EventID).
var causeRE = regexp.MustCompile(`^ev-[1-9][0-9]{0,18}$`)

// causeEnvRE is an environment name (bus relay.EnvRE).
var causeEnvRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

type causeKey struct{}

type cause struct{ id, env string }

// WithCause marks the starts made with ctx as caused by the bus event id of
// env's ledger; an id that is not "ev-<n>", or an env that is not an
// environment name, marks nothing.
func WithCause(ctx context.Context, id, env string) context.Context {
	if !causeRE.MatchString(id) || !causeEnvRE.MatchString(env) {
		return ctx
	}
	return context.WithValue(ctx, causeKey{}, cause{id: id, env: env})
}

func causeOf(ctx context.Context) (id, env string) {
	c, _ := ctx.Value(causeKey{}).(cause)
	return c.id, c.env
}

// AnnRequester names, on a PipelineRun started over HTTP, who asked for it:
// the calling thread's actor ("thread:<session>", from its X-Bifrost-Session
// header, as the questions and approvals routes take it). An annotation, not
// a label: a thread actor has a colon and outgrows a label value. Gate-cluster
// carries it into the run's ci.run.started, so a subscription can wake the one
// who started a PROD release (2026-11-17, with the communication layer: main's
// known regressions are pushed to whoever is releasing). Routing only; it
// authorizes nothing.
const AnnRequester = "bifrost.io/requester"

// requesterSession is a thread's session id (actor.threadSession's pattern).
var requesterSession = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type requesterKey struct{}

// WithRequester marks the starts made with ctx as asked for by the thread
// whose session id is session; anything else marks nothing.
func WithRequester(ctx context.Context, session string) context.Context {
	if !requesterSession.MatchString(session) {
		return ctx
	}
	return context.WithValue(ctx, requesterKey{}, "thread:"+session)
}

func requesterOf(ctx context.Context) string {
	r, _ := ctx.Value(requesterKey{}).(string)
	return r
}
