package agentprogress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	// The body itself is at the cap. Wrapping it into the snapshot pushes the
	// stored bytes over the cap, so it is refused and nothing is kept.
	if put(t, h, `{"at":1,"verdict":"kept"}`).Code != http.StatusAccepted {
		t.Fatal("seed")
	}
	if rec := put(t, h, exact); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("exact snapshot: got %d, want 413: %s", rec.Code, rec.Body.String())
	}
	kept := decodeGet(t, get(t, h))
	if !bytes.Contains(kept.Document, []byte(`"kept"`)) {
		t.Fatalf("oversized put replaced the document: %s", kept.Document)
	}
	roomFit := room - 512
	if roomFit < 1 {
		t.Fatal("no room for a fitting fixture")
	}
	fit := prefix + strings.Repeat("a", roomFit) + suffix
	if int64(len(fit)) >= MaxDocumentBytes {
		t.Fatalf("fit fixture %d is not under the input cap", len(fit))
	}
	if rec := put(t, h, fit); rec.Code != http.StatusAccepted {
		t.Fatalf("fit: %d %s", rec.Code, rec.Body.String())
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

func TestSchemaRejectsWrongTypes(t *testing.T) {
	h, _, _ := newRig(t, "")
	cases := []struct {
		body string
		path string
	}{
		{`{"at":1,"owner_now":[null]}`, "owner_now[0]"},
		{`{"at":1,"owner_now":"x"}`, "owner_now"},
		{`{"at":1,"works":[1]}`, "works[0]"},
		{`{"at":1,"needs_you":[1]}`, "needs_you[0]"},
		{`{"at":1,"recent":[true]}`, "recent[0]"},
		{`{"at":1,"feed":["x"]}`, "feed[0]"},
		{`{"at":1,"owner_next":[]}`, "owner_next"},
		{`{"at":1,"coordinator":"watching"}`, "coordinator"},
		{`{"at":1,"verdict":1}`, "verdict"},
		{`{"at":1,"sure":"yes"}`, "sure"},
		{`{"at":1,"level":false}`, "level"},
		{`{"at":1,"works":[{"steps":[null]}]}`, "works[0].steps[0]"},
		{`{"at":1,"works":[{"round":"2"}]}`, "works[0].round"},
		{`{"at":1,"owner_now":[{"actions":[null]}]}`, "owner_now[0].actions[0]"},
		{`{"at":1,"owner_now":[{"actions":[{"done":"yes"}]}]}`, "owner_now[0].actions[0].done"},
		{`{"at":1,"works":[{"steps":[{"needs_owner":"yes"}]}]}`, "works[0].steps[0].needs_owner"},
		{`{"at":1,"works":[{"next":"later"}]}`, "works[0].next"},
		{`{"at":1,"works":[{"steps":"run"}]}`, "works[0].steps"},
		{`{"at":1,"feed":[{"at":"now"}]}`, "feed[0].at"},
	}
	for _, tc := range cases {
		rec := put(t, h, tc.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.path) {
			t.Fatalf("body %s: got %d %s, want 400 mentioning %s", tc.body, rec.Code, rec.Body.String(), tc.path)
		}
	}
	okBodies := []string{
		`{"at":1,"owner_next":null,"coordinator":null,"works":[],"owner_now":[],"needs_you":[],"feed":[],"recent":[]}`,
		`{"at":1,"level":"run","sure":true,"verdict":"都正常"}`,
		`{"at":1,"works":[{"title":"t","custom":{"z":1},"steps":[{"name":"s","extra":true}]}],"extra":[1,"x",null]}`,
	}
	for _, body := range okBodies {
		rec := put(t, h, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("body %s: got %d %s", body, rec.Code, rec.Body.String())
		}
	}
	got := decodeGet(t, get(t, h))
	if !bytes.Contains(got.Document, []byte(`"custom"`)) || !bytes.Contains(got.Document, []byte(`"extra"`)) {
		t.Fatalf("unknown fields dropped: %s", got.Document)
	}
}

func TestNullMeansAbsent(t *testing.T) {
	h, _, _ := newRig(t, "")
	body := `{"at":1,"verdict":null,"level":null,"sure":null,"needs_you":null,"recent":null,"feed":null,"owner_now":null,"works":[{"title":"t","risk":null,"round":null,"sure":null,"steps":[{"name":"s","started":null,"ended":null,"needs_owner":null,"note":null}]}],"owner_next":{"name":null,"at":null},"coordinator":{"at":1,"state":"up","note":null}}`
	rec := put(t, h, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("null fields: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeGet(t, get(t, h))
	for _, want := range []string{`"risk":null`, `"round":null`, `"started":null`} {
		if !bytes.Contains(got.Document, []byte(want)) {
			t.Fatalf("stored document missing %s: %s", want, got.Document)
		}
	}
}

func TestSchemaCaps(t *testing.T) {
	h, _, _ := newRig(t, "")
	works := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"title":"t"}`
		}
		return `{"at":1,"works":[` + strings.Join(parts, ",") + `]}`
	}
	if rec := put(t, h, works(maxWorks)); rec.Code != http.StatusAccepted {
		t.Fatalf("30 works: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, works(maxWorks+1)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "works") {
		t.Fatalf("31 works: %d %s", rec.Code, rec.Body.String())
	}
	steps := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"name":"s"}`
		}
		return `{"at":1,"works":[{"steps":[` + strings.Join(parts, ",") + `]},{"steps":[` + strings.Join(parts, ",") + `]}]}`
	}
	if rec := put(t, h, steps(maxSteps)); rec.Code != http.StatusAccepted {
		t.Fatalf("20 steps: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, steps(maxSteps+1)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "works[0].steps") {
		t.Fatalf("21 steps: %d %s", rec.Code, rec.Body.String())
	}
	owners := func(n, actions int) string {
		acts := make([]string, actions)
		for i := range acts {
			acts[i] = `{"text":"a"}`
		}
		entries := make([]string, n)
		for i := range entries {
			entries[i] = `{"name":"o","actions":[` + strings.Join(acts, ",") + `]}`
		}
		return `{"at":1,"owner_now":[` + strings.Join(entries, ",") + `]}`
	}
	if rec := put(t, h, owners(maxOwners, maxActions)); rec.Code != http.StatusAccepted {
		t.Fatalf("owners at cap: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, owners(maxOwners+1, 1)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "owner_now") {
		t.Fatalf("11 owners: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, owners(1, maxActions+1)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "actions") {
		t.Fatalf("11 actions: %d %s", rec.Code, rec.Body.String())
	}
	rows := func(key string, n int, elem string) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = elem
		}
		return `{"at":1,"` + key + `":[` + strings.Join(parts, ",") + `]}`
	}
	if rec := put(t, h, rows("feed", maxFeed, `{"text":"f"}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("50 feed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, rows("feed", maxFeed+1, `{"text":"f"}`)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "feed") {
		t.Fatalf("51 feed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, rows("recent", maxRecent, `"r"`)); rec.Code != http.StatusAccepted {
		t.Fatalf("30 recent: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, rows("recent", maxRecent+1, `"r"`)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "recent") {
		t.Fatalf("31 recent: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, rows("needs_you", maxNeedsYou, `"n"`)); rec.Code != http.StatusAccepted {
		t.Fatalf("30 needs_you: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(t, h, rows("needs_you", maxNeedsYou+1, `"n"`)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "needs_you") {
		t.Fatalf("31 needs_you: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRedactsSecretKeys(t *testing.T) {
	body := `{"at":1,"token":"abcdefgh12345678","ApiKey":"zzzzzzzzzzzz","Cookie":{"session":"abcdefgh12345678","n":1},"note":"Bearer abcdefghijklmnop","verdict":"plain"}`
	h, rec, store := newRig(t, filepath.Join(t.TempDir(), "state.json"))
	if put(t, h, body).Code != http.StatusAccepted {
		t.Fatal("put")
	}
	got := decodeGet(t, get(t, h))
	for _, secret := range []string{"abcdefgh12345678", "zzzzzzzzzzzz", "abcdefghijklmnop"} {
		if bytes.Contains(got.Document, []byte(secret)) {
			t.Fatalf("secret %s kept: %s", secret, got.Document)
		}
	}
	for _, want := range []string{`"token":"[redacted]"`, `"ApiKey":"[redacted]"`, `"Cookie":"[redacted]"`, `Bearer [redacted]`, `"verdict":"plain"`} {
		if !bytes.Contains(got.Document, []byte(want)) {
			t.Fatalf("missing %s in %s", want, got.Document)
		}
	}
	if bytes.Contains(got.Document, []byte(`"session"`)) || bytes.Contains(got.Document, []byte(`"n":1`)) {
		t.Fatalf("secret object was walked instead of replaced: %s", got.Document)
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	snap, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if bytes.Contains(snap.Document, []byte("abcdefgh12345678")) {
		t.Fatalf("store kept the token: %s", snap.Document)
	}
}

func TestRedactsAnyValueUnderSecretKey(t *testing.T) {
	body := `{"at":1,"password":123456,"SecretFlag":false,"api_key":[1,"abcdefgh12345678"],"credential":{"n":9},"cookie":null,"nested":{"token":true},"verdict":"plain","count":7}`
	h, _, _ := newRig(t, "")
	if put(t, h, body).Code != http.StatusAccepted {
		t.Fatal("put")
	}
	got := decodeGet(t, get(t, h))
	for _, want := range []string{
		`"password":"[redacted]"`,
		`"SecretFlag":"[redacted]"`,
		`"api_key":"[redacted]"`,
		`"credential":"[redacted]"`,
		`"cookie":"[redacted]"`,
		`"token":"[redacted]"`,
		`"verdict":"plain"`,
		`"count":7`,
	} {
		if !bytes.Contains(got.Document, []byte(want)) {
			t.Fatalf("missing %s in %s", want, got.Document)
		}
	}
	for _, kept := range []string{`123456`, `abcdefgh12345678`, `"SecretFlag":false`, `"token":true`, `"cookie":null`, `"n":9`, `"api_key":[`} {
		if bytes.Contains(got.Document, []byte(kept)) {
			t.Fatalf("kept %s in %s", kept, got.Document)
		}
	}
}

func TestNoHTMLEscapeAndStoredAngles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	h, rec, _ := newRig(t, path)
	body := `{"at":1,"v":"` + strings.Repeat("<", 200_000) + `>&"}`
	if int64(len(body)) > MaxDocumentBytes {
		t.Fatalf("fixture %d exceeds the input cap", len(body))
	}
	if put(t, h, body).Code != http.StatusAccepted {
		t.Fatal("put")
	}
	got := decodeGet(t, get(t, h))
	if bytes.Contains(got.Document, []byte(`\u003c`)) || bytes.Contains(got.Document, []byte(`\u003e`)) || bytes.Contains(got.Document, []byte(`\u0026`)) {
		t.Fatalf("response escaped HTML: %s", got.Document[:80])
	}
	if !bytes.Contains(got.Document, []byte("<>&")) {
		t.Fatal("angles were not stored")
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) > MaxDocumentBytes {
		t.Fatalf("snapshot is %d bytes", len(raw))
	}
	if bytes.Contains(raw, []byte(`\u003c`)) || !bytes.Contains(raw, []byte("<")) {
		t.Fatal("flushed snapshot escaped HTML")
	}
}

func TestGenerationTime(t *testing.T) {
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	h, rec, _ := newRig(t, "")
	rec.now = func() time.Time { return base }
	oldAt := base.Add(-3 * time.Minute).Unix()
	if put(t, h, `{"at":`+strconv.FormatInt(oldAt, 10)+`,"verdict":"old"}`).Code != http.StatusAccepted {
		t.Fatal("old")
	}
	got := decodeGet(t, get(t, h))
	gen, err := time.Parse(time.RFC3339Nano, got.GeneratedAt)
	if err != nil {
		t.Fatalf("generated_at %q: %v", got.GeneratedAt, err)
	}
	if !gen.Equal(time.Unix(oldAt, 0).UTC()) {
		t.Fatalf("generated_at %s, want %s", gen, time.Unix(oldAt, 0).UTC())
	}
	if got.GenerationLagSeconds != 180 {
		t.Fatalf("lag %d, want 180", got.GenerationLagSeconds)
	}
	future := base.Add(3 * time.Minute).Unix()
	if put(t, h, `{"at":`+strconv.FormatInt(future, 10)+`}`).Code != http.StatusAccepted {
		t.Fatal("future")
	}
	ahead := decodeGet(t, get(t, h))
	if ahead.GenerationLagSeconds != -180 {
		t.Fatalf("future lag %d, want -180", ahead.GenerationLagSeconds)
	}
}
