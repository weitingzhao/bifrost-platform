package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// The main reconciler (S3 must 3, CUT §10, §10.6). An operation at main
// (main_intent, or a main_result other than non_ff) holds its mission with no
// time limit (landingHolds); this is what moves it on, from what main shows.
// It reads no rule and checks no eligibility: a rule withdrawn or expired
// does not stop it (§2.3 reconcile).

// PushTimeout is how long one push to main may take before it is given up
// (S3's push must guarantee it, CUT §10.6 2A). MainFence is how long after a
// main_intent a missing R is taken to mean the push never landed: the
// timeout and a margin. Before it, absence proves nothing.
const (
	PushTimeout = 2 * time.Minute
	MainFence   = PushTimeout + 10*time.Minute
)

// MainEvidence is what main shows for an operation's R: main's head as
// fetched, and whether R is in its history. HasR false is a real negative
// (a complete history without R), never a read that failed.
type MainEvidence struct {
	Head string
	HasR bool
	// Validity is whether R is in effect at Head (S3 B-02): "effective",
	// "reverted" (RevertedBy is the reversal in effect) or "unknown"; empty
	// reads as unknown. Unknown is never taken as in effect.
	Validity, RevertedBy string
	// Unreadable: the read failed. Only a landed operation records it (its
	// earlier evidence no longer stands, review 662 B-04); for any other it
	// proves nothing and nothing is written.
	Unreadable bool
	// ReadAt is when the read began (ReconcileMain sets it, on the decision
	// clock, before calling the reader). Absence counts only from a read
	// that began past the fence: a read begun inside it and finished, or
	// written, after it proves nothing (review 658 B2). Zero: never past.
	ReadAt time.Time
}

// MainEvidenceFunc reads main for R, and R's validity from the root's task
// and accepted commit A. A failure is an error, never HasR false.
type MainEvidenceFunc func(ctx context.Context, repo, root, a, r string) (MainEvidence, error)

// MainCandidate is an operation the reconciler reads main for, with the
// state it was listed in: the locked write is refused if it moved since.
type MainCandidate struct {
	Op, Root, Phase, Outcome, R, M0, A string
	IntentAt                           time.Time
	// Late: it once had a main_intent and ended failed{bound} as never sent;
	// main is read for a late landing (§10.6 2A).
	Late bool
}

// ReconcileDenied ends every main_result{denied} failed{denied}, from the
// stream alone (no Git). It returns the operations it ended.
func ReconcileDenied(ctx context.Context, db DB, repo string) ([]string, error) {
	stream := "landing:" + repo
	var ended []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		ended = nil
		if repo != "bifrost-platform" {
			// Recovery may outlive its rule, but must never create an empty
			// stream just because an unsigned repository was configured.
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id=$1)`, stream).Scan(&exists); err != nil || !exists {
				return err
			}
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		for _, op := range latestOps(s) {
			o := s.Operations[op]
			if o.Phase != "main_result" || o.Outcome != "denied" {
				continue
			}
			if err := appendMainRecord(ctx, tx, stream, o.Task, map[string]any{"kind": "failed", "op": op, "why": "denied"}); err != nil {
				return err
			}
			// The repository stops in the same transaction (B6): every path
			// that ends a push denied passes here.
			if err := stopRepo(ctx, tx, stream, repo, op, o.Task); err != nil {
				return err
			}
			ended = append(ended, op)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ended, nil
}

// MainCandidates lists what the reconciler reads main for: each root's latest
// operation at main other than denied, and each operation that once had a
// main_intent and ended failed{bound}, with no late-landing record yet.
func MainCandidates(ctx context.Context, db DB, repo string) ([]MainCandidate, error) {
	stream := "landing:" + repo
	var out []MainCandidate
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		intents, err := mainIntents(ctx, tx, stream)
		if err != nil {
			return err
		}
		for _, op := range latestOps(s) {
			o := s.Operations[op]
			if o.AtMain() && !(o.Phase == "main_result" && o.Outcome == "denied") {
				out = append(out, MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A, IntentAt: intents[op]})
			}
		}
		// Landed and not reverted, its mission still open, or closed less
		// than a day ago (CUT §11.8 v3 item 2: a reversal right after the
		// close is still seen): read again for a reversal (S3 B-02, ⑪).
		for _, op := range latestOps(s) {
			o := s.Operations[op]
			if o.Phase != "landed" || o.Reverted {
				continue
			}
			var open bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.task t JOIN agentrt.mission m USING (mission_id)
			  WHERE t.task_id = $1 AND (m.state IN ('active','paused','awaiting_owner','proposed')
			    OR m.state = 'done' AND EXISTS (SELECT 1 FROM agentrt.event e WHERE e.mission_id = m.mission_id
			      AND e.event_type = 'mission_state_changed' AND e.body_json->>'to' = 'done' AND e.at > $2)))`, o.Task, DecisionAt().Add(-LateRevertWithin)).Scan(&open); err != nil {
				return err
			}
			if open {
				out = append(out, MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A, IntentAt: intents[op]})
			}
		}
		for op, at := range intents {
			// Ended as never sent (failed{bound}), or then declined by the
			// Owner: a decision about it, not proof main cannot get R
			// (review 658 R2).
			o, ok := s.Operations[op]
			if !ok || o.Phase != "failed" || (o.Outcome != "bound" && o.Outcome != "declined") {
				continue
			}
			seen, err := hasAnomaly(ctx, tx, stream, op, "late_landing")
			if err != nil {
				return err
			}
			if !seen {
				out = append(out, MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A, IntentAt: at, Late: true})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Op < out[j].Op })
		return nil
	})
	return out, err
}

