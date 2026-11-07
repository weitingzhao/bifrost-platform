package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Batch landing (R5:5.7, goal 2; landing/batch.go): the batch is a
// controller note on the repository stream beside the protocol, like
// landing_rework_seen: the fold never reads it, and each member stays an
// ordinary operation for every other reader. landing_batch names the
// members in order (the first leads), M0, R and the committer date they were
// replayed with, and after, the red batch this one halves. landing_batch_red
// records how a red batch ended: the members its failure blamed, and when
// it blamed none the halves that succeed it, first half first.

// LandingBatch is a landing_batch body, with the seq it was recorded at.
type LandingBatch struct {
	Batch, Lead   string
	Members       []string
	M0, R         string
	CommitterDate time.Time
	After         string
	Seq           int64
}

// LandingBatchRed is a landing_batch_red body.
type LandingBatchRed struct {
	Batch  string
	Blamed []string
	Halves [][]string
	Seq    int64
}

// ErrBatchRefused: the stream does not take the batch record now; nothing
// was written.
var ErrBatchRefused = errors.New("landing batch record refused")

var batchFields = []string{"v", "batch", "lead", "members", "M0", "R", "committer_date", "after"}
var batchRedFields = []string{"v", "batch", "blamed", "halves"}

func (b LandingBatch) body() map[string]any {
	return map[string]any{"v": "1", "batch": b.Batch, "lead": b.Lead, "members": anyStrings(b.Members), "M0": b.M0, "R": b.R,
		"committer_date": b.CommitterDate.UTC().Format(time.RFC3339Nano), "after": b.After}
}

func (r LandingBatchRed) body() map[string]any {
	halves := []any{}
	for _, h := range r.Halves {
		halves = append(halves, anyStrings(h))
	}
	return map[string]any{"v": "1", "batch": r.Batch, "blamed": anyStrings(r.Blamed), "halves": halves}
}

func anyStrings(s []string) []any {
	out := make([]any, 0, len(s))
	for _, x := range s {
		out = append(out, x)
	}
	return out
}

// stringList reads a list of strings, or false when v is not one.
func stringList(v any) ([]string, bool) {
	switch l := v.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, 0, len(l))
		for _, x := range l {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func landingBatchOf(m map[string]any) LandingBatch {
	s := func(k string) string { v, _ := m[k].(string); return v }
	b := LandingBatch{Batch: s("batch"), Lead: s("lead"), M0: s("M0"), R: s("R"), After: s("after")}
	b.Members, _ = stringList(m["members"])
	b.CommitterDate, _ = time.Parse(time.RFC3339Nano, s("committer_date"))
	return b
}

func landingBatchRedOf(m map[string]any) LandingBatchRed {
	r := LandingBatchRed{}
	r.Batch, _ = m["batch"].(string)
	r.Blamed, _ = stringList(m["blamed"])
	halves, _ := m["halves"].([]any)
	for _, h := range halves {
		l, _ := stringList(h)
		r.Halves = append(r.Halves, l)
	}
	return r
}

// badLandingBatch is why a landing_batch body is refused, or "".
func badLandingBatch(v any) string {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, batchFields) || m["v"] != "1" {
		return "landing_batch is a v1 object of " + strings.Join(batchFields, ", ")
	}
	b := landingBatchOf(m)
	list, isList := stringList(m["members"])
	_, after := m["after"].(string)
	switch {
	case !isList || len(list) < 2 || len(list) > landing.BatchMax:
		return fmt.Sprintf("landing_batch members are 2 to %d operations", landing.BatchMax)
	case slices.Contains(list, "") || len(slices.Compact(slices.Sorted(slices.Values(list)))) != len(list):
		return "landing_batch members are operations, each once"
	case b.Lead != list[0] || b.Batch != landing.BatchID(b.Lead):
		return "landing_batch is named for its lead, its first member"
	case !landingSHA.MatchString(b.M0) || !landingSHA.MatchString(b.R):
		return "landing_batch M0 and R are commits"
	case b.CommitterDate.IsZero():
		return "landing_batch committer_date is a time"
	case !after:
		return "landing_batch after is a batch or empty"
	}
	return ""
}

// badLandingBatchRed is why a landing_batch_red body is refused, or "".
func badLandingBatchRed(v any) string {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, batchRedFields) || m["v"] != "1" {
		return "landing_batch_red is a v1 object of " + strings.Join(batchRedFields, ", ")
	}
	if b, _ := m["batch"].(string); b == "" {
		return "landing_batch_red needs batch"
	}
	if _, ok := stringList(m["blamed"]); !ok {
		return "landing_batch_red blamed is a list of operations"
	}
	halves, ok := m["halves"].([]any)
	if !ok {
		return "landing_batch_red halves is a list"
	}
	for _, h := range halves {
		if _, ok := stringList(h); !ok {
			return "landing_batch_red halves are lists of operations"
		}
	}
	return ""
}

