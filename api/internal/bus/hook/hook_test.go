package hook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	agentactor "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the actor_registered vocabulary and the seed, in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

var (
	ctx    = context.Background()
	secret = []byte("test-hook-secret")
	shaA   = strings.Repeat("a", 40)
	shaB   = strings.Repeat("b", 40)
)

func payload(ref, repo, before, after string) []byte {
	b, _ := json.Marshal(map[string]any{"ref": ref, "before": before, "after": after, "repository": map[string]any{"name": repo}})
	return b
}

func sign(body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func post(h *hook.Hook, body []byte, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bus/hooks/gitea", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "push")
	if sig != "" {
		req.Header.Set("X-Gitea-Signature", sig)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// noDB fails the test if the hook reaches the ledger.
func noDB(t *testing.T) func() eventlog.DB {
	return func() eventlog.DB {
		t.Error("the hook reached the ledger")
		return nil
	}
}

// 2026-11-16: bus step 1 (RULES.md §1 E3): the hook holds no bearer; a body
// Gitea did not sign with the secret is refused before anything is read.
func TestHookRefusesABadSignature(t *testing.T) {
	h := &hook.Hook{Secret: secret, Repos: []string{"bifrost-platform"}, DB: noDB(t)}
	body := payload("refs/heads/main", "bifrost-platform", shaA, shaB)
	for name, sig := range map[string]string{"none": "", "wrong": sign([]byte("other")), "not hex": "zz"} {
		if rec := post(h, body, sig); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s signature: HTTP %d", name, rec.Code)
		}
	}
}

// 2026-11-16: bus step 1 (step-1 config): without BUS_GITEA_HOOK_SECRET the
// hook is off: 503, nothing written.
func TestHookWithoutItsSecretIsOff(t *testing.T) {
	h := &hook.Hook{Repos: []string{"bifrost-platform"}, DB: noDB(t)}
	body := payload("refs/heads/main", "bifrost-platform", shaA, shaB)
	if rec := post(h, body, sign(body)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP %d", rec.Code)
	}
}

// 2026-11-16: bus step 1: only a push to main of a watched repository
// writes; another branch, another repository or a deleted main is ignored.
func TestHookIgnoresWhatIsNotMain(t *testing.T) {
	h := &hook.Hook{Secret: secret, Repos: []string{"bifrost-platform"}, DB: noDB(t)}
	for name, body := range map[string][]byte{
		"a branch":           payload("refs/heads/ready/x", "bifrost-platform", shaA, shaB),
		"a tag":              payload("refs/tags/v1", "bifrost-platform", shaA, shaB),
		"another repository": payload("refs/heads/main", "bifrost-ui", shaA, shaB),
		"main deleted":       payload("refs/heads/main", "bifrost-platform", shaA, strings.Repeat("0", 40)),
	} {
		if rec := post(h, body, sign(body)); rec.Code != http.StatusAccepted {
			t.Errorf("%s: HTTP %d %s", name, rec.Code, rec.Body)
		}
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): a push to main appends one
// git.main.advanced to git:<repo>, and the same commit again appends nothing
// (Gitea's redelivery, or the hook and the reconciliation both seeing it).
func TestHookAppendsOncePerCommit(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := agentactor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	h := &hook.Hook{Secret: secret, Repos: []string{"bifrost-platform"}, DB: func() eventlog.DB { return pool }}
	body := payload("refs/heads/main", "bifrost-platform", strings.Repeat("0", 40), shaA)
	for i, want := range []bool{true, false} {
		rec := post(h, body, sign(body))
		var got struct{ Appended bool }
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Appended != want {
			t.Fatalf("push %d: HTTP %d %s", i+1, rec.Code, rec.Body)
		}
	}
	var n int
	var bodyJSON, actor string
	if err := pool.QueryRow(ctx, `SELECT count(*), max(body_json::text), max(actor_id) FROM agentrt.event WHERE event_stream_id = 'git:bifrost-platform' AND event_type = 'git.main.advanced'`).
		Scan(&n, &bodyJSON, &actor); err != nil || n != 1 || actor != hook.Actor {
		t.Fatalf("%d events by %s: %v", n, actor, err)
	}
	if !strings.Contains(bodyJSON, `"before": ""`) || !strings.Contains(bodyJSON, `"change": "pushed"`) {
		t.Errorf("body %s", bodyJSON)
	}
	// The reconciliation finds main elsewhere and records it, before = the
	// ledger's last commit; when the ledger agrees it writes nothing.
	rec := &hook.Reconciler{DB: func() eventlog.DB { return pool }, Repos: []string{"bifrost-platform"},
		Head: func(context.Context, string) (string, error) { return shaB, nil }}
	for range 2 {
		if err := rec.Pass(ctx); err != nil {
			t.Fatal(err)
		}
	}
	last, err := hook.LastCommit(ctx, pool)("bifrost-platform")
	if err != nil || last != shaB {
		t.Fatalf("last commit %q %v", last, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'git:bifrost-platform' AND body_json->>'change' = 'reconciled' AND body_json->>'before' = $1`, shaA).
		Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d reconciled events %v", n, err)
	}
	if _, err := eventlog.VerifyStream(ctx, pool, hook.Stream("bifrost-platform")); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): the database's closed vocabulary
// takes the bus type (migration 0025). Since 0041 (2026-11-20) it takes bus
// topics by their shape, so each is written to a copy of the constraint.
func TestTheDatabaseTakesTheBusType(t *testing.T) {
	db := agentrttest.Migrated(t)
	conn := db.Connect(t, db.Admin)
	var def string
	if err := conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TEMP TABLE vocab (event_type text, CONSTRAINT event_type_closed `+def+`)`); err != nil {
		t.Fatal(err)
	}
	for _, typ := range busTypes() {
		if _, err := conn.Exec(ctx, `INSERT INTO vocab VALUES ($1)`, typ); err != nil {
			t.Errorf("event_type_closed does not take %s: %v", typ, err)
		}
	}
}

// busTypes is the dotted types the vocabulary declares: the bus's.
func busTypes() []string {
	vocabulary, _ := eventlog.EventTypes()
	var out []string
	for _, typ := range vocabulary {
		if strings.Contains(typ, ".") {
			out = append(out, typ)
		}
	}
	return out
}

// 2026-11-16: bus step 1 (RULES.md §7: "register line + Go vocabulary, tests
// aligned"): every bus type the event log is declared is registered, and its
// body carries the fixed fields its register entry promises.
func TestRegisterAndVocabularyAgree(t *testing.T) {
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	rocket := all["rocket"]
	declared := busTypes()
	if len(declared) == 0 {
		t.Fatal("no bus type is declared")
	}
	for _, typ := range declared {
		ch, ok := rocket.Channels[typ]
		if !ok {
			t.Errorf("%s is declared to the event log but not registered", typ)
			continue
		}
		fields, _ := eventlog.BodyOf(typ)
		has := map[string]bool{}
		for _, f := range fields {
			has[f.Name] = !f.Optional
		}
		need := []string{"subject", "ref", "change"}
		if ch.Key != "" {
			need = append(need, "key", ch.Key)
		}
		for _, n := range need {
			if !has[n] {
				t.Errorf("%s: the body does not require %q", typ, n)
			}
		}
		// The ref the events carry is the register's, its {placeholder} filled.
		pattern := "^" + regexp.MustCompile(`\\\{[a-z]+\\\}`).ReplaceAllString(regexp.QuoteMeta(ch.Ref), `[A-Za-z0-9._-]+`) + "$"
		if typ == hook.Type && !regexp.MustCompile(pattern).MatchString(hook.Ref("bifrost-platform")) {
			t.Errorf("%s: ref %q is not the register's %q", typ, hook.Ref("bifrost-platform"), ch.Ref)
		}
	}
	// The positive control: git.main.advanced is one of them.
	if !strings.Contains(strings.Join(declared, " "), hook.Type) {
		t.Errorf("%s is not declared", hook.Type)
	}
}

// 2026-11-16: bus step 1: the body is held to its shape: subject, ref and
// key are made of repo and commit, before is empty or a commit.
func TestTheBodyIsChecked(t *testing.T) {
	if err := eventlog.CheckBody(hook.Type, hook.Body("bifrost-platform", shaA, "", hook.ChangePushed)); err != nil {
		t.Fatal(err)
	}
	bad := hook.Body("bifrost-platform", shaA, "", hook.ChangePushed)
	bad["subject"] = "other@" + shaA
	shortBefore := hook.Body("bifrost-platform", shaA, "abc", hook.ChangePushed)
	badChange := hook.Body("bifrost-platform", shaA, "", "forced")
	for name, b := range map[string]map[string]any{"subject": bad, "before": shortBefore, "change": badChange} {
		if err := eventlog.CheckBody(hook.Type, b); err == nil {
			t.Errorf("a wrong %s passes", name)
		}
	}
}