// ReconcileMainOne writes what main's evidence, read with no lock held,
// says about one candidate (§10.2, §10.6). Under the stream lock it checks
// the operation is still as listed; if it moved, the evidence is stale and
// nothing is written. It returns what it did ("" for nothing).
//
//	main_intent, R in main          → main_result{unknown}, landed
//	main_intent, R absent, fenced   → main_result{unknown}, failed{bound} (head = M0)
//	                                  main_result{non_ff} (head ≠ M0)
//	main_result{unknown}            → the same without the first record
//	main_result{ok}, R in main      → landed
//	main_result{ok}, R absent       → one anomaly record (ok_missing); the Owner
//	                                  confirms failed{main_lost} on a card
//	failed{bound} after an intent,
//	R in main                       → one anomaly record (late_landing)
//
// Absence before the fence (MainFence after the main_intent) writes nothing:
// a push may still be landing.
func ReconcileMainOne(ctx context.Context, db DB, repo string, c MainCandidate, ev MainEvidence) (string, error) {
	return reconcileMainOne(ctx, db, "", repo, c, ev)
}

// ReconcileStgMainOne captures B5-e's member proof under the same none guard
// as its release records. Ordinary B4 reconciliation remains independent.
func ReconcileStgMainOne(ctx context.Context, db DB, tenant, repo string, c MainCandidate, ev MainEvidence) (string, error) {
	return reconcileMainOne(ctx, db, tenant, repo, c, ev)
}
func reconcileMainOne(ctx context.Context, db DB, tenant, repo string, c MainCandidate, ev MainEvidence) (string, error) {
	stream := "landing:" + repo
	did := ""
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = ""
		if tenant != "" {
			enabled, err := stgControlEnabled(ctx, tx, tenant, func() bool { return true })
			if err != nil || !enabled {
				return err
			}
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[c.Op]
		if !ok || o.Task != c.Root || o.Phase != c.Phase || o.Outcome != c.Outcome || o.R != c.R || o.M0 != c.M0 ||
			(!c.Late && s.LatestOperation(c.Root) != c.Op) {
			return nil // moved since it was listed: the evidence is stale
		}
		// Absence counts only from a read begun past the fence (2A, review
		// 658 B2); the decision clock is past it too, read under the lock.
		fence := c.IntentAt.Add(MainFence)
		fenced := !c.IntentAt.IsZero() && !ev.ReadAt.IsZero() && !ev.ReadAt.Before(fence) && !DecisionAt().Before(fence)
		write := func(records ...map[string]any) error {
			for _, r := range records {
				if err := appendMainRecord(ctx, tx, stream, c.Root, r); err != nil {
					return err
				}
			}
			// What the outcome rests on, in the same transaction: the head
			// read, when the read began (review 658 R4).
			return appendMainEvidence(ctx, tx, stream, c, ev, did)
		}
		unknown := map[string]any{"kind": "main_result", "op": c.Op, "outcome": "unknown"}
		if ev.Unreadable && c.Phase != "landed" {
			return nil // a failed read proves nothing about an unsettled push
		}
		switch {
		case c.Phase == "landed":
			// Only a read begun after every read already recorded counts,
			// a reversal included (review 663 B-01): an older read committed
			// late moves nothing. Every newer read is recorded, so the last
			// record is always the newest read (no deduplication: a newer
			// read with the same answer must still raise the mark).
			prev, found, err := latestMainEvidence(ctx, tx, stream, c.Op)
			if err != nil {
				return err
			}
			if found && !ev.ReadAt.After(prev.readAt) {
				return nil
			}
			// A reversal of a landing already recorded (S3 B-02).
			if !ev.Unreadable && ev.HasR && validityOf(ev) == "reverted" {
				did = "reverted"
				return write(map[string]any{"kind": "reverted", "op": c.Op, "R": c.R, "by": ev.RevertedBy})
			}
			// Otherwise what this read says (review 662 B-04): effective,
			// unknown, R gone from main, or unreadable.
			did = "observed"
			return appendMainEvidence(ctx, tx, stream, c, ev, did)
		case c.Late:
			if !ev.HasR {
				return nil
			}
			// Under the stream lock: two passes with the same candidate
			// record it once (review 658 R1).
			seen, err := hasAnomaly(ctx, tx, stream, c.Op, "late_landing")
			if err != nil || seen {
				return err
			}
			did = "late_landing"
			return anomaly(ctx, tx, stream, c, ev, "late_landing")
		case c.Phase == "main_result" && c.Outcome == "ok":
			if ev.HasR {
				did = "landed"
				return write(landedRecords(c, ev)...)
			}
			seen, err := hasAnomaly(ctx, tx, stream, c.Op, "ok_missing")
			if err != nil || seen {
				return err
			}
			did = "ok_missing"
			return anomaly(ctx, tx, stream, c, ev, "ok_missing")
		case c.Phase == "main_intent" || c.Phase == "main_result" && c.Outcome == "unknown":
			var first []map[string]any
			if c.Phase == "main_intent" {
				first = append(first, unknown)
			}
			switch {
			case ev.HasR:
				did = "landed"
				return write(append(first, landedRecords(c, ev)...)...)
			case !fenced:
				return nil
			case ev.Head == c.M0:
				did = "unsent"
				return write(append(first, map[string]any{"kind": "failed", "op": c.Op, "why": "bound"})...)
			default:
				did = "non_ff"
				return write(map[string]any{"kind": "main_result", "op": c.Op, "outcome": "non_ff"})
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return did, nil
}

// ReconcileMain is one pass: denied first (no Git), then each candidate's
// evidence read with no transaction open, then its locked write. A read that
// fails skips that operation only; the errors are joined. It never reads a
// rule.
func ReconcileMain(ctx context.Context, db DB, repo string, read MainEvidenceFunc) (map[string]string, error) {
	done := map[string]string{}
	denied, err := ReconcileDenied(ctx, db, repo)
	if err != nil {
		return done, err
	}
	for _, op := range denied {
		done[op] = "denied"
	}
	if read == nil {
		return done, nil
	}
	cands, err := MainCandidates(ctx, db, repo)
	if err != nil {
		return done, err
	}
	var errs []error
	for _, c := range cands {
		started := DecisionAt()
		ev, err := read(ctx, repo, c.Root, c.A, c.R)
		ev.ReadAt = started
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Op, err))
			if c.Phase != "landed" {
				continue
			}
			// A landing's earlier evidence no longer stands (review 662 B-04).
			ev = MainEvidence{Unreadable: true, ReadAt: started}
		}
		did, err := ReconcileMainOne(ctx, db, repo, c, ev)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Op, err))
			continue
		}
		if did != "" {
			done[c.Op] = did
		}
	}
	return done, errors.Join(errs...)
}