// checkLandingBatchAppend checks both batch records when written, under the
// stream lock Append holds. landing_batch: the controller on the platform
// stream, the body its text and this record alone; each member is its
// root's latest operation, claimed, or prepared with this M0 and R, one per
// root, and in no other batch; after, when set, names a red batch that was
// split. landing_batch_red: a batch of the stream not yet red, whose
// members are still each root's latest operation, not ended; blamed are
// members, and with none blamed the halves are Split's of the members.
func checkLandingBatchAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, isBatch := ev.Body["landing_batch"]
	red, isRed := ev.Body["landing_batch_red"]
	if !isBatch && !isRed {
		return nil
	}
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrBatchRefused, why) }
	name := "landing_batch"
	if isRed {
		name = "landing_batch_red"
	}
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != landingRepoStream || !onlyBusinessKey(ev.Body, name) {
		return refuse("it is the controller's note on the platform stream, its text and this record alone")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	batches, reds, err := readLandingBatches(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	if isRed {
		r := landingBatchRedOf(red.(map[string]any))
		b, ok := findBatch(batches, r.Batch)
		switch {
		case !ok:
			return refuse("no such batch")
		case slices.ContainsFunc(reds, func(x LandingBatchRed) bool { return x.Batch == r.Batch }):
			return refuse("the batch is red already")
		case b.Broken(s) || slices.ContainsFunc(b.Members, func(op string) bool { return s.Operations[op].Terminal() }):
			return refuse("a member is ended or no longer its root's latest operation")
		case slices.ContainsFunc(r.Blamed, func(op string) bool { return !slices.Contains(b.Members, op) }):
			return refuse("blamed are members")
		}
		want := [][]string(nil)
		if len(r.Blamed) == 0 {
			first, second := landing.Split(b.Members)
			want = [][]string{first, second}
		}
		if len(want) != len(r.Halves) || !slices.EqualFunc(want, r.Halves, slices.Equal[[]string]) {
			return refuse("a batch blaming none is succeeded by its halves, first half first; one blaming any by none")
		}
		return nil
	}
	b := landingBatchOf(v.(map[string]any))
	if _, ok := findBatch(batches, b.Batch); ok {
		return refuse("the batch is recorded already")
	}
	roots := map[string]bool{}
	for _, op := range b.Members {
		o, ok := s.Operations[op]
		switch {
		case !ok || o.Task == "" || s.LatestOperation(o.Task) != op:
			return refuse(op + " is not its root's latest operation")
		case roots[o.Task]:
			return refuse("one member per root")
		case o.Phase != "claimed" && (o.Phase != "prepared" || o.M0 != b.M0 || o.R != b.R || o.DryRun):
			return refuse(op + " is neither claimed nor prepared with the batch's M0 and R")
		}
		roots[o.Task] = true
		for _, other := range batches {
			if slices.Contains(other.Members, op) {
				return refuse(op + " is in another batch")
			}
		}
	}
	if b.After != "" && !slices.ContainsFunc(reds, func(r LandingBatchRed) bool { return r.Batch == b.After && len(r.Halves) > 0 }) {
		return refuse("after names no split red batch")
	}
	return nil
}

// Broken: the batch can no longer reach main as it is: a member is not its
// root's latest operation any more, or ended without landing.
func (b LandingBatch) Broken(s landing.State) bool {
	for _, op := range b.Members {
		o, ok := s.Operations[op]
		if !ok || s.LatestOperation(o.Task) != op || o.Terminal() && !o.EverLanded {
			return true
		}
	}
	return false
}

// Open: some member has not ended.
func (b LandingBatch) Open(s landing.State) bool {
	return slices.ContainsFunc(b.Members, func(op string) bool { o, ok := s.Operations[op]; return ok && !o.Terminal() })
}

func findBatch(batches []LandingBatch, id string) (LandingBatch, bool) {
	for _, b := range batches {
		if b.Batch == id {
			return b, true
		}
	}
	return LandingBatch{}, false
}

// readLandingBatches reads the stream's batches and red batches, in order.
func readLandingBatches(ctx context.Context, tx pgx.Tx, stream string) ([]LandingBatch, []LandingBatchRed, error) {
	rows, err := tx.Query(ctx, `SELECT seq, body_json->'landing_batch', body_json->'landing_batch_red' FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND (body_json ? 'landing_batch' OR body_json ? 'landing_batch_red') ORDER BY seq`, stream)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var batches []LandingBatch
	var reds []LandingBatchRed
	for rows.Next() {
		var seq int64
		var rawB, rawR []byte
		if err := rows.Scan(&seq, &rawB, &rawR); err != nil {
			return nil, nil, err
		}
		var b, r map[string]any
		if len(rawB) > 0 {
			if err := json.Unmarshal(rawB, &b); err != nil {
				return nil, nil, err
			}
		}
		if len(rawR) > 0 {
			if err := json.Unmarshal(rawR, &r); err != nil {
				return nil, nil, err
			}
		}
		if b != nil {
			x := landingBatchOf(b)
			x.Seq = seq
			batches = append(batches, x)
		}
		if r != nil {
			x := landingBatchRedOf(r)
			x.Seq = seq
			reds = append(reds, x)
		}
	}
	return batches, reds, rows.Err()
}

// landingBatchMates are the other members of op's batch, when it is in one.
func landingBatchMates(ctx context.Context, tx pgx.Tx, stream, op string) (map[string]bool, error) {
	if op == "" {
		return nil, nil
	}
	var members []string
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_batch'->'members' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json ? 'landing_batch' AND body_json->'landing_batch'->'members' ? $2 ORDER BY seq LIMIT 1`, stream, op).Scan(&members)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mates := map[string]bool{}
	for _, m := range members {
		if m != op {
			mates[m] = true
		}
	}
	return mates, nil
}

// LandingBatchOf is the batch of repo's operation op, when it is in one.
func LandingBatchOf(ctx context.Context, db DB, repo, op string) (LandingBatch, bool, error) {
	var b LandingBatch
	var found bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		batches, _, err := readLandingBatches(ctx, tx, "landing:"+repo)
		for _, x := range batches {
			if slices.Contains(x.Members, op) {
				b, found = x, true
			}
		}
		return err
	})
	return b, found, err
}

