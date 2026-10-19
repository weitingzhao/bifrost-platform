package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// recorderLog seals events the way the recorder's events.py does and gives
// the request that carries them.
func recorderLog(t *testing.T, work string, events ...map[string]any) recorderimport.Request {
	t.Helper()
	var lines []string
	prev := ""
	for i, e := range events {
		e["v"], e["seq"], e["prev_sha256"], e["by"], e["at"] = 2, i+1, prev, "coordinator", 1791952517.25+float64(i)
		raw, err := json.Marshal(e)
		ok(t, err)
		s := sha256.Sum256(raw)
		prev = hex.EncodeToString(s[:])
		lines = append(lines, prev+" "+string(raw))
	}
	all := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return recorderimport.Request{Format: recorderimport.Format, Lines: lines,
		Source: recorderimport.Source{WorkID: work, File: work + "/events.jsonl", SHA256: hex.EncodeToString(all[:])}}
}

func recorderDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	ok(t, err)
	return db.Pool(t, db.Runtime)
}

func streamEvents(t *testing.T, pool *pgxpool.Pool, stream, typ string) int {
	t.Helper()
	var n int
	ok(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = $2`, stream, typ).Scan(&n))
	return n
}

var tdLog = func(t *testing.T) recorderimport.Request {
	return recorderLog(t, "td-336",
		map[string]any{"type": "work_opened", "work_id": "td-336", "title": "approval page"},
		map[string]any{"type": "step_started", "step_id": "03e31b45", "command": "sh -c 'cd /Users/someone/Desktop/stocks && codex exec'"},
		map[string]any{"type": "step_finished", "step_id": "03e31b45", "exit_code": 0})
}

// Gate B §2 item 3 A: a recorder log goes through POST /plan/imports' shape:
// tried, kept, one card; on Import the controller lands it as the stream
// legacy:recorder:<work>, one event per line with its original digest and
// local paths masked; the card is in needs_you while it waits; asking again
// answers what happened; another log of the same work is refused.
func TestRecorderImportThroughTheCard(t *testing.T) {
	t.Parallel()
	pool := recorderDB(t)
	a := &asker{}
	req := tdLog(t)
	stream := store.RecorderStream("td-336")
	out, err := store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k1", "retire the recorder", req, a.ask)
	ok(t, err)
	q, _ := out["question_id"].(string)
	if q == "" || len(a.cards) != 1 || a.cards[0].Options[0] != store.RecorderApply || !strings.Contains(a.cards[0].Body, "3 events") ||
		!strings.Contains(a.cards[0].Body, "1 of them had a path into one machine") {
		t.Fatalf("proposal %v, cards %+v", out, a.cards)
	}
	if strings.Contains(a.cards[0].Body, "/Users/") {
		t.Fatal("the card carries a local path")
	}
	if n := streamEvents(t, pool, stream, "legacy_imported"); n != 0 {
		t.Fatalf("the trial import left %d events", n)
	}
	again, err := store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k2", "retire the recorder", req, a.ask)
	ok(t, err)
	if again["question_id"] != q || len(a.cards) != 1 {
		t.Fatalf("asking again while the card waits: %v", again)
	}
	prog, err := store.Progress(ctx, pool)
	ok(t, err)
	if b, _ := json.Marshal(prog["needs_you"]); !strings.Contains(string(b), "Import recorder log td-336") {
		t.Fatalf("needs_you does not list the card: %s", b)
	}

	a.answer(q, store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, pool, a.question, a.ack)
	ok(t, err)
	if n != 1 || streamEvents(t, pool, stream, "legacy_imported") != 4 || streamEvents(t, pool, stream, "plan_change_decided") != 1 {
		t.Fatalf("decided %d; the stream has %d legacy_imported", n, streamEvents(t, pool, stream, "legacy_imported"))
	}
	if a.acked[q] != 1 || streamEvents(t, pool, stream, "note") != 1 {
		t.Fatalf("the card was acknowledged %d times, noted %d", a.acked[q], streamEvents(t, pool, stream, "note"))
	}
	rows, err := pool.Query(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'legacy_imported'
	  AND body_json->>'source' = 'recorder' ORDER BY event_id`, stream)
	ok(t, err)
	var got []map[string]any
	for rows.Next() {
		var b map[string]any
		ok(t, rows.Scan(&b))
		got = append(got, b)
	}
	rows.Close()
	for i, b := range got {
		line := strings.SplitN(req.Lines[i], " ", 2)
		if b["digest"] != "sha256:"+line[0] || fmt.Sprint(b["seq"]) != fmt.Sprint(i+1) || b["work_id"] != "td-336" {
			t.Fatalf("record %d: %v", i+1, b)
		}
		masked, _ := b["masked"].(bool)
		if masked != (i == 1) || (i != 1 && b["record"] != line[1]) || strings.Contains(fmt.Sprint(b["record"]), "/Users/") {
			t.Fatalf("record %d kept as %v", i+1, b)
		}
	}
	var chained bool
	ok(t, pool.QueryRow(ctx, `SELECT count(*) = 3 FROM agentrt.event r JOIN agentrt.event h ON r.cause_event_id = h.event_id
	  WHERE r.event_stream_id = $1 AND r.body_json->>'source' = 'recorder' AND h.body_json->>'source' LIKE 'recorder_import:%'`, stream).Scan(&chained))
	if !chained {
		t.Fatal("the records are not caused by the import's head")
	}
	if _, err = store.VerifyStream(ctx, pool, stream); err != nil {
		t.Fatal(err)
	}
	// Imported: the same log answers so; another log of the work is refused.
	done, err := store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k3", "again", req, a.ask)
	ok(t, err)
	if done["imported"] != false || done["decision"] != "applied" || len(a.cards) != 1 {
		t.Fatalf("the imported log asked again: %v", done)
	}
	longer := recorderLog(t, "td-336", map[string]any{"type": "work_opened", "work_id": "td-336"})
	_, err = store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k4", "another", longer, a.ask)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "imported once") {
		t.Fatalf("another log of an imported work: %v", err)
	}
}

