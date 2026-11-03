package store_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-19 §5: the folds an explanation reuses take no context, so their
// cost is bounded by the input bounds alone. This measures them at the row
// bound (DefaultExplainBounds): real records of a landed operation, cloned
// to that many rows of one stream, decoded as the read decodes them and
// folded by the landing fold and by B5's fold. At 300,000 rows each took
// over three minutes (the folds grow with the square of the operations). Acceptance: each fold at most 1.5 s, the peak heap of
// decode and fold at most 300 MB. Run by hand (it takes a while):
//
//	AGENTRT_EXPLAIN_MEASURE=1 go test ./internal/agentrt/store/ -run TestExplainFoldsAtTheBound -v
//
// Serial: FreezeEventClock.
func TestExplainFoldsAtTheBound(t *testing.T) {
	if os.Getenv("AGENTRT_EXPLAIN_MEASURE") != "1" {
		t.Skip("a measurement, run by hand: AGENTRT_EXPLAIN_MEASURE=1")
	}
	w, c := dbWorld(t)
	model := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, model, dbOldA, "landed")
	type row struct {
		seq int64
		at  time.Time
		raw []byte
	}
	rows, err := w.rt.Query(ctx, `SELECT seq, at, body_json::text FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' ORDER BY seq`, landingRepoStream)
	ok(t, err)
	var prefix, op []row
	for rows.Next() {
		var r row
		var s string
		ok(t, rows.Scan(&r.seq, &r.at, &s))
		r.raw = []byte(s)
		if bytes.Contains(r.raw, []byte(model)) {
			op = append(op, r)
		} else {
			prefix = append(prefix, r)
		}
	}
	ok(t, rows.Err())
	rows.Close()
	if len(op) < 6 {
		t.Fatalf("fixture: %d records of the model operation", len(op))
	}
	bound := int(store.DefaultExplainBounds().Rows)
	roots := (bound - len(prefix)) / len(op)
	// Each clone is the model with its own root id, in claim order; the
	// stream's times keep increasing.
	const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	id := func(i int) string {
		b := []byte("t_" + "00000000000000000000000000")
		for j := len(b) - 1; i > 0 && j >= 2; j-- {
			b[j] = crockford[i%32]
			i /= 32
		}
		return string(b)
	}
	type raw struct {
		seq int64
		at  time.Time
		doc []byte
	}
	var all []raw
	var total int64
	for _, r := range prefix {
		all = append(all, raw{r.seq, r.at, r.raw})
		total += int64(len(r.raw))
	}
	// Each clone keeps the model's own times: its records' times inside the
	// body (committer_date, a CI run's created_at) stay consistent with them.
	seq := prefix[len(prefix)-1].seq
	for i := 1; i <= roots; i++ {
		root := []byte(id(i))
		for _, r := range op {
			seq++
			doc := bytes.ReplaceAll(r.raw, []byte(model), root)
			all = append(all, raw{seq, r.at, doc})
			total += int64(len(doc))
		}
	}
	t.Logf("%d rows (%d operations of %d records, %d before), %.1f MB of body_json", len(all), roots, len(op), len(prefix), float64(total)/(1<<20))

	// The peak heap while fn runs, sampled every millisecond.
	measure := func(name string, fn func() error) {
		t.Helper()
		runtime.GC()
		var base runtime.MemStats
		runtime.ReadMemStats(&base)
		var peak atomic.Uint64
		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			var m runtime.MemStats
			for {
				select {
				case <-stop:
					return
				case <-time.After(time.Millisecond):
					runtime.ReadMemStats(&m)
					if m.HeapAlloc > peak.Load() {
						peak.Store(m.HeapAlloc)
					}
				}
			}
		}()
		start := time.Now()
		err := fn()
		took := time.Since(start)
		close(stop)
		wg.Wait()
		ok(t, err)
		grew := float64(peak.Load()) - float64(base.HeapAlloc)
		t.Logf("%s: %s, peak heap +%.0f MB over %.0f MB", name, took.Round(time.Millisecond), grew/(1<<20), float64(base.HeapAlloc)/(1<<20))
		if took > 1500*time.Millisecond {
			t.Errorf("%s took %s, over 1.5 s", name, took)
		}
		if grew > 300<<20 {
			t.Errorf("%s grew the heap %.0f MB, over 300 MB", name, grew/(1<<20))
		}
	}
	// As the reads run them, one after the other: the landing read decodes
	// the landing record of each row and folds it (LandingState); B5's read
	// decodes each whole body and folds it (StgReleaseState).
	measure("landing read and fold", func() error {
		entries := make([]landing.Entry, 0, len(all))
		for _, r := range all {
			v, err := canon.Parse(r.doc)
			if err != nil {
				return err
			}
			rec, err := landing.Parse(v.(map[string]any)["landing"])
			if err != nil {
				return fmt.Errorf("seq %d: %w", r.seq, err)
			}
			entries = append(entries, landing.Entry{At: r.at, Record: rec})
		}
		s, err := landing.FoldWithGrants(entries, nil)
		if err == nil && len(s.Operations) < roots {
			err = fmt.Errorf("folded %d operations, want %d", len(s.Operations), roots)
		}
		return err
	})
	measure("B5 read and fold", func() error {
		stg := make([]store.StgEntry, 0, len(all))
		for _, r := range all {
			v, err := canon.Parse(r.doc)
			if err != nil {
				return err
			}
			stg = append(stg, store.StgEntry{Seq: r.seq, At: r.at, Event: store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", Body: v.(map[string]any)}})
		}
		_, err := store.StgFold(stg)
		return err
	})
}

// R5:5.5-19 §5 and review rounds 11 M4, 12 M1: one whole explanation at
// scale, as the server runs it: 20 active steps sharing one repository,
// five roots each (three landed, two declined, each declined root's
// current set recorded: the dearest rework path), and the repository's
// landing stream filled close to the row bound with operations of roots no
// step explains, one transaction. It reports the time, the statements and
// the peak heap; the time must stay inside the 5 s deadline with room (at
// most 2.5 s) and the heap under 300 MB. Run by hand with the measurement
// above:
//
//	AGENTRT_EXPLAIN_MEASURE=1 go test ./internal/agentrt/store/ -run TestExplainWholeAtScale -v
//
// Serial: FreezeEventClock.
func TestExplainWholeAtScale(t *testing.T) {
	if os.Getenv("AGENTRT_EXPLAIN_MEASURE") != "1" {
		t.Skip("a measurement, run by hand: AGENTRT_EXPLAIN_MEASURE=1")
	}
	w, c := dbWorld(t)
	shas := func(i, j int) string { return fmt.Sprintf("%02x", (i*5+j)%250+1) + strings.Repeat("e", 38) }
	for i := 0; i < 20; i++ {
		step := fmt.Sprintf("R2:2.1-%d", 30+i)
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: step, Parent: item, Kind: "step", Label: fmt.Sprintf("2.1-%d", 30+i),
				Title: "scale", StepKind: "agent", Ordinal: 30 + i, Root: root})
			return err
		}))
		var sibs []store.ReworkSibling
		type declinedRoot struct{ root, op, a string }
		var declined []declinedRoot
		for j := 0; j < 5; j++ {
			r := dbAccepted(t, w, c, step, "bifrost-platform")
			end := "landed"
			if j >= 3 {
				end = "declined"
			}
			op := dbOp(t, w, c, r, shas(i, j), end)
			if end == "landed" {
				sibs = append(sibs, store.ReworkSibling{By: r, ByOp: op, NewA: shas(i, j), Why: "different"})
			} else {
				declined = append(declined, declinedRoot{r, op, shas(i, j)})
			}
		}
		for _, d := range declined {
			c.dbTick()
			_, st, err := store.RecordReworkSeen(ctx, w.rt, "bifrost-platform", d.root, d.op, d.a, sibs)
			ok(t, err)
			if st != "written" {
				t.Fatalf("fixture: rework set %q", st)
			}
		}
		w.rule(t, step, `{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`)
		exActive(t, w, step)
	}
	// Fill the stream to just under the row bound: a landed operation of a
	// root on a step not explained (dbStep stays planned), copied under new
	// root ids, written as the hash chain takes them (seq after the head,
	// prev the head's sha); its body is the model's with the id replaced.
	model := dbAccepted(t, w, c, dbStep, "bifrost-platform")
	dbOp(t, w, c, model, strings.Repeat("9f", 20), "landed")
	c.dbTick()
	owner := w.db.Connect(t, w.db.Owner)
	type rec struct {
		typ, actor, body string
		at               time.Time
	}
	rows, err := owner.Query(ctx, `SELECT event_type, actor_id, body_json::text, at FROM agentrt.event WHERE event_stream_id = $1
	  AND body_json::text LIKE '%' || $2 || '%' ORDER BY seq`, landingRepoStream, model)
	ok(t, err)
	recs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rec, error) {
		var x rec
		return x, r.Scan(&x.typ, &x.actor, &x.body, &x.at)
	})
	ok(t, err)
	var have, lastSeq int64
	var lastSha string
	ok(t, owner.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = ANY($1)`,
		[]string{landingRepoStream, store.CardsStream("bifrost-platform")}).Scan(&have))
	ok(t, owner.QueryRow(ctx, `SELECT last_seq, last_sha256 FROM agentrt.event_stream WHERE event_stream_id = $1`, landingRepoStream).Scan(&lastSeq, &lastSha))
	target := store.DefaultExplainBounds().Rows - 1_000
	copies := int((target - have) / int64(len(recs)))
	ok(t, store.InTx(ctx, owner, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		for i := 1; i <= copies; i++ {
			id := fmt.Sprintf("t_7%025X", i)
			for _, r := range recs {
				lastSeq++
				sha := fmt.Sprintf("%064x", lastSeq)
				b.Queue(`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
				  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)`, landingRepoStream, lastSeq, r.typ, r.actor, strings.ReplaceAll(r.body, model, id), lastSha, sha, r.at)
				lastSha = sha
			}
		}
		return tx.SendBatch(ctx, b).Close()
	}))
	ok(t, owner.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = ANY($1)`,
		[]string{landingRepoStream, store.CardsStream("bifrost-platform")}).Scan(&have))
	t.Logf("stream filled to %d rows (bound %d), %d copies of %d records", have, store.DefaultExplainBounds().Rows, copies, len(recs))
	d := &countDB{conn: w.db.Connect(t, w.db.Runtime)}
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak.Load() {
					peak.Store(m.HeapAlloc)
				}
			}
		}
	}()
	start := time.Now()
	steps, _, err := store.ExplainRootWithin(ctx, d, root, store.DefaultExplainBounds())
	took := time.Since(start)
	close(stop)
	<-done
	ok(t, err)
	explained, current := 0, 0
	for _, s := range steps {
		m := s.(map[string]any)
		if m["kind"] != "explained" {
			continue
		}
		explained++
		for _, b := range m["blockers"].([]store.Blocker) {
			if rw, _ := b["rework"].(map[string]any); rw != nil {
				if seen, _ := rw["seen"].(map[string]any); seen != nil && seen["current"] == true {
					current++
				}
			}
		}
	}
	grew := (float64(peak.Load()) - float64(base.HeapAlloc)) / (1 << 20)
	t.Logf("%d steps explained, %d declined roots with their current set, %s, %d statements, peak heap +%.0f MB",
		explained, current, took.Round(time.Millisecond), d.n.Load(), grew)
	if explained != 20 || current != 40 {
		t.Fatalf("%d steps explained, %d current sets; want 20 and 40", explained, current)
	}
	if took > 2500*time.Millisecond {
		t.Errorf("one explanation took %s, over 2.5 s", took)
	}
	if grew > 300 {
		t.Errorf("one explanation grew the heap %.0f MB, over 300 MB", grew)
	}
}