// OpenLandingBatches are repo's batches some member of which has not ended.
func OpenLandingBatches(ctx context.Context, db DB, repo string) ([]LandingBatch, error) {
	var out []LandingBatch
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		s, err := LandingState(ctx, tx, "landing:"+repo)
		if err != nil {
			return err
		}
		batches, _, err := readLandingBatches(ctx, tx, "landing:"+repo)
		for _, b := range batches {
			if b.Open(s) {
				out = append(out, b)
			}
		}
		return err
	})
	return out, err
}

// AppendLandingBatch records b on repo's stream (checkLandingBatchAppend).
func AppendLandingBatch(ctx context.Context, db DB, repo string, b LandingBatch) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: "landing:" + repo, Type: "note", Actor: "controller",
			Body: map[string]any{"text": fmt.Sprintf("Landing batch of %d led by %s: one landing CI on R %s", len(b.Members), b.Lead, b.R), "landing_batch": b.body()}})
		return err
	})
}

// RecordBatchRed ends repo's red batch in one transaction: each blamed
// member through RecordLandingCIRed's rework (failure attached; a root that
// went back gets no card, one that could not go back fails as before); each
// other member failed{ci_red} and then the controller's retry naming the
// batch, so its next claim is free and no card is made. A batch that blamed
// none records that its halves succeed it, first half first. text starts
// the notes' text. It returns the members whose roots went back.
//
// Lock order: every blamed member's step, task and mission stream first
// (reworkRedLanding), then the repository's stream.
func RecordBatchRed(ctx context.Context, db DB, repo, batch string, failure landing.CIFailure, blamed []string, text string, p Precheck) ([]string, error) {
	stream := "landing:" + repo
	var reworked []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		reworked = nil
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		batches, reds, err := readLandingBatches(ctx, tx, stream)
		if err != nil {
			return err
		}
		b, ok := findBatch(batches, batch)
		if !ok {
			return fmt.Errorf("unknown landing batch %s", batch)
		}
		if slices.ContainsFunc(reds, func(r LandingBatchRed) bool { return r.Batch == batch }) {
			return nil
		}
		red := LandingBatchRed{Batch: batch, Blamed: []string{}}
		for _, op := range b.Members {
			if slices.Contains(blamed, op) {
				red.Blamed = append(red.Blamed, op)
			}
		}
		if len(red.Blamed) == 0 {
			first, second := landing.Split(b.Members)
			red.Halves = [][]string{first, second}
		}
		for _, op := range red.Blamed {
			o := s.Operations[op]
			if p.reviews() || s.LatestOperation(o.Task) != op || o.Terminal() {
				continue
			}
			went, err := reworkRedLanding(ctx, tx, o, op, ciRed(op, failure))
			if err != nil {
				return err
			}
			if went {
				reworked = append(reworked, op)
			}
		}
		why := fmt.Sprintf("its landing CI %s was red and blamed %d of %d members", failure.Run, len(red.Blamed), len(b.Members))
		if len(red.Blamed) == 0 {
			why = fmt.Sprintf("its landing CI %s was red and named no member: its halves of %d and %d are landed next", failure.Run, len(red.Halves[0]), len(red.Halves[1]))
		}
		if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": "Landing batch " + batch + " red: " + why, "landing_batch_red": red.body()}}); err != nil {
			return err
		}
		for _, op := range b.Members {
			o := s.Operations[op]
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task,
				Body: map[string]any{"text": text, "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "ci_red"}}}); err != nil {
				return err
			}
			if slices.Contains(red.Blamed, op) {
				continue
			}
			if err = appendBatchRetry(ctx, tx, stream, op, o.Task, "batch "+batch+" red; "+op+" not blamed: it lands in a later batch"); err != nil {
				return err
			}
		}
		return nil
	})
	return reworked, err
}

