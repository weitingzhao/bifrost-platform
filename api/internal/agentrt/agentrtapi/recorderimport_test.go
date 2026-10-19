package agentrtapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// recorderRequest seals two recorder events as events.py does.
func recorderRequest(t *testing.T) recorderimport.Request {
	t.Helper()
	var lines []string
	prev := ""
	for i, e := range []map[string]any{{"type": "work_opened", "work_id": "td-337"}, {"type": "step_seen", "step_id": "a1"}} {
		e["v"], e["seq"], e["prev_sha256"], e["by"], e["at"] = 2, i+1, prev, "wrapper", 1791936767.5
		raw, err := json.Marshal(e)
		must(t, err)
		s := sha256.Sum256(raw)
		prev = hex.EncodeToString(s[:])
		lines = append(lines, prev+" "+string(raw))
	}
	all := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return recorderimport.Request{Format: recorderimport.Format, Lines: lines,
		Source: recorderimport.Source{WorkID: "td-337", File: "td-337/events.jsonl", SHA256: hex.EncodeToString(all[:])}}
}

// Gate B §2 item 3 A: POST /plan/imports takes a recorder log by its
// format, puts it on one card, and the decision pass lands it; a log that
// breaks its chain is refused with the line and the rule, and a field the
// format does not have is refused.
func TestRecorderImportThroughTheRoute(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	req := recorderRequest(t)
	before := len(cards)
	body := map[string]any{"idempotency_key": "r1", "reason": "retire the recorder", "request": req}
	code, out := call(t, s, "reporter", "POST", "plan/imports", body, "X-Bifrost-Session", "s1")
	if code != 200 || out["question_id"] == nil || out["log_artifact_id"] == nil {
		t.Fatalf("recorder import proposal = %d %v", code, out)
	}
	if len(cards) != before+1 || !strings.HasPrefix(cards[len(cards)-1].Title, "Import recorder log td-337: ") ||
		cards[len(cards)-1].Options[0] != store.RecorderApply {
		t.Fatalf("cards = %+v", cards[before:])
	}
	n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil)
	must(t, err)
	var landed int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'legacy:recorder:td-337'
	  AND event_type = 'legacy_imported'`).Scan(&landed))
	if n != 1 || landed != 3 {
		t.Fatalf("decided %d, landed %d events; want 1 and a head with 2 records", n, landed)
	}

	broken := recorderRequest(t)
	broken.Lines[1] = strings.Replace(broken.Lines[1], `"step_id":"a1"`, `"step_id":"a2"`, 1)
	all := sha256.Sum256([]byte(strings.Join(broken.Lines, "\n") + "\n"))
	broken.Source.SHA256 = hex.EncodeToString(all[:])
	code, out = call(t, s, "reporter", "POST", "plan/imports", map[string]any{"idempotency_key": "r2", "reason": "x", "request": broken},
		"X-Bifrost-Session", "s1")
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(out), "line 2: the body does not hash to its digest") {
		t.Fatalf("a broken chain = %d %v", code, out)
	}
	// Review 402 B1: a log whose masking would change a key is refused at
	// the proposal, before any card.
	keyed := recorderRequest(t)
	kb := `{"/tmp/x":"first","<tmp>/x":"second","at":1,"by":"wrapper","prev_sha256":"","seq":1,"type":"work_opened","v":2,"work_id":"td-337"}`
	ks := sha256.Sum256([]byte(kb))
	keyed.Lines = []string{hex.EncodeToString(ks[:]) + " " + kb}
	kall := sha256.Sum256([]byte(keyed.Lines[0] + "\n"))
	keyed.Source.SHA256 = hex.EncodeToString(kall[:])
	asked := len(cards)
	code, out = call(t, s, "reporter", "POST", "plan/imports", map[string]any{"idempotency_key": "r4", "reason": "x", "request": keyed},
		"X-Bifrost-Session", "s1")
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(out), "a key has a path") || len(cards) != asked {
		t.Fatalf("a log with a path in a key = %d %v (%d cards asked)", code, out, len(cards)-asked)
	}
	extra := map[string]any{"format": recorderimport.Format, "source": req.Source, "lines": req.Lines, "nodes": []any{}}
	code, out = call(t, s, "reporter", "POST", "plan/imports", map[string]any{"idempotency_key": "r3", "reason": "x", "request": extra},
		"X-Bifrost-Session", "s1")
	if code != http.StatusBadRequest || !strings.HasPrefix(fmt.Sprint(out["message"]), "body: ") {
		t.Fatalf("a field the format does not have = %d %v", code, out)
	}
}
