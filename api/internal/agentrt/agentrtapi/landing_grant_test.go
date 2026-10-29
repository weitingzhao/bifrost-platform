package agentrtapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// grantFixture appends a grant for the waiting op as the approval executor
// would (actor controller, store.Append's checks): the route under test is
// the revocation, not the grant's material (store's landing grant tests).
func grantFixture(t *testing.T, pool *pgxpool.Pool, op string) string {
	t.Helper()
	var o landing.Operation
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		s, err := store.LandingState(ctx, tx, "landing:bifrost-platform")
		o = s.Operations[op]
		return err
	}))
	now := time.Now().UTC()
	approved := now.Add(-time.Minute)
	g := store.LandingGrant{V: "1", GrantID: "lg_appr_fixture", ApprovalID: "appr_fixture", ApprovalNumber: 7, Action: store.ActionGuardrailLanding,
		ParamsHash: strings.Repeat("c", 64), Home: "prod", Env: "prod", Tenant: "business", Repo: "bifrost-platform", Task: o.Task, Attempt: o.Attempt,
		Op: op, A: o.A, Base: commitA, R: o.R, M0: o.M0, CommitterDate: o.CommitterDate.UTC().Format(time.RFC3339Nano),
		CandidateDigest: "sha256:" + strings.Repeat("1", 64), MaterialDigest: "sha256:" + strings.Repeat("2", 64),
		Waives: []string{"floor_soft"}, SoftAtRequest: []string{"floor_soft"}, Files: 1, Lines: 2,
		Rule: store.GrantRule{ID: "landing:business", Version: 1, SHA256: strings.Repeat("3", 64)}, ChecksVersion: 2,
		CI:      store.GrantCI{Pipeline: "bifrost-ci-platform", Run: "ci-platform-1", UID: "uid-1", CreatedAt: now.Format(time.RFC3339Nano), Status: "Succeeded", SHA: o.R},
		Purpose: store.LandingGrantPurpose, STGRelease: true, ApprovedAt: approved.Format(time.RFC3339Nano), GrantedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: approved.Add(store.LandingGrantTTL).Format(time.RFC3339Nano)}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: o.Task,
			Body: map[string]any{"text": "grant fixture", "landing_grant": g.Body()}})
		return err
	}))
	return g.GrantID
}

// T30 (route half): no route writes a grant or its use. The landing grant
// routes are the material (a read) and the Owner's revocation alone.
func TestNoRouteWritesAGrant(t *testing.T) {
	t.Parallel()
	var grantRoutes []string
	for _, rt := range walkRoutes(t) {
		if strings.Contains(rt.pattern, "grant") || strings.Contains(rt.pattern, "material") {
			grantRoutes = append(grantRoutes, rt.method+" "+rt.pattern)
		}
	}
	slices.Sort(grantRoutes)
	want := []string{"GET landing/{op}/material", "POST landing/grants/{grant_id}/revoke"}
	if !slices.Equal(grantRoutes, want) {
		t.Fatalf("grant routes %v, want %v", grantRoutes, want)
	}
}

// The revocation is the Owner's: every other caller is refused before the
// store, the Owner's revocation of the fixture's grant succeeds once, and
// a second answers "already".
func TestRevokeRouteIsTheOwners(t *testing.T) {
	t.Parallel()
	pool := pinWorld(t)
	s := server(t, pool)
	pinned := pinnedFixture(t, pool)
	code, exact := call(t, s, "agentd:coordinator-mbp", "POST", "claim/pinned", map[string]any{"host_id": "coordinator-mbp",
		"task_id": pinned, "idempotency_key": pinned + ":1", "free_slots": 1})
	if code != http.StatusOK {
		t.Fatalf("claim: %d %v", code, exact)
	}
	op := landingWait(t, pool, pinned, exact)
	grant := grantFixture(t, pool, op)
	for _, caller := range []string{"viewer", "operator", "agentd:coordinator-mbp", "seat:coordinator"} {
		if code, out := call(t, s, caller, "POST", "landing/grants/"+grant+"/revoke", nil); code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Errorf("%s revoked: %d %v", caller, code, out)
		}
	}
	code, out := call(t, s, "admin", "POST", "landing/grants/"+grant+"/revoke", nil)
	if code != http.StatusOK || out["revoked"] != true || out["already"] == true {
		t.Fatalf("the Owner's revocation: %d %v", code, out)
	}
	code, out = call(t, s, "admin", "POST", "landing/grants/"+grant+"/revoke", nil)
	if code != http.StatusOK || out["already"] != true {
		t.Fatalf("again: %d %v", code, out)
	}
	if code, out = call(t, s, "admin", "POST", "landing/grants/lg_nothing/revoke", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown grant: %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "landing/"+op+"/material", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("material without sources: %d %v", code, out)
	}
}