func latestOps(s landing.State) []string {
	roots := map[string]bool{}
	for _, o := range s.Operations {
		if o.Task != "" {
			roots[o.Task] = true
		}
	}
	var ops []string
	for root := range roots {
		if op := s.LatestOperation(root); op != "" {
			ops = append(ops, op)
		}
	}
	sort.Strings(ops)
	return ops
}

// mainIntents is when each operation's main_intent was written.
func mainIntents(ctx context.Context, tx pgx.Tx, stream string) (map[string]time.Time, error) {
	rows, err := tx.Query(ctx, `SELECT body_json->'landing'->>'op', min(at) FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing'->>'kind' = 'main_intent'
	  GROUP BY 1`, stream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var op string
		var at time.Time
		if err := rows.Scan(&op, &at); err != nil {
			return nil, err
		}
		out[op] = at
	}
	return out, rows.Err()
}

func appendMainRecord(ctx context.Context, tx pgx.Tx, stream, root string, r map[string]any) error {
	r["v"] = landing.Version
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": fmt.Sprintf("Main reconciled: %v %v", r["kind"], r["outcome"]), "landing": r}})
	return err
}

func hasAnomaly(ctx context.Context, tx pgx.Tx, stream, op, kind string) (bool, error) {
	var seen bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_anomaly'->>'op' = $2 AND body_json->'landing_anomaly'->>'kind' = $3)`, stream, op, kind).Scan(&seen)
	return seen, err
}

