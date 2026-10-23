package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The presence pass (the controller's agentrt-presence loop, every five
// seconds; presence.go has the view). A host the controller reports offline
// (HostPresence.ReportedOffline) gets a policy_alert on its stream (rule
// host_offline, its artifact saying when it last polled and what was running
// on it); once it polls again, a note there that it is back online. An
// executor that is known not to be available gets a policy_alert on the
// controller stream (rule executor_unavailable, line_digest naming the
// executor, its artifact saying why); once it is available again, a note
// there. Each turn is recorded once, as a host's disk is (hostdisk.go): the
// latest record of each host and executor is its state, so a pass after a
// restart says nothing it said before. Each pass that records a turn also
// tells the Owner in his conversation with the chief of staff, as the
// platform and not the seat (TellWaitingBriefs' precedent, briefwait.go): the
// chief of staff runs on the hosts it would report, so only the platform can
// say it within the minute. The note names the jobs no executor can take now
// (chief_of_staff among them: the chief of staff cannot answer). The records
// and the note are one transaction, written under the seat's lock (taken
// first, as AcceptBrief and TellWaitingBriefs take it), and the view and the
// records are read again under it, so two passes never record a turn twice.

// The alerts' phrases, and the notes' words after the host or executor.
const (
	hostOfflinePhrase         = "a host stopped polling"
	executorUnavailablePhrase = "an executor cannot take work now"
	hostBackOnline            = " is back online"
	executorAvailableAgain    = " is available again"
)

// presencePassTimeout bounds a pass, and waitsLockTimeout each lock it waits
// for, as the waits' pass is bounded.
const presencePassTimeout = 20 * time.Second

// presenceTurn is a turn of a host or an executor: offline or unavailable
// (down), or back; since is when the open alert a turn back answers was
// recorded.
type presenceTurn struct {
	host, executor string
	down           bool
	since          time.Time
}

// Presence is one pass at now (zero: the database's clock): it records each
// turn and tells the Owner of the pass's turns in one note. It returns how
// many turns it recorded.
func Presence(ctx context.Context, db DB, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, presencePassTimeout)
	defer cancel()
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		p, err := readPresence(ctx, tx, now)
		if err != nil {
			return err
		}
		turns, err := presenceTurns(ctx, tx, p)
		if err != nil || len(turns) == 0 {
			return err
		}
		if err = ensureWriter(ctx, tx, Writer{Actor: Seat}, false); err != nil {
			return err
		}
		if p, err = readPresence(ctx, tx, now); err != nil {
			return err
		}
		if turns, err = presenceTurns(ctx, tx, p); err != nil || len(turns) == 0 {
			return err
		}
		if err = recordTurns(ctx, tx, p, turns); err != nil {
			return err
		}
		n = len(turns)
		return nil
	})
	return n, err
}

// presenceTurns compares the view with each host's and executor's latest
// record. Not known is no turn.
func presenceTurns(ctx context.Context, tx pgx.Tx, p presence) ([]presenceTurn, error) {
	var turns []presenceTurn
	for _, h := range p.hosts {
		open, since, err := latestPresence(ctx, tx, "host:"+h.HostID, `body_json->>'rule' = 'host_offline'`, h.HostID+hostBackOnline)
		if err != nil {
			return nil, err
		}
		switch {
		case h.ReportedOffline && !open:
			turns = append(turns, presenceTurn{host: h.HostID, down: true})
		case open && h.Online:
			turns = append(turns, presenceTurn{host: h.HostID, since: since})
		}
	}
	for _, e := range p.executors {
		if e.Available == nil {
			continue
		}
		open, since, err := latestPresence(ctx, tx, ControllerStream,
			`body_json->>'rule' = 'executor_unavailable' AND body_json->>'line_digest' = `+sqlText(lineDigest(e.Executor)), e.Executor+executorAvailableAgain)
		if err != nil {
			return nil, err
		}
		switch {
		case !*e.Available && !open:
			turns = append(turns, presenceTurn{executor: e.Executor, down: true})
		case *e.Available && open:
			turns = append(turns, presenceTurn{executor: e.Executor, since: since})
		}
	}
	return turns, nil
}

