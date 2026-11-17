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