// anomaly records, once per operation and kind, what only the Owner can
// settle: a push reported ok whose R main does not show (ok_missing), or an
// operation ended as never sent whose R main now shows (late_landing).
func anomaly(ctx context.Context, tx pgx.Tx, stream string, c MainCandidate, ev MainEvidence, kind string) error {
	text := map[string]string{
		"ok_missing":   "Main reconciled: the push was reported ok, but main does not have R; the Owner confirms whether it is lost",
		"late_landing": "Main reconciled: an operation ended as never sent, but main now has R; the Owner looks",
	}[kind]
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: c.Root,
		Body: map[string]any{"text": text, "landing_anomaly": map[string]any{"op": c.Op, "root": c.Root, "kind": kind, "R": c.R, "M0": c.M0, "head": ev.Head}}})
	return err
}

// badLandingAnomaly is why an anomaly body is refused, or "".
func badLandingAnomaly(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_anomaly is not an object"
	}
	for _, k := range []string{"op", "root", "kind", "R"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_anomaly needs " + k
		}
	}
	for _, k := range []string{"R", "M0", "head"} {
		if v, _ := m[k].(string); !landingSHA.MatchString(v) {
			return "landing_anomaly " + k + " is not a commit"
		}
	}
	if !landingRoot.MatchString(m["root"].(string)) {
		return "landing_anomaly root is not a task"
	}
	if m["kind"] != "ok_missing" && m["kind"] != "late_landing" {
		return "landing_anomaly kind is not ok_missing or late_landing"
	}
	return ""
}

// checkLandingAnomalyAppend: only the controller records an anomaly, on the
// repository stream, under the root it names.
func checkLandingAnomalyAppend(ev Event) error {
	v, exists := ev.Body["landing_anomaly"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID == "" || ev.TaskID != m["root"] {
		return fmt.Errorf("landing_anomaly is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_anomaly is not a landing record")
	}
	return nil
}

var landingSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var landingRoot = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)

// badLandingMain is why an evidence body is refused, or "".
func badLandingMain(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_main is not an object"
	}
	for _, k := range []string{"op", "root", "verdict", "read_at"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_main needs " + k
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, m["read_at"].(string)); err != nil {
		return "landing_main read_at is not a time"
	}
	hasR, ok := m["has_r"].(bool)
	if !ok {
		return "landing_main needs has_r"
	}
	version, versioned := m["v"]
	if !versioned {
		// Written before versions: as it was checked then, validity typed.
		for _, k := range []string{"R", "M0", "head"} {
			if v, _ := m[k].(string); !landingSHA.MatchString(v) {
				return "landing_main " + k + " is not a commit"
			}
		}
		if !slices.Contains([]string{"landed", "unsent", "non_ff", "reverted"}, m["verdict"].(string)) {
			return "landing_main verdict is not landed, unsent, non_ff or reverted"
		}
		if v, ok := m["validity"]; ok {
			if s, isString := v.(string); !isString || !slices.Contains([]string{"effective", "reverted", "unknown"}, s) {
				return "landing_main validity is not effective, reverted or unknown"
			}
		}
		return ""
	}
	if version != MainEvidenceVersion {
		return "landing_main version is not 2"
	}
	if !slices.Contains([]string{"landed", "unsent", "non_ff", "reverted", "observed"}, m["verdict"].(string)) {
		return "landing_main verdict is not landed, unsent, non_ff, reverted or observed"
	}
	for _, k := range []string{"R", "M0"} {
		if v, _ := m[k].(string); !landingSHA.MatchString(v) {
			return "landing_main " + k + " is not a commit"
		}
	}
	validity, _ := m["validity"].(string)
	head, headOK := m["head"].(string)
	by, byOK := m["reverted_by"].(string)
	if !slices.Contains(mainValidities, validity) || !headOK || !byOK {
		return "landing_main needs validity, head and reverted_by"
	}
	verdict := m["verdict"].(string)
	if validity == "unreadable" {
		if head != "" || hasR || by != "" || verdict != "observed" {
			return "landing_main unreadable is an observation with no head, R or reversal"
		}
		return ""
	}
	if !landingSHA.MatchString(head) {
		return "landing_main head is not a commit"
	}
	switch {
	case (validity == "absent") == hasR:
		return "landing_main absent is exactly has_r false"
	case validity == "reverted" && !landingSHA.MatchString(by), validity != "reverted" && by != "":
		return "landing_main reverted names its reversal, and only then"
	case (verdict == "unsent" || verdict == "non_ff") && validity != "absent":
		return "landing_main unsent and non_ff rest on R absent"
	case verdict == "landed" && !hasR, verdict == "reverted" && validity != "reverted":
		return "landing_main landed rests on R in main, reverted on its reversal"
	}
	return ""
}

