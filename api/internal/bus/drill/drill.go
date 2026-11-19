// Package drill gives the problem channels that never fire in normal life a
// controlled proof that their event can be written (the Owner, 2026-11-18:
// first make problems visible). Once a day per channel it appends one event of
// the channel's own type to its own stream, drill:<channel id>, with the
// quiet value of that type: an alert resolved in the namespace drill, a state
// ok from the source drill, a loop healthy, a job succeeded, each on a subject
// that names the drill.
//
// A drill wakes nobody and counts as nothing real:
//   - no subscription row matches a drill's change (a test reads every row);
//   - the relay publishes it, which is what it proves, counted under its own
//     stream, so no real topic's count moves; the gate and the observer read the
//     ledger without the drill: streams (StreamPrefix);
//   - the evidence a row names in problems.yaml never names a drill stream; only
//     its drill: key does, and the Bus page shows it apart.
//
// Pass is the only writer, and only the workers' wiring calls it.
package drill

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/opsevents"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownernotice"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownerq"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// StreamPrefix starts the ledger stream of every drill; the bus's readers leave
// these streams out.
const StreamPrefix = relay.DrillStreamPrefix

// Stream is the ledger stream of one channel's drill.
func Stream(id string) string { return StreamPrefix + id }

// Every is how often one channel is drilled at most; the stream's last event
// says when it was, so a restart does not repeat one. It is short enough that a
// drill published just before a rollout, whose count went with the old process
// before anyone read it, is soon followed by one that is read (2026-11-19).
const Every = 6 * time.Hour

// checkEvery is how often the loop looks for a drill that is due.
const checkEvery = 15 * time.Minute

const (
	actorID = "controller"
	// source and namespace say "drill" wherever the event's type has a place for it.
	source = "drill"
	nspace = "drill"
	cause  = "drill: not a real event"
)

type drill struct {
	id, typ string
	body    map[string]any
}

// drills is one event per channel that never fires in normal life, each of the
// type and the quiet value its real event has.
func drills() []drill {
	var out []drill
	for _, c := range []struct{ id, name string }{
		{"N06", alerts.NameHeartbeatMissing}, {"L27", alerts.NameHeartbeatMissing},
		{"Y-1", alerts.NameApprovalClaim},
		{"Y-3", alerts.NameLoopFailing}, {"Y-4", alerts.NameArtifactRefused},
		{"Y-5", alerts.NameRelayUnreachable}, {"X-A24", alerts.NameWebhookFailing},
		{"Y-6", alerts.NameRuleGroupsMissing},
	} {
		if a, ok := alerts.Normalize(c.name, nspace, "", "resolved"); ok {
			out = append(out, drill{c.id, alerts.Type, alerts.Body(a)})
		}
	}
	for _, e := range []struct {
		id string
		ev opsevents.Event
	}{
		{"R12", opsevents.EnvHealth("drill/r12", "ok", source, cause)},
		{"L11", opsevents.EnvHealth("drill/l11", "ok", source, cause)},
		{"L12", opsevents.PluginStatus("drill-l12", "ok", source, cause)},
		{"S14", opsevents.Loop("drill/s14", true)},
		{"X-A26", opsevents.Loop("drill/x-a26", true)},
		{"L13", opsevents.Job("drill-l13", true)},
	} {
		if opsevents.Valid(e.ev) {
			out = append(out, drill{e.id, e.ev.Type, opsevents.Body(e.ev)})
		}
	}
	// A question id the store never issues (it is random), with a status of its own.
	const question = "q_d000000000000000"
	for _, q := range []struct {
		id string
		q  ownerq.Question
	}{
		{"N02", ownerq.Question{Type: ownerq.TypeAsked, ID: question, Status: source}},
		{"L16", ownerq.Question{Type: ownerq.TypeAnswered, ID: question, Status: source}},
	} {
		if ownerq.Valid(q.q) {
			out = append(out, drill{q.id, q.q.Type, ownerq.Body(q.q)})
		}
	}
	for _, id := range []string{"L17", "N04"} {
		n := ownernotice.Notice{Type: ownernotice.TypePolicy, Subject: "drill/" + strings.ToLower(id), Change: ownernotice.ChangeExpiring}
		out = append(out, drill{id, n.Type, ownernotice.Body(n)})
	}
	// A fix, not a report: a known regression would wake the commit's session.
	zero := strings.Repeat("0", 40)
	out = append(out, drill{"X-A29", regressions.TypeFixed, regressions.FixedBody("drill", zero, zero)})
	return out
}

// Pass appends the drills that are due and says how many. Before the bus's
// actor exists it writes nothing.
func Pass(ctx context.Context, db eventlog.DB) (int, error) {
	n := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		if ok, err := actor.Exists(ctx, tx, actorID); err != nil || !ok {
			return err
		}
		now := eventlog.Now(ctx)
		for _, d := range drills() {
			stream := Stream(d.id)
			if last, found, err := eventlog.Last(ctx, tx, stream); err != nil {
				return err
			} else if found && now.Sub(last.At) < Every {
				continue
			}
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: d.typ, Actor: actorID, Body: d.body}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// Run drills what is due, now and every checkEvery, until ctx ends. db returns
// nil while the database is not connected.
func Run(ctx context.Context, db func() eventlog.DB, logf func(format string, args ...any)) {
	safego.Go("bus-drill", func() {
		tick := time.NewTicker(checkEvery)
		defer tick.Stop()
		for {
			if d := db(); d != nil {
				if n, err := Pass(ctx, d); err != nil {
					logf("bus drill: not written: %v", err)
				} else if n > 0 {
					logf("bus drill: %d written to %s*", n, StreamPrefix)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
}
