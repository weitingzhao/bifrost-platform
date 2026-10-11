package agentprogress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newRig(t *testing.T, path string) (*Handler, *Recorder, *Store) {
	t.Helper()
	store := NewStore(path)
	rec := NewRecorder(store)
	return NewHandler(rec), rec, store
}

func put(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/progress", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandlePut(rec, req)
	return rec
}

func get(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/progress", nil)
	rec := httptest.NewRecorder()
	h.HandleGet(rec, req)
	return rec
}

func decodeGet(t *testing.T, rec *httptest.ResponseRecorder) getResponse {
	t.Helper()
	var body getResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body %s", err, rec.Body.String())
	}
	return body
}

func TestNothingPublished(t *testing.T) {
	h, _, _ := newRig(t, "")
	rec := get(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	body := decodeGet(t, rec)
	if string(body.Document) != "null" || body.Stale || body.AgeSeconds != 0 || body.Host != "" || body.ReceivedAt != "" {
		t.Fatalf("empty: %+v", body)
	}
}

func TestMalformedAndHost(t *testing.T) {
	h, _, _ := newRig(t, "")
	bad := []string{
		``,
		`[]`,
		`null`,
		`"at"`,
		`1`,
		`{"verdict":"x"}`,
		`{"at":"1"}`,
		`{"at":true}`,
		`{"at":null}`,
		`{"at":1}{"at":2}`,
		`{"at":1,"host":"bad host"}`,
		`{"at":1,"host":"-nope"}`,
		`{"at":1,"host":1}`,
	}
	for _, body := range bad {
		rec := put(t, h, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: got %d, want 400: %s", body, rec.Code, rec.Body.String())
		}
	}
	ok := put(t, h, `{"at":1791680000.5,"verdict":"都正常","extra":true}`)
	if ok.Code != http.StatusAccepted {
		t.Fatalf("valid: %d %s", ok.Code, ok.Body.String())
	}
	got := decodeGet(t, get(t, h))
	var doc map[string]any
	if err := json.Unmarshal(got.Document, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["verdict"] != "都正常" || doc["extra"] != true {
		t.Fatalf("document lost fields: %s", got.Document)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.ReceivedAt); err != nil {
		t.Fatalf("received_at: %v", err)
	}
}

func TestSizeLimit(t *testing.T) {
	h, _, _ := newRig(t, "")
	prefix := `{"at":1,"v":"`
	suffix := `"}`
	room := int(MaxDocumentBytes) - len(prefix) - len(suffix)
	exact := prefix + strings.Repeat("a", room) + suffix
	if int64(len(exact)) != MaxDocumentBytes {
		t.Fatalf("fixture length %d, want %d", len(exact), MaxDocumentBytes)
	}
	if rec := put(t, h, exact); rec.Code != http.StatusAccepted {
		t.Fatalf("exact: %d %s", rec.Code, rec.Body.String())
	}
	over := exact + " "
	if int64(len(over)) <= MaxDocumentBytes {
		t.Fatal("over fixture is not over")
	}
	if rec := put(t, h, over); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over: got %d, want 413: %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/progress", strings.NewReader(`{"at":1}`))
	req.ContentLength = MaxDocumentBytes + 1
	rec := httptest.NewRecorder()
	h.HandlePut(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("content-length: got %d, want 413", rec.Code)
	}
}

func TestRedactsNestedBearer(t *testing.T) {
	secret := "Bearer abcdefghijklmnop"
	body := `{"at":1,"works":[{"steps":[{"note":"see ` + secret + ` please"}]}]}`
	h, rec, store := newRig(t, filepath.Join(t.TempDir(), "state.json"))
	if put(t, h, body).Code != http.StatusAccepted {
		t.Fatal("put")
	}
	got := decodeGet(t, get(t, h))
	if bytes.Contains(got.Document, []byte("abcdefghijklmnop")) || !bytes.Contains(got.Document, []byte("Bearer [redacted]")) {
		t.Fatalf("get did not redact: %s", got.Document)
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	snap, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if bytes.Contains(snap.Document, []byte("abcdefghijklmnop")) || !bytes.Contains(snap.Document, []byte("Bearer [redacted]")) {
		t.Fatalf("store did not redact: %s", snap.Document)
	}
}

func TestStaleFlag(t *testing.T) {
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	h, rec, _ := newRig(t, "")
	rec.now = func() time.Time { return base }
	if put(t, h, `{"at":1,"verdict":"running"}`).Code != http.StatusAccepted {
		t.Fatal("put")
	}
	h.now = func() time.Time { return base.Add(StaleAfter) }
	fresh := decodeGet(t, get(t, h))
	if fresh.Stale || fresh.AgeSeconds != int64(StaleAfter/time.Second) {
		t.Fatalf("exactly 5 min should not be stale: %+v", fresh)
	}
	h.now = func() time.Time { return base.Add(StaleAfter + time.Second) }
	stale := decodeGet(t, get(t, h))
	if !stale.Stale || stale.AgeSeconds != int64(StaleAfter/time.Second)+1 {
		t.Fatalf("older than 5 min: %+v", stale)
	}
	h.now = func() time.Time { return base.Add(4 * time.Minute) }
	young := decodeGet(t, get(t, h))
	if young.Stale {
		t.Fatalf("4 min should be fresh: %+v", young)
	}
}

func TestLatestWinsAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-progress", "state.json")
	h, rec, _ := newRig(t, path)
	if put(t, h, `{"at":1,"verdict":"first","host":"vision-mac"}`).Code != http.StatusAccepted {
		t.Fatal("first")
	}
	if put(t, h, `{"at":2,"verdict":"second","host":"vision-mac"}`).Code != http.StatusAccepted {
		t.Fatal("second")
	}
	got := decodeGet(t, get(t, h))
	var doc map[string]any
	if err := json.Unmarshal(got.Document, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["verdict"] != "second" || got.Host != "vision-mac" {
		t.Fatalf("latest: host %q doc %s", got.Host, got.Document)
	}
	// Before flush the file is not the source of truth.
	early := NewStore(path)
	if _, ok, err := early.Load(); err != nil || ok {
		t.Fatalf("unflushed store visible: ok=%v err=%v", ok, err)
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path)
	snap, ok, err := restarted.Load()
	if err != nil || !ok {
		t.Fatalf("restart: ok=%v err=%v", ok, err)
	}
	if !bytes.Contains(snap.Document, []byte(`"verdict":"second"`)) || snap.Host != "vision-mac" {
		t.Fatalf("restarted snapshot: host %q doc %s", snap.Host, snap.Document)
	}
	h2 := NewHandler(NewRecorder(restarted))
	again := decodeGet(t, get(t, h2))
	if !bytes.Contains(again.Document, []byte(`"second"`)) {
		t.Fatalf("handler after restart: %s", again.Document)
	}
}

func TestPublishFlushRace(t *testing.T) {
	rec := NewRecorder(NewStore(""))
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			raw := json.RawMessage(`{"at":` + strings.Repeat("1", 1) + `}`)
			_ = n
			rec.Publish(raw, "h")
		}(i)
		go func() {
			defer wg.Done()
			_ = rec.Flush()
		}()
	}
	wg.Wait()
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rec.Current(); err != nil {
		t.Fatal(err)
	}
}
