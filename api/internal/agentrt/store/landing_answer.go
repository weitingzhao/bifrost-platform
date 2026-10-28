package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// What became of one answered landing card (CUT §8, review 647). Applied
// wrote a retry or failed{declined}; moot needed nothing; refused is what the
// Owner has to know: the answer was not carried out (a retry under a mission
// that is no longer active, or one the protocol does not take).
const (
	LandingAnswerApplied = "applied"
	LandingAnswerMoot    = "moot"
	LandingAnswerRefused = "refused"
)

// LandingAnswerResult is how an answer was handled. Recorded is false when an
// earlier pass had already recorded it (the stored verdict is returned, not a
// new one). Acked says the controller acknowledged the card after the record
// committed; a result that is not acked is still handled, and the next pass
// returns it again and acks.
type LandingAnswerResult struct {
	Outcome, Why    string
	Recorded, Acked bool
	// Record is the stored record as committed: the answer as given (who,
	// when, its text), the binding and the mission's state then. S3 builds
	// the Owner's notice from it, never from the card or mission as they are
	// now (review 656 A-R3). Nil when nothing was handled.
	Record map[string]any
}

// ErrLandingAnswerBinding: the card is not bound to an operation of this root
// on the stream, or its stored record names another root or operation. It is
// S3's binding at fault, not the Owner's answer, so nothing is recorded.
var ErrLandingAnswerBinding = errors.New("landing answer: the card's binding does not match the stream")

// HandleLandingAnswer handles one answered landing card exactly once (CUT §8).
// Under the claim's locks (task, then its mission, then the repository stream)
// it looks for a record of this question on the stream; if there is one it
// returns it and writes nothing. Otherwise it decides, appends the retry or
// failed{declined} when the answer applies, and appends the record in the same
// transaction. A refusal is a record, not an error, so it is not rolled back;
// only a database failure returns an error and leaves nothing, and the next
// pass tries again. The caller acknowledges the card only after this commits.
//
// The lock order is the claim's, so it is ordered against a close as a claim
// is: the close decides first and a retry is refused (mission_not_active), or
// the retry commits first and the close reads it (landingHolds, review 645
// R1). The record is a note on the repository stream with no "landing" key,
// so LandingState does not fold it.
func HandleLandingAnswer(ctx context.Context, db DB, q Question, root, after string, answeredAt time.Time) (LandingAnswerResult, error) {
	const stream = "landing:bifrost-platform"
	var res LandingAnswerResult
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		res, err = handleLandingAnswerTx(ctx, tx, q, root, after, answeredAt)
		return err
	})
	if err != nil {
		// Nothing is known to be committed: no verdict is reported (review
		// 656 A-R2). A commit that did land is found by the next pass.
		return LandingAnswerResult{}, err
	}
	return res, nil
}

// decideLandingAnswer is the answer's verdict on the folded stream, and the
// landing record to append when it applies. It checks the record against the
// protocol before saying applied, so an answer the protocol would refuse is
// recorded as refused rather than failing the transaction (review 655 B1).
// Moot is only for what needs nothing at all (already done, or a later
// operation took over); an answer that was not carried out for any other
// reason is refused, so the Owner sees it and answers a new card (review 655
// R1). A card bound to no operation of this root is S3's error.
func decideLandingAnswer(s landing.State, root, after string, q Question, answeredAt time.Time, missionState string, at time.Time) (string, string, map[string]any, error) {
	o, exists := s.Operations[after]
	switch {
	case !exists || o.Task != root:
		return "", "", nil, fmt.Errorf("%w: %s names %s of %s", ErrLandingAnswerBinding, q.ID, after, root)
	case s.LatestOperation(root) != after:
		return LandingAnswerMoot, "superseded", nil, nil
	case !slices.Contains(landingAnswerChoices, q.Option):
		return LandingAnswerRefused, "unknown_choice", nil, nil
	case answeredAt.Before(o.ClaimedAt):
		return LandingAnswerRefused, "answered_before", nil, nil
	}
	r := map[string]any{"v": landing.Version, "op": after}
	if q.Option == "confirm_main_lost" {
		// CUT §10.6 (1A): only on a push reported ok whose R main no longer
		// shows, and only by the Owner's answer.
		switch {
		case o.Phase == "failed" && o.Outcome == "main_lost":
			return LandingAnswerMoot, "already_lost", nil, nil
		case o.Phase != "main_result" || o.Outcome != "ok":
			return LandingAnswerRefused, "not_main_lost", nil, nil
		}
		r["kind"], r["why"] = "failed", "main_lost"
	} else if q.Option == "try_again" {
		switch {
		case o.Retried:
			return LandingAnswerMoot, "already_retried", nil, nil
		case o.Phase != "failed":
			return LandingAnswerRefused, "not_failed", nil, nil
		case answeredAt.Before(o.LastAt):
			return LandingAnswerRefused, "answered_before", nil, nil
		case missionState != "active":
			return LandingAnswerRefused, "mission_not_active", nil, nil
		}
		// The protocol keeps a bounded reason; the whole text is in the
		// record (answer_text). A long note is not a reason to refuse (review
		// 656 A-R5).
		reason := boundedReason(q.Text)
		if reason == "" {
			reason = "try_again"
		}
		r["kind"], r["root"], r["after_op"], r["reason"], r["by"] = "retry", root, after, reason, q.AnsweredBy
	} else {
		if o.Outcome == "declined" {
			return LandingAnswerMoot, "already_declined", nil, nil
		}
		r["kind"], r["why"] = "failed", "declined"
	}
	rec, err := landing.Parse(r)
	if err != nil {
		// The record is built here: a parse failure is a bug, not a verdict.
		return "", "", nil, fmt.Errorf("landing answer %s: %w", q.ID, err)
	}
	if err := s.Check(landing.Entry{At: at, Record: rec}); err != nil {
		switch q.Option {
		case "try_again":
			return LandingAnswerRefused, "not_retryable", nil, nil
		case "confirm_main_lost":
			return LandingAnswerRefused, "not_main_lost", nil, nil
		}
		return LandingAnswerRefused, "not_declinable", nil, nil
	}
	switch q.Option {
	case "try_again":
		return LandingAnswerApplied, "retry", r, nil
	case "confirm_main_lost":
		return LandingAnswerApplied, "main_lost", r, nil
	}
	return LandingAnswerApplied, "declined", r, nil
}