// Not imported: the Owner's other answer records the decision and lands
// nothing; a request that breaks the chain is refused with the line and the
// rule, and writes nothing at all (T32 经导入).
func TestRecorderImportRefused(t *testing.T) {
	t.Parallel()
	pool := recorderDB(t)
	a := &asker{}
	out, err := store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k1", "retire the recorder", tdLog(t), a.ask)
	ok(t, err)
	q := out["question_id"].(string)
	a.answer(q, store.QuestionAnswered, "b")
	_, err = store.DecidePlanChanges(ctx, pool, a.question, nil)
	ok(t, err)
	stream := store.RecorderStream("td-336")
	var decision string
	ok(t, pool.QueryRow(ctx, `SELECT body_json->>'decision' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'plan_change_decided'`,
		stream).Scan(&decision))
	if decision != "refused" || streamEvents(t, pool, stream, "legacy_imported") != 0 {
		t.Fatalf("decision %s; %d imported", decision, streamEvents(t, pool, stream, "legacy_imported"))
	}

	before := rows(t, pool)
	broken := tdLog(t)
	broken.Lines[2] = strings.Replace(broken.Lines[2], `"seq":3`, `"seq":4`, 1)
	all := sha256.Sum256([]byte(strings.Join(broken.Lines, "\n") + "\n"))
	broken.Source.SHA256 = hex.EncodeToString(all[:])
	for name, c := range map[string]struct {
		req  recorderimport.Request
		want string
	}{
		"digest": {broken, "line 3: the body does not hash"},
		"file":   {func() recorderimport.Request { r := tdLog(t); r.Source.File = "/Users/x/events.jsonl"; return r }(), "relative path"},
		"format": {func() recorderimport.Request { r := tdLog(t); r.Format = "x"; return r }(), "format"},
	} {
		_, err := store.ProposeRecorderImport(ctx, pool, wr("thread:s2"), "k-"+name, "bad", c.req, a.ask)
		if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", name, err, c.want)
		}
	}
	if after := rows(t, pool); after != before {
		t.Fatalf("refused requests wrote rows: %v -> %v", before, after)
	}
}