// appendBatchRetry is the controller's retry of root's failed op.
func appendBatchRetry(ctx context.Context, tx pgx.Tx, stream, op, root, reason string) error {
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": "Landing retried: " + reason,
			"landing": map[string]any{"v": landing.Version, "kind": "retry", "op": op, "root": root, "after_op": op, "reason": reason, "by": "controller"}}})
	return err
}

// ReleaseLandingBatch ends, in one transaction, every member of repo's batch
// that has not reached main and has not ended, so that each root's next
// claim joins a new batch: discarded{main_changed} when main moved (why
// main_changed) and the protocol takes it from the member's phase; else
// failed{stale} and the controller's retry naming the batch. A root out of
// operations (its eighth) ends failed{bound}. It returns how many ended.
func ReleaseLandingBatch(ctx context.Context, db DB, repo, batch, why, text string) (int, error) {
	stream := "landing:" + repo
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		batches, _, err := readLandingBatches(ctx, tx, stream)
		if err != nil {
			return err
		}
		b, ok := findBatch(batches, batch)
		if !ok {
			return fmt.Errorf("unknown landing batch %s", batch)
		}
		for _, op := range b.Members {
			o, ok := s.Operations[op]
			if !ok || !o.EndsAtBound() || s.LatestOperation(o.Task) != op {
				continue
			}
			put := func(kind string, fields map[string]any) error {
				fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
				_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, Body: map[string]any{"text": text + kind, "landing": fields}})
				return err
			}
			_, count := landing.RootBudget(s, o.Task)
			switch {
			case count >= 8:
				err = put("failed", map[string]any{"why": "bound"})
			case why == "main_changed" && slices.Contains([]string{"branch_intent", "branch_pushed", "parked", "awaiting_grant"}, o.Phase):
				err = put("discarded", map[string]any{"why": "main_changed"})
			default:
				if err = put("failed", map[string]any{"why": "stale"}); err == nil {
					err = appendBatchRetry(ctx, tx, stream, op, o.Task, "batch "+batch+" released ("+why+"): "+op+" lands in a later batch")
				}
			}
			if err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// PendingBatchHalf is the half of a split red batch to land next on repo,
// the most recent split first (a half that splits again is landed before
// its sibling half): the roots of that half whose latest operation is still
// the red batch's, retried and waiting for its next claim, and the red
// batch's id, which the half's batch names as after. Nil when no half waits.
func PendingBatchHalf(ctx context.Context, db DB, repo string) ([]string, string, error) {
	var roots []string
	var after string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		roots, after = nil, ""
		s, err := LandingState(ctx, tx, "landing:"+repo)
		if err != nil {
			return err
		}
		_, reds, err := readLandingBatches(ctx, tx, "landing:"+repo)
		if err != nil {
			return err
		}
		for i := len(reds) - 1; i >= 0; i-- {
			for _, half := range reds[i].Halves {
				for _, op := range half {
					o, ok := s.Operations[op]
					if ok && s.LatestOperation(o.Task) == op && o.Phase == "failed" && o.Retried {
						roots = append(roots, o.Task)
					}
				}
				if len(roots) > 0 {
					after = reds[i].Batch
					return nil
				}
			}
		}
		return nil
	})
	return roots, after, err
}

// GrantBatch names the batch a waiting lead's change belongs to: every
// member's operation, root and accepted result, in batch order.
type GrantBatch struct {
	Batch   string             `json:"batch"`
	Members []GrantBatchMember `json:"members"`
}

// GrantBatchMember is one member of a GrantBatch.
type GrantBatchMember struct {
	Op   string `json:"op"`
	Task string `json:"task"`
	A    string `json:"A"`
	// Soft: on the lead's material, the member's own soft checks.
	Soft []string `json:"soft,omitempty"`
}