func landingAnswerText(outcome, why, choice, missionState string) string {
	switch outcome {
	case LandingAnswerApplied:
		return "Landing answer applied: " + choice
	case LandingAnswerRefused:
		if why == "mission_not_active" {
			return fmt.Sprintf("Landing answer refused: %s was not carried out, the mission is %s", choice, missionState)
		}
		return fmt.Sprintf("Landing answer refused: %s was not carried out (%s)", choice, why)
	}
	return fmt.Sprintf("Landing answer needed nothing: %s (%s)", choice, why)
}

// landingAnswerWhys is the closed vocabulary of a record's why, by outcome.
var landingAnswerWhys = map[string][]string{
	LandingAnswerApplied: {"retry", "declined", "main_lost"},
	LandingAnswerMoot:    {"superseded", "already_retried", "already_declined", "already_lost"},
	LandingAnswerRefused: {"mission_not_active", "not_retryable", "not_declinable", "not_failed", "answered_before", "unknown_choice", "not_main_lost"},
}

// landingAnswerChoices are the semantic choices a landing card offers:
// retry a failure, do not merge, and confirm that a push reported ok is lost
// (CUT §10.6).
var landingAnswerChoices = []string{"try_again", "do_not_merge", "confirm_main_lost"}

// landingAnswerRule is what each why can follow, as decideLandingAnswer
// reaches it (review 657 B657-1): the choices ("any" for every choice; nil
// for a choice that is none of the known ones) and whether the mission must
// have been active, not active, or either ("").
type landingAnswerRule struct {
	choices []string
	mission string // "active", "not_active" or ""
}

var landingAnswerRules = map[string]landingAnswerRule{
	"superseded":         {[]string{"any"}, ""},
	"unknown_choice":     {nil, ""},
	"answered_before":    {landingAnswerChoices, ""},
	"retry":              {[]string{"try_again"}, "active"},
	"not_retryable":      {[]string{"try_again"}, "active"},
	"mission_not_active": {[]string{"try_again"}, "not_active"},
	"already_retried":    {[]string{"try_again"}, ""},
	"not_failed":         {[]string{"try_again"}, ""},
	"declined":           {[]string{"do_not_merge"}, ""},
	"already_declined":   {[]string{"do_not_merge"}, ""},
	"not_declinable":     {[]string{"do_not_merge"}, ""},
	"main_lost":          {[]string{"confirm_main_lost"}, ""},
	"already_lost":       {[]string{"confirm_main_lost"}, ""},
	"not_main_lost":      {[]string{"confirm_main_lost"}, ""},
}

// missionStates is every state a mission row can hold (migrations 0001): a
// record keeps the state its mission was in then, a rebound root's mission
// included.
var missionStates = []string{"proposed", "awaiting_owner", "active", "paused", "done", "stopped", "expired"}

