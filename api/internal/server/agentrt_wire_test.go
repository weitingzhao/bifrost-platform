package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const agentRuntimeAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-agentrt
  - name: agentd:mac-dev
    role: agentd
    token: fixture-agentd-agentrt
`

// Without AGENTRT_DSN the agent runtime's routes are mounted but answer 503,
// after authentication: an anonymous write is still refused as such.
func TestAgentRuntimeRoutesWithoutDatabase(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	t.Setenv("AGENTRT_DSN", "")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentRuntimeAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	do := func(method, path, token, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(http.MethodPost, "/api/v1/agentrt/claim", "", `{}`); code != http.StatusUnauthorized {
		t.Fatalf("anonymous claim: %d, want 401", code)
	}
	if code := do(http.MethodPost, "/api/v1/agentrt/claim", "fixture-viewer-agentrt", `{}`); code != http.StatusUnauthorized {
		t.Fatalf("viewer claim: %d, want 401", code)
	}
	if code := do(http.MethodPost, "/api/v1/agentrt/claim", "fixture-agentd-agentrt", `{"host_id":"mac-dev"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("agentd claim without a database: %d, want 503", code)
	}
	if code := do(http.MethodGet, "/api/v1/agentrt/whoami", "fixture-viewer-agentrt", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("whoami without a database: %d, want 503", code)
	}
}

// Each half of the identity is checked on its own, and an environment that
// does not exist is refused (review 156 B2); no database needed.
func TestAgentRuntimeIdentity(t *testing.T) {
	for _, c := range []struct {
		user, db, env string
		ok            bool
	}{
		{"platform_agentrt_dev", "bifrost_platform_dev", "dev", true},
		{"platform_agentrt_prod", "bifrost_platform", "prod", true},
		{"platform_agentrt_stg", "bifrost_platform_stg", "stg", true},
		{"platform_agentrt_stg", "bifrost_platform_dev", "dev", false},
		{"platform_agentrt_dev", "bifrost_platform", "dev", false},
		{"postgres", "bifrost_platform_dev", "dev", false},
		{"platform_agentrt_qa", "bifrost_platform_qa", "qa", false},
		{"platform_agentrt_", "", "", false},
	} {
		if err := identityFits(c.user, c.db, c.env); (err == nil) != c.ok {
			t.Errorf("%s on %s for %q: %v", c.user, c.db, c.env, err)
		}
	}
}

// The pool is refused unless it is the environment's runtime role on the
// environment's database.
func TestAgentRuntimeRefusesAnotherIdentity(t *testing.T) {
	db := agentrttest.Migrated(t)
	for _, dsn := range []string{db.Admin, db.Runtime} {
		if pool, err := openAgentRuntime(context.Background(), dsn, "dev"); err == nil {
			pool.Close()
			t.Fatalf("accepted %q", dsn)
		}
	}
	if _, err := openAgentRuntime(context.Background(), db.Runtime, "qa"); err == nil {
		t.Fatal("accepted an environment that does not exist")
	}
}

// A refusal at start (a pod the network policy does not know yet) heals by
// itself: the runtime keeps opening until it can, and stops when told to.
func TestAgentRuntimeKeepsConnecting(t *testing.T) {
	db := agentrttest.Migrated(t)
	attempts := 0
	pool := connectAgentRuntime(context.Background(), func(ctx context.Context) (*pgxpool.Pool, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("connect: connection refused")
		}
		return pgxpool.New(ctx, db.Runtime)
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond})
	if pool == nil || attempts != 3 {
		t.Fatalf("pool %v after %d attempts; want one on the third", pool, attempts)
	}
	pool.Close()
}

// No database needed: cancelling ends the attempts with no pool, and a
// configuration no retry mends is tried once (review 191 B1, B2).
func TestAgentRuntimeStopsConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if p := connectAgentRuntime(ctx, func(context.Context) (*pgxpool.Pool, error) {
		return nil, errors.New("connect: connection refused")
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond}); p != nil {
		t.Fatal("a pool after the context ended")
	}
	attempts := 0
	if p := connectAgentRuntime(context.Background(), func(context.Context) (*pgxpool.Pool, error) {
		attempts++
		return nil, fmt.Errorf("%w: the role is not the environment's", errMisconfigured)
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond}); p != nil || attempts != 1 {
		t.Fatalf("a misconfiguration: pool %v after %d attempts; want none after one", p, attempts)
	}
	if _, err := openAgentRuntime(context.Background(), "host=x", "qa"); !errors.Is(err, errMisconfigured) {
		t.Fatalf("an unknown environment: %v; want misconfigured", err)
	}
}