// checkLandingMainAppend: only the controller records evidence, on the
// repository stream, under the root it names, never as a landing record.
func checkLandingMainAppend(ev Event) error {
	v, exists := ev.Body["landing_main"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID == "" || ev.TaskID != m["root"] {
		return fmt.Errorf("landing_main is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_main is not a landing record")
	}
	return nil
}

// MainEvidenceVersion is the version of the landing_main records written
// now (review 662 N-01): every field present and typed, validity one of
// mainValidities, consistent with has_r and reverted_by. Records without a
// version (written before) are read as they were.
const MainEvidenceVersion = "2"

var mainValidities = []string{"effective", "reverted", "unknown", "absent", "unreadable"}

// mainValidity is what a read says about R at main's head: absent (a full
// history without R), unreadable (the read failed), else validityOf.
func mainValidity(ev MainEvidence) string {
	switch {
	case ev.Unreadable:
		return "unreadable"
	case !ev.HasR:
		return "absent"
	}
	return validityOf(ev)
}

func revertedBy(ev MainEvidence) string {
	if mainValidity(ev) == "reverted" {
		return ev.RevertedBy
	}
	return ""
}

// appendMainEvidence records what an outcome or an observation rests on:
// the head read, whether R is in it and in effect, when the read began.
func appendMainEvidence(ctx context.Context, tx pgx.Tx, stream string, c MainCandidate, ev MainEvidence, verdict string) error {
	head := ev.Head
	if ev.Unreadable {
		head = ""
	}
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: c.Root,
		Body: map[string]any{"text": "Main reconciled: evidence (" + verdict + ")", "landing_main": map[string]any{
			"v": MainEvidenceVersion, "op": c.Op, "root": c.Root, "verdict": verdict, "R": c.R, "M0": c.M0, "head": head,
			"has_r": ev.HasR && !ev.Unreadable, "validity": mainValidity(ev), "reverted_by": revertedBy(ev),
			"read_at": ev.ReadAt.UTC().Format(time.RFC3339Nano)}}})
	return err
}

type mainObservation struct {
	validity, revertedBy string
	readAt               time.Time
}

// latestMainEvidence is the operation's last recorded evidence. A record
// written before versions has no absent/unreadable: its has_r false reads
// as absent.
func latestMainEvidence(ctx context.Context, tx pgx.Tx, stream, op string) (mainObservation, bool, error) {
	var validity, by, readAt string
	var hasR bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(body_json->'landing_main'->>'validity',''), COALESCE(body_json->'landing_main'->>'reverted_by',''),
	  body_json->'landing_main'->>'read_at', COALESCE((body_json->'landing_main'->>'has_r')::boolean, false)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing_main'->>'op' = $2
	  ORDER BY event_id DESC LIMIT 1`, stream, op).Scan(&validity, &by, &readAt, &hasR)
	if errors.Is(err, pgx.ErrNoRows) {
		return mainObservation{}, false, nil
	}
	if err != nil {
		return mainObservation{}, false, err
	}
	at, err := time.Parse(time.RFC3339Nano, readAt)
	if err != nil {
		return mainObservation{}, false, err
	}
	switch {
	case !hasR && validity != "unreadable":
		validity = "absent"
	case validity == "":
		validity = "unknown"
	}
	if validity != "reverted" {
		by = ""
	}
	return mainObservation{validity: validity, revertedBy: by, readAt: at}, true, nil
}

// validityOf is the evidence's validity, unknown unless it says otherwise
// with what it rests on.
func validityOf(ev MainEvidence) string {
	switch {
	case ev.Validity == "effective":
		return "effective"
	case ev.Validity == "reverted" && landingSHA.MatchString(ev.RevertedBy):
		return "reverted"
	}
	return "unknown"
}

// landedRecords are what a landing whose R main has writes: landed, and
// reverted after it when the reversal is in effect already (S3 B-02, ⑪:
// both facts, in this order, in one transaction). Unknown validity records
// landed only; it is not delivery (B4, B5 read validity when they decide).
func landedRecords(c MainCandidate, ev MainEvidence) []map[string]any {
	out := []map[string]any{{"kind": "landed", "op": c.Op, "R": c.R}}
	if validityOf(ev) == "reverted" {
		out = append(out, map[string]any{"kind": "reverted", "op": c.Op, "R": c.R, "by": ev.RevertedBy})
	}
	return out
}
