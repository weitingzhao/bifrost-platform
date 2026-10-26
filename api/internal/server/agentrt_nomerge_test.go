package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Exercise the same API request_action calls, including the Console record,
// admin decision, executor context, runtime role and event chain.
func TestMarkMissionsNoMergeApprovalPath(t *testing.T) {
	testNoMergeApproval(t, "")
}

func TestMarkMissionsNoMergeLostCommitResponse(t *testing.T) {
	testNoMergeApproval(t, "lost_reply")
}

func TestMarkMissionsNoMergeCommitRecovery(t *testing.T) {
	for _, fault := range []string{"not_sent", "mixed", "readback_unavailable"} {
		t.Run(fault, func(t *testing.T) { testNoMergeApproval(t, fault) })
	}
}

func testNoMergeApproval(t *testing.T, fault string) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "stg")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	srv.agentRuntime.api.DB = conn
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		for _, id := range []string{"M-1", "M-2"} {
			if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: id, Title: "archived drill", Goal: "test", Deadline: time.Now().Add(time.Hour)}); err != nil {
				return err
			}
			if err := store.MoveMission(ctx, tx, "controller", id, "active", "test"); err != nil {
				return err
			}
			if err := store.MoveMission(ctx, tx, "controller", id, "done", "test"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var version int64
	if err := conn.QueryRow(ctx, `SELECT version FROM agentrt.mission WHERE mission_id='M-1'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	params := func(env string) map[string]any {
		return map[string]any{"env": env, "missions": []any{map[string]any{"mission_id": "M-2", "expected_version": version}, map[string]any{"mission_id": "M-1", "expected_version": version}},
			"reason": "archived drill", "archive_manifest_sha256": strings.Repeat("ab", 32)}
	}
	h := srv.Router()
	do := func(method, path string, body any, role string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+role+"-test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	request := func(p map[string]any) *httptest.ResponseRecorder {
		return do(http.MethodPost, "/api/v1/approvals", map[string]any{"action": "mark_missions_no_merge", "params": p, "reason": "D-L5 archive verified", "rollback": "separate approved correction required"}, "operator")
	}
	if _, err := actions.Execute(ctx, "mark_missions_no_merge", params("stg")); err == nil {
		t.Fatal("ran without approval")
	}
	// Refuse the wrong environment before filing and again at execution.
	if rec := request(params("prod")); rec.Code != 422 {
		t.Fatalf("wrong env: %d %s", rec.Code, rec.Body.String())
	}
	execCtx := actions.WithCreateAttempt(ctx, "appr_0123456789abcdef", 1)
	if _, err := srv.markMissionsNoMerge(execCtx, params("prod")); err == nil {
		t.Fatal("cross-environment execution")
	}
	for _, missions := range []any{nil, "M-1", []any{}, []any{map[string]any{"mission_id": "M-1", "expected_version": 1.5}}, []any{map[string]any{"mission_id": "M-1", "expected_version": version, "extra": "silently lost?"}}} {
		p := params("stg")
		p["missions"] = missions
		if rec := request(p); rec.Code != 400 && rec.Code != 422 {
			t.Fatalf("invalid missions accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := request(params("stg"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = do(http.MethodGet, "/api/v1/approvals/"+created.ID, nil, "viewer")
	var row struct {
		Tier         string               `json:"tier"`
		Env          string               `json:"env"`
		Runner       string               `json:"runner"`
		Summary      string               `json:"summary"`
		Params       store.NoMergeRequest `json:"params"`
		ApprovalLine string               `json:"approval_line"`
		ParamsHash   string               `json:"params_hash"`
		Number       int                  `json:"number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.Tier != "D" || row.Env != "stg" || row.Runner != "platform" || !strings.Contains(row.Summary, "no_merge") || row.Params.Validate() != nil || row.Params.Missions[0].ExpectedVersion != version {
		t.Fatalf("Console record: %s", rec.Body.String())
	}
	decision := map[string]any{"channel": "console", "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash, "confirm_number": row.Number}
	if rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", decision, "operator"); rec.Code != http.StatusUnauthorized {
		t.Fatal("operator approved", rec.Body.String())
	}
	if rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", map[string]any{"channel": "chat"}, "admin"); rec.Code != 403 {
		t.Fatal("chat approved", rec.Body.String())
	}
	if fault == "mixed" {
		// An exact retry can encounter one prior write and one unchanged mission.
		// Losing the new COMMIT must not turn that mixed evidence into success.
		partial := row.Params
		partial.Missions = []store.NoMergeMission{{MissionID: "M-1", ExpectedVersion: version}}
		if _, err := store.MarkMissionsNoMerge(ctx, conn, partial, created.ID); err != nil {
			t.Fatal(err)
		}
	}
	if fault != "" {
		faultConfig, err := pgx.ParseConfig(db.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		var wire *noMergeDropReplyConn
		faultConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			c, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			wire = &noMergeDropReplyConn{Conn: c}
			return wire, nil
		}
		faultConn, err := pgx.ConnectConfig(ctx, faultConfig)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = faultConn.Close(context.Background()) })
		// Only the writing transaction uses the broken connection. Recovery gets
		// a healthy connection, as the production pool does after discarding it.
		srv.agentRuntime.api.DB = &noMergeLostCommitDB{DB: conn, first: faultConn, wire: wire, fault: fault}
	}
	rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", decision, "admin")
	wantStatus := "executed"
	if fault == "not_sent" {
		wantStatus = "failed"
	}
	if fault == "mixed" || fault == "readback_unavailable" {
		wantStatus = "unknown"
	}
	assertStatus := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		var result struct {
			Status string
			Error  string
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || result.Status != wantStatus {
			t.Fatalf("approval: %d %s; want %s", rec.Code, rec.Body.String(), wantStatus)
		}
		if wantStatus == "unknown" && !strings.Contains(result.Error, "Owner") {
			t.Fatalf("missing Owner instruction: %s", rec.Body.String())
		}
	}
	assertStatus(rec)
	assertStatus(do(http.MethodGet, "/api/v1/approvals/"+created.ID, nil, "viewer"))
	assertMissions := func() {
		t.Helper()
		for _, id := range []string{"M-1", "M-2"} {
			changed := fault != "not_sent" && !(fault == "mixed" && id == "M-2")
			wantVersion, wantReason, wantEvents := version, "", 0
			if changed {
				wantVersion++
				wantReason = "archived drill"
				wantEvents = 1
			}
			var gotVersion int64
			var reason string
			var events int
			if err := conn.QueryRow(ctx, `SELECT COALESCE(scope_json->>'no_merge',''), version FROM agentrt.mission WHERE mission_id=$1`, id).Scan(&reason, &gotVersion); err != nil {
				t.Fatal(err)
			}
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE mission_id=$1 AND event_type='mission_amended' AND body_json->>'approval_id'=$2`, id, created.ID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if gotVersion != wantVersion || reason != wantReason || events != wantEvents {
				t.Fatalf("%s: version=%d reason=%q events=%d; want %d %q %d", id, gotVersion, reason, events, wantVersion, wantReason, wantEvents)
			}
			if _, err := store.VerifyStream(ctx, conn, store.MissionStream(id)); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertMissions()
	if wantStatus == "executed" {
		srv.agentRuntime.api.DB = conn
		result, err := srv.markMissionsNoMerge(actions.WithCreateAttempt(ctx, created.ID, 2), params("stg"))
		if err != nil {
			t.Fatal(err)
		}
		for _, mission := range result.(map[string]any)["missions"].([]store.NoMergeResult) {
			if !mission.Duplicate || mission.EventID == 0 {
				t.Fatalf("retry missing evidence: %+v", mission)
			}
		}
		assertMissions() // no new versions or events, and both chains still verify
	}
}

// From review 585: PostgreSQL receives COMMIT normally, then the connection
// discards its response. pgx.Commit sees a real transport failure.
type noMergeDropReplyConn struct {
	net.Conn
	drop      atomic.Bool
	dropWrite atomic.Bool
}

func (c *noMergeDropReplyConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.drop.Swap(false) {
		_ = c.Conn.Close()
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}

func (c *noMergeDropReplyConn) Write(p []byte) (int, error) {
	if c.dropWrite.Swap(false) {
		_ = c.Conn.Close()
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(p)
}

type noMergeLostCommitDB struct {
	store.DB
	first *pgx.Conn
	wire  *noMergeDropReplyConn
	fault string
}

func (db *noMergeLostCommitDB) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if db.first == nil {
		if db.fault == "readback_unavailable" {
			return nil, io.ErrClosedPipe
		}
		return db.DB.BeginTx(ctx, options)
	}
	conn := db.first
	db.first = nil
	tx, err := conn.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return noMergeLostCommitTx{Tx: tx, wire: db.wire, notSent: db.fault == "not_sent" || db.fault == "mixed"}, nil
}

type noMergeLostCommitTx struct {
	pgx.Tx
	wire    *noMergeDropReplyConn
	notSent bool
}

func (tx noMergeLostCommitTx) Commit(ctx context.Context) error {
	if tx.notSent {
		tx.wire.dropWrite.Store(true)
	} else {
		tx.wire.drop.Store(true)
	}
	return tx.Tx.Commit(ctx)
}