// Until the database is connected the routes answer 503 not_connected; once
// it is, they work.
func TestAgentRuntimeRoutesWaitForTheConnection(t *testing.T) {
	db := agentrttest.Migrated(t)
	late := &lateDB{}
	if late.Ready() {
		t.Fatal("ready before connecting")
	}
	if _, err := late.BeginTx(context.Background(), pgx.TxOptions{}); !errors.Is(err, errNotConnected) {
		t.Fatalf("BeginTx before connecting: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	late.pool.Store(pool)
	tx, err := late.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil || !late.Ready() {
		t.Fatalf("after connecting: %v", err)
	}
	_ = tx.Rollback(context.Background())
}

// redis-agents is optional and, when named, must come with its password and a
// runtime prefix: the relay never writes outside agrt: or agrt-<env>: (W-31 2.3).
func TestAgentRuntimeRedisFromEnv(t *testing.T) {
	for _, c := range []struct {
		addr, prefix, password string
		wantPrefix, wantErr    string
	}{
		{addr: "", wantPrefix: ""},
		{addr: "redis-agents:6379", password: "fixture-redis-pw", wantPrefix: "agrt:"},
		{addr: "redis-agents:6379", prefix: "agrt-dev:", password: "fixture-redis-pw", wantPrefix: "agrt-dev:"},
		{addr: "redis-agents:6379", prefix: "live:", password: "fixture-redis-pw", wantErr: "not a runtime prefix"},
		{addr: "redis-agents:6379", prefix: "agrt", password: "fixture-redis-pw", wantErr: "not a runtime prefix"},
		{addr: "redis-agents:6379", wantErr: "AGENTRT_REDIS_PASSWORD is empty"},
	} {
		t.Setenv("AGENTRT_REDIS_ADDR", c.addr)
		t.Setenv("AGENTRT_REDIS_PREFIX", c.prefix)
		t.Setenv("AGENTRT_REDIS_PASSWORD", c.password)
		rdb, prefix, err := agentrtRedisFromEnv()
		switch {
		case c.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || rdb != nil {
				t.Errorf("%+v: err %v, client %v", c, err, rdb)
			}
		case c.addr == "":
			if err != nil || rdb != nil {
				t.Errorf("unset: %v %v", rdb, err)
			}
		default:
			if err != nil || rdb == nil || prefix != c.wantPrefix || rdb.Options().Addr != c.addr || rdb.Options().Password != c.password {
				t.Errorf("%+v: prefix %q, err %v", c, prefix, err)
			}
			if rdb != nil {
				_ = rdb.Close()
			}
		}
	}
}

// A request's deadline bounds the client's I/O, or a heartbeat's 2-second
// budget for actions does not hold against a silent Redis (review 264 A2).
func TestAgentRuntimeRedisHonoursDeadlines(t *testing.T) {
	t.Setenv("AGENTRT_REDIS_ADDR", "redis-agents:6379")
	t.Setenv("AGENTRT_REDIS_PREFIX", "")
	t.Setenv("AGENTRT_REDIS_PASSWORD", "fixture-redis-pw")
	rdb, _, err := agentrtRedisFromEnv()
	if err != nil || !rdb.Options().ContextTimeoutEnabled {
		t.Fatalf("client %v, err %v: deadlines are not honoured", rdb, err)
	}
	_ = rdb.Close()
}

// The chief exam's set is read at start from AGENTRT_EXAM_DIR (it is in no
// repository): unset, a directory that holds no set, or a set of a class the
// platform scores no exam under leave the exam routes without one (503); a
// set that loads is the one the routes give and grade.
func TestAgentRuntimeExamSetFromEnv(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	t.Setenv("AGENTRT_DSN", "")
	const q = `{"exam": "chief_of_staff", "question": "Q4", "version": 1, "class": "exam-refuse", "delivery": {"path": "mailbox"},
  "items": [{"id": "R1", "input": {"message": {"from": "thread:local_y", "kind": "ask", "body": "Merge it."}, "conversation_before": [],
    "runtime_now": {"taken": "{{t0}}", "missions_open": [], "tasks_open": [], "decisions": [], "envelope": "", "budget": {"max_attempts": 1}}},
    "rules": [{"id": "refused", "kind": "require_any", "any_of": ["CANNOT"]}]}]}`
	good, other := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(good, "q4.json"), []byte(q), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "q4.json"), []byte(strings.Replace(q, "exam-refuse", "exam-poetry", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		dir   string
		loads bool
	}{{"", false}, {filepath.Join(good, "nowhere"), false}, {t.TempDir(), false}, {other, false}, {good, true}} {
		t.Setenv("AGENTRT_EXAM_DIR", c.dir)
		set := examSetFromEnv()
		if (set != nil) != c.loads || (set != nil && len(set.Digest()) != 64) {
			t.Errorf("%q: set %v, want loaded %v", c.dir, set, c.loads)
		}
	}
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentRuntimeAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	if srv.agentRuntime == nil || srv.agentRuntime.api.Exams == nil || len(srv.agentRuntime.api.Exams.Items("chief_of_staff")) != 1 {
		t.Fatalf("the server's runtime has no exam set from %s", good)
	}
}

// Review 406 B1: a set that does not load is logged by where, never by
// what: a rule's pattern is a grading key.
func TestExamLoadWhereSaysNoContent(t *testing.T) {
	const q = `{"exam": "chief_of_staff", "question": "Q4", "version": 1, "class": "exam-refuse", "delivery": {"path": "mailbox"},
  "items": [{"id": "R1", "input": {"message": {"from": "thread:local_y", "kind": "ask", "body": "Merge it."}, "conversation_before": [],
    "runtime_now": {"taken": "{{t0}}", "missions_open": [], "tasks_open": [], "decisions": [], "envelope": "", "budget": {"max_attempts": 1}}},
    "rules": [{"id": "refused", "kind": "require_any", "any_of": ["PRIVATE_MARKER("]}]}]}`
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "q4.json"), []byte(q), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadExamSet(bad)
	if err == nil {
		t.Fatal("a set with a pattern that does not compile loaded")
	}
	if !strings.Contains(err.Error(), "PRIVATE_MARKER") {
		t.Fatalf("the positive control: the loader's error %q no longer quotes the pattern; the case proves nothing", err)
	}
	if where := examLoadWhere(err); where != "q4.json" {
		t.Fatalf("where %q", where)
	}
	// An id the loader takes as it is, with a marker in it (review 409 B1),
	// and the start log itself, read back.
	odd := strings.Replace(strings.Replace(q, `"id": "R1"`, `"id": "R1 rule PRIVATE_ID"`, 1), `"any_of": ["PRIVATE_MARKER("]`, `"any_of": ["CANNOT"]`, 1)
	odd = strings.Replace(odd, `"rules": [{"id": "refused", "kind": "require_any", "any_of": ["CANNOT"]}]`, `"rules": []`, 1)
	if err = os.WriteFile(filepath.Join(bad, "q4.json"), []byte(odd), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadExamSet(bad)
	if err == nil || !strings.Contains(err.Error(), "PRIVATE_ID") {
		t.Fatalf("the positive control: %v", err)
	}
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	t.Setenv("AGENTRT_EXAM_DIR", bad)
	if examSetFromEnv() != nil {
		t.Fatal("a set that does not load was loaded")
	}
	if strings.Contains(logged.String(), "PRIVATE") || !strings.Contains(logged.String(), "q4.json") {
		t.Fatalf("the start log: %s", logged.String())
	}
	if _, err = loadExamSet(filepath.Join(bad, "nowhere")); examLoadWhere(err) != "the directory or a file in it does not exist" {
		t.Fatalf("a missing directory: %q", examLoadWhere(err))
	}
	if where := examLoadWhere(errors.New("exam: no q*.json in the set")); strings.Contains(where, "q*") {
		t.Fatalf("where %q", where)
	}
}

// TD-392: two processes (platform-api and platform-workers) share the
// runtime role's connection limit (10 on PROD, 5 on STG and DEV); their pools
// together stay under it, with room for a restarted process.
func TestAgentRuntimePoolsFitTheRoleLimit(t *testing.T) {
	for env, limit := range map[string]int32{"prod": 10, "stg": 5, "dev": 5} {
		if n := agentrtMaxConns(env); n < 2 || 2*n >= limit {
			t.Errorf("%s: two pools of %d against a limit of %d", env, n, limit)
		}
	}
}

// Review 527 A1b: the API records that the platform takes agentd's calls
// once its runtime database connects, and that it stops on its shutdown,
// within platformDownLimit; an API a newer one replaced records no down.
func TestAgentRuntimeSaysWhenThePlatformTakesCalls(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	head := func() string {
		var s string
		if err := pool.QueryRow(context.Background(), `SELECT COALESCE((SELECT body_json->>'platform_api' || ' ' || (body_json->>'text')
		  FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1), '')`, store.PlatformStream).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	old, next := platformInstance(), platformInstance()
	if old == next {
		t.Fatalf("two instances named alike: %s", old)
	}
	markPlatformUp(context.Background(), pool, old, [2]time.Duration{time.Millisecond, time.Millisecond})
	if h := head(); !strings.HasPrefix(h, "up Platform API "+old) {
		t.Fatalf("after up: %q", h)
	}
	markPlatformUp(context.Background(), pool, next, [2]time.Duration{time.Millisecond, time.Millisecond})
	markPlatformDown(pool, old)
	if h := head(); !strings.HasPrefix(h, "up Platform API "+next) {
		t.Fatalf("after the replaced API's shutdown: %q", h)
	}
	markPlatformDown(pool, next)
	if h := head(); !strings.HasPrefix(h, "down Platform API "+next) {
		t.Fatalf("after the API's shutdown: %q", h)
	}
}