// latestPresence reads whether the latest of stream's presence records of one
// host or executor (the controller's policy_alert matching alert, or its note
// starting with back) is an alert, and when it was recorded.
func latestPresence(ctx context.Context, tx pgx.Tx, stream, alert, back string) (bool, time.Time, error) {
	var kind string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT event_type, at FROM agentrt.event WHERE event_stream_id = $1 AND actor_id = 'controller' AND (
	    (event_type = 'policy_alert' AND `+alert+`) OR (event_type = 'note' AND starts_with(body_json->>'text', $2)))
	  ORDER BY seq DESC LIMIT 1`, stream, back).Scan(&kind, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, at, nil
	}
	return kind == "policy_alert", at, err
}

// recordTurns writes each turn's record and the Owner's note.
func recordTurns(ctx context.Context, tx pgx.Tx, p presence, turns []presenceTurn) error {
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	var lines []string
	for _, t := range turns {
		var line string
		switch {
		case t.host != "":
			h := hostIn(p, t.host)
			if t.down {
				running, err := runningOn(ctx, tx, t.host)
				if err != nil {
					return err
				}
				line = "Host " + t.host + " is offline: " + lastPollWords(h, p.now) + "." + running
				if err = presenceAlert(ctx, tx, "host:"+t.host, "host_offline", hostOfflinePhrase, "", line); err != nil {
					return err
				}
			} else {
				polled := "it polls again"
				if h.LastPollAt != nil {
					polled = "it polled at " + stamp(*h.LastPollAt)
				}
				text := t.host + hostBackOnline + ": " + polled + "; it was reported offline at " + stamp(t.since) + "."
				line = "Host " + text
				if _, err := Append(ctx, tx, Event{Stream: "host:" + t.host, Type: "note", Actor: "controller",
					Body: map[string]any{"text": text}}); err != nil {
					return err
				}
			}
		default:
			e := p.executor(t.executor)
			if t.down {
				line = t.executor + " is unavailable: " + e.Why + ". It is a candidate of " + strings.Join(e.Jobs, ", ") + "."
				if err := presenceAlert(ctx, tx, ControllerStream, "executor_unavailable", executorUnavailablePhrase,
					lineDigest(t.executor), line); err != nil {
					return err
				}
			} else {
				line = t.executor + executorAvailableAgain + ": " + e.Why + "."
				if _, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller",
					Body: map[string]any{"text": line + " It was reported unavailable at " + stamp(t.since) + "."}}); err != nil {
					return err
				}
			}
		}
		lines = append(lines, "- "+line)
	}
	_, err := postMessageAs(ctx, tx, Seat, "owner", "note", []byte(presenceNote(p, turns, lines)), "", "", false)
	return err
}

func hostIn(p presence, id string) HostPresence {
	for _, h := range p.hosts {
		if h.HostID == id {
			return h
		}
	}
	return HostPresence{HostID: id}
}

// lastPollWords says when h last polled, against the threshold.
func lastPollWords(h HostPresence, now time.Time) string {
	limit := fmt.Sprintf("a host is offline %d s after its last poll", int(HostLostAfter/time.Second))
	if h.LastPollAt == nil {
		return "it has never polled (" + limit + ")"
	}
	return fmt.Sprintf("its last poll was at %s, %s (%s)", h.LastPollAt.UTC().Format(time.RFC3339),
		agoWords(now.Sub(*h.LastPollAt)), limit)
}

// runningOn names the live attempts on host, for the record of its going
// offline: their work goes on only if it polls again before their leases
// lapse.
func runningOn(ctx context.Context, tx pgx.Tx, host string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT a.attempt_id, a.task_id, a.executor_actor_id, t.role FROM agentrt.attempt a
	  JOIN agentrt.task t ON t.task_id = a.task_id WHERE a.host_id = $1 AND a.state IN ('leased','running','stale')
	  ORDER BY a.attempt_id LIMIT 10`, host)
	if err != nil {
		return "", err
	}
	var parts []string
	var attempt, task, executor, role string
	if _, err = pgx.ForEachRow(rows, []any{&attempt, &task, &executor, &role}, func() error {
		parts = append(parts, fmt.Sprintf("%s of task %s (%s, %s)", attempt, task, role, executor))
		return nil
	}); err != nil || len(parts) == 0 {
		return "", err
	}
	return " Running on it: attempt" + plural(len(parts), " ", "s ") + strings.Join(parts, ", ") + ".", nil
}

// presenceAlert writes a policy_alert of rule on stream, its text kept as an
// artifact.
func presenceAlert(ctx context.Context, tx pgx.Tx, stream, rule, phrase, digest, text string) error {
	art, err := PutArtifact(ctx, tx, "controller", "log_tail", "text/plain; charset=utf-8", []byte(text))
	if err != nil {
		return err
	}
	body := map[string]any{"rule": rule, "source": "controller", "phrase": phrase, "artifact_id": art}
	if digest != "" {
		body["line_digest"] = digest
	}
	_, err = Append(ctx, tx, Event{Stream: stream, Type: "policy_alert", Actor: "controller", Body: body})
	return err
}

// presenceNote is the Owner's note of a pass: its turns, then the jobs no
// executor can take now.
func presenceNote(p presence, turns []presenceTurn, lines []string) string {
	var s strings.Builder
	s.WriteString("From the platform, not the chief of staff: who can take work changed.\n\n")
	s.WriteString(strings.Join(lines, "\n"))
	s.WriteString("\n\n")
	back := slices.ContainsFunc(turns, func(t presenceTurn) bool { return t.executor != "" && !t.down })
	switch {
	case len(p.unserved) > 0:
		jobs := slices.Clone(p.unserved)
		for i, j := range jobs {
			if j == "chief_of_staff" {
				jobs[i] = j + " (so the chief of staff cannot answer you until one can)"
			}
		}
		s.WriteString("No executor can take these jobs now: " + strings.Join(jobs, ", ") + ".\n\n")
	case back:
		s.WriteString("Every job has an executor that can take it again.\n\n")
	}
	s.WriteString("The platform says so here again when this changes.")
	return s.String()
}