// badLandingAnswer is why a record body is refused, or "": every field there,
// the outcome and why a pair of the vocabulary, and the why one that can
// follow the choice and the mission's state as decideLandingAnswer decides
// (review 656 A-R1, 657 B657-1 and R657-4), from a console answer at a real
// time. The choice may be empty: a card answered with text only is an
// unknown choice, recorded as such.
func badLandingAnswer(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_answer is not an object"
	}
	for _, k := range []string{"question_id", "root", "op", "outcome", "why", "by", "answered_at", "channel", "mission_state"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_answer needs " + k
		}
	}
	for _, k := range []string{"choice", "answer_text"} {
		if _, ok := m[k].(string); !ok {
			return "landing_answer needs " + k + " (empty when the Owner gave none)"
		}
	}
	at, err := time.Parse(time.RFC3339Nano, m["answered_at"].(string))
	if err != nil || at.IsZero() || at.Year() < 2000 {
		return "landing_answer answered_at is not a real time"
	}
	if m["channel"] != "console" {
		return "landing_answer is only for a console answer"
	}
	state, choice, why := m["mission_state"].(string), m["choice"].(string), m["why"].(string)
	if !slices.Contains(missionStates, state) {
		return "landing_answer mission_state is not a mission state"
	}
	whys, ok := landingAnswerWhys[m["outcome"].(string)]
	if !ok || !slices.Contains(whys, why) {
		return fmt.Sprintf("landing_answer: %v / %v is not in the vocabulary", m["outcome"], why)
	}
	rule := landingAnswerRules[why]
	known := slices.Contains(landingAnswerChoices, choice)
	switch {
	case rule.choices == nil && known,
		rule.choices != nil && !slices.Contains(rule.choices, "any") && !slices.Contains(rule.choices, choice):
		return fmt.Sprintf("landing_answer: %s does not follow the choice %q", why, choice)
	case rule.mission == "active" && state != "active", rule.mission == "not_active" && state == "active":
		return fmt.Sprintf("landing_answer: %s does not follow a mission %s", why, state)
	}
	return ""
}

// checkLandingAnswerAppend: only the controller writes an answer's record,
// on the repository stream, under the root it names (review 656 A-R1).
func checkLandingAnswerAppend(ev Event) error {
	v, exists := ev.Body["landing_answer"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != "landing:bifrost-platform" || m == nil || ev.TaskID == "" || ev.TaskID != m["root"] {
		return fmt.Errorf("landing_answer is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_answer is not a landing record")
	}
	return nil
}

// boundedReason is text cut to the protocol's reason limit on a rune
// boundary, with a marker when it was cut.
func boundedReason(text string) string {
	const limit, marker = 1000, " …"
	if len(text) <= limit {
		return text
	}
	cut := limit - len(marker)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + marker
}

// handleLandingAnswerTx is HandleLandingAnswer inside the caller's
// transaction: the landing card handler checks its card still applies under
// the same locks first (review 670 B-02).
func handleLandingAnswerTx(ctx context.Context, tx pgx.Tx, q Question, root, after string, answeredAt time.Time) (LandingAnswerResult, error) {
	const stream = "landing:bifrost-platform"
	var res LandingAnswerResult
	var mission, missionState string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, root).Scan(&mission); err != nil {
		return res, err
	}
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission).Scan(&missionState); err != nil {
		return res, err
	}
	if err := lockStream(ctx, tx, stream); err != nil {
		return res, err
	}
	// A separate statement after the stream lock, so a pass that waited
	// for another's commit sees its record (Read Committed, review 655).
	var stored map[string]any
	var actor, storedTask string
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_answer', actor_id, COALESCE(task_id, '') FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing_answer'->>'question_id' = $2 ORDER BY seq LIMIT 1`,
		stream, q.ID).Scan(&stored, &actor, &storedTask)
	if err == nil {
		// A stored record is a credential the Ack rests on: it must be
		// the controller's, whole and consistent (review 656 A-R1).
		if why := badLandingAnswer(stored); why != "" || actor != "controller" || storedTask != stored["root"] {
			return res, fmt.Errorf("landing answer %s: stored record is not valid: %s (actor %s, task %s)", q.ID, why, actor, storedTask)
		}
		if stored["root"] != root || stored["op"] != after {
			return res, fmt.Errorf("%w: %s was recorded for %v %v", ErrLandingAnswerBinding, q.ID, stored["root"], stored["op"])
		}
		res = LandingAnswerResult{Outcome: stored["outcome"].(string), Why: stored["why"].(string), Record: stored}
		return res, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return res, err
	}
	s, err := LandingState(ctx, tx, stream)
	if err != nil {
		return res, err
	}
	at := eventClock().UTC().Truncate(time.Microsecond)
	outcome, why, record, err := decideLandingAnswer(s, root, after, q, answeredAt, missionState, at)
	if err != nil {
		return res, err
	}
	hook("landing answer: decided", q.ID)
	if record != nil {
		if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": "Landing question: " + q.Option, "landing": record}}); err != nil {
			return res, err
		}
		hook("landing answer: business appended", q.ID)
	}
	// The answer as given is kept here: the question store trims and an
	// Ack is no archive (review 655 R2).
	body := map[string]any{"question_id": q.ID, "root": root, "op": after, "choice": q.Option, "outcome": outcome, "why": why,
		"by": q.AnsweredBy, "answered_at": answeredAt.UTC().Format(time.RFC3339Nano), "channel": q.Channel,
		"answer_text": q.Text, "mission_state": missionState}
	if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: mission, TaskID: root,
		Body: map[string]any{"text": landingAnswerText(outcome, why, q.Option, missionState), "landing_answer": body}}); err != nil {
		return res, err
	}
	res = LandingAnswerResult{Outcome: outcome, Why: why, Recorded: true, Record: body}
	return res, nil

}
