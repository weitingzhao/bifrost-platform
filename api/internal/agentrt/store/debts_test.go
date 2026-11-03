package store_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Technical debt, 5.8-3a's data layer (DESIGN-tech-debt-runtime-2026-11-01
// §2.4, §10 row 5.8-3a): numbering per environment, the floor, the rule a
// filing needs, and the guards of migration 0011. Project keys are made up:
// no product's name goes into the platform (T3).

const debtProjectsText = `rule_id: debt-projects
rule_version: 1
direction: initial
effective_at: 2026-01-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
projects:
  - {key: crew, name: "Crew", group: crew, owns: ["bifrost-platform/**"]}
  - {key: alpha, name: "Alpha", group: product, owns: ["alpha/**"]}
`

// debtWorld is a world with the debt-projects rule in force (or not), a
// crew thread and the Owner.
type debtWorld struct {
	*world
	crew, owner store.Writer
	keys        atomic.Int64
}

func newDebtWorld(t *testing.T, rule bool) *debtWorld {
	t.Helper()
	dw := &debtWorld{world: newWorld(t), crew: wr("thread:crew-1"), owner: store.Writer{Actor: "owner", Token: "admin"}}
	if rule {
		dw.putRule(t)
	}
	return dw
}

func (dw *debtWorld) putRule(t *testing.T) {
	t.Helper()
	(&issueWorld{world: dw.world}).putRule(t, debtProjectsText)
}

func (dw *debtWorld) key() string { return fmt.Sprintf("dk-%d", dw.keys.Add(1)) }

func debtFiling(title string) store.DebtFiling {
	return store.DebtFiling{Project: "crew", Title: title, Priority: "P2",
		Texts:    []store.DebtText{{Kind: "claim", Body: "what is wrong: " + title}},
		Evidence: []store.DebtEvidence{{Kind: "code_ref", Repo: "bifrost-platform", Path: "api/internal/x.go", LineFrom: 10, Quote: "x := 1"}}}
}

// file files a debt as the crew in env on db and returns its number.
func (dw *debtWorld) file(t *testing.T, db store.DB, env, title string) (string, error) {
	t.Helper()
	out, err := store.FileDebt(ctx, db, env, dw.crew, dw.key(), debtFiling(title))
	if err != nil {
		return "", err
	}
	return out["debt_id"].(string), nil
}

func (dw *debtWorld) mustFile(t *testing.T, env, title string) string {
	t.Helper()
	id, err := dw.file(t, dw.rt, env, title)
	ok(t, err)
	return id
}

// runtimeAs is a connection as another environment's runtime role, given
// on this test database, broadly, the dev runtime role's kind of privileges
// (the database was migrated for dev): it shows what the TD- guard does by
// current_user, not what 0011 grants (the migrate package's tests check
// that). The role is the cluster's: created when the test cluster lacks it,
// as TestEachEnvironmentGrantsItsOwnRuntimeOnly does.
func (dw *debtWorld) runtimeAs(t *testing.T, role string) *pgx.Conn {
	t.Helper()
	admin := dw.db.Connect(t, dw.db.Admin)
	id := pgx.Identifier{role}.Sanitize()
	for _, q := range []string{
		`DO $$ BEGIN CREATE ROLE ` + id + ` LOGIN; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$`,
		`GRANT CONNECT ON DATABASE ` + pgx.Identifier{dw.db.Name}.Sanitize() + ` TO ` + id,
		`GRANT USAGE ON SCHEMA agentrt TO ` + id,
		`GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA agentrt TO ` + id,
		`GRANT USAGE ON ALL SEQUENCES IN SCHEMA agentrt TO ` + id,
		`GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA agentrt TO ` + id,
	} {
		_, err := admin.Exec(ctx, q)
		ok(t, err)
	}
	return dw.db.Connect(t, dw.db.Admin+" user="+role)
}

func (dw *debtWorld) prodRuntime(t *testing.T) *pgx.Conn {
	return dw.runtimeAs(t, "platform_agentrt_prod")
}

func debtCount(t *testing.T, db *pgx.Conn, pattern string) int {
	t.Helper()
	var n int
	ok(t, db.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt WHERE debt_id LIKE $1`, pattern).Scan(&n))
	return n
}

// STG issues TDS-n and DEV TDD-n, each from the floor (488) up and each its
// own sequence; neither ever issues TD- (T2: the store's half); no other
// environment issues anything.
func TestDebtStgAndDevNeverIssueTD(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	for i, want := range []string{"TDS-489", "TDS-490"} {
		if got := dw.mustFile(t, "stg", fmt.Sprintf("stg debt %d", i)); got != want {
			t.Fatalf("STG filing %d got %s, want %s", i, got, want)
		}
	}
	if got := dw.mustFile(t, "dev", "dev debt"); got != "TDD-489" {
		t.Fatalf("DEV filing got %s, want TDD-489", got)
	}
	for _, env := range []string{"", "qa", "PROD"} {
		_, err := dw.file(t, dw.rt, env, "nowhere")
		wantIs(t, err, store.ErrInvalid, "is not dev, stg or prod")
	}
	if n := debtCount(t, dw.rt, "TD-%"); n != 0 {
		t.Fatalf("%d TD- numbers were issued outside PROD", n)
	}
	v, err := store.DebtState(ctx, dw.rt, "TDS-489")
	ok(t, err)
	if v.State != "open" || v.Flow != "manual" || v.Version != 0 || v.Project != "crew" || v.CreatedBy != dw.crew.Actor {
		t.Fatalf("a filed debt: %+v", v)
	}
	if _, err := store.VerifyStream(ctx, dw.rt, store.DebtStream("TDS-489")); err != nil {
		t.Fatal(err)
	}
}

// Filing needs the environment's debt-projects rule in force, whichever the
// environment (§2.4); on PROD, with the rule, only PROD's runtime role gets
// its TD- number past the database (T2: the store's PROD path end to end).
func TestDebtProdFilingNeedsTheRuleAndTheRole(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, false)
	prod := dw.prodRuntime(t)
	for _, env := range []string{"prod", "stg"} {
		_, err := dw.file(t, prod, env, "too early")
		wantIs(t, err, store.ErrState, "no debt-projects rule is in force")
	}
	dw.putRule(t)
	f := debtFiling("not ours")
	f.Project = "omega"
	_, err := store.FileDebt(ctx, prod, "prod", dw.crew, dw.key(), f)
	wantIs(t, err, store.ErrInvalid, "not one the debt-projects rule lists")
	// The dev runtime told it is PROD: the database refuses the number.
	_, err = dw.file(t, dw.rt, "prod", "wrong role")
	wantErr(t, err, "only PROD's runtime (platform_agentrt_prod) issues TD- numbers")
	got, err := dw.file(t, prod, "prod", "the first real one")
	ok(t, err)
	if got != "TD-489" {
		t.Fatalf("PROD's first number is %s, want TD-489", got)
	}
	if n := debtCount(t, dw.rt, "TD-%"); n != 1 {
		t.Fatalf("%d TD- debts, want 1", n)
	}
}

// The same key and request answer the same number and write nothing more;
// the same key with another request is a conflict; a filing that fails
// writes nothing and takes no number.
func TestDebtFilingIsIdempotent(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	f := debtFiling("once")
	first, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, "same", f)
	ok(t, err)
	again, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, "same", f)
	ok(t, err)
	if first["debt_id"] != "TDS-489" || again["debt_id"] != first["debt_id"] {
		t.Fatalf("first %v, again %v", first["debt_id"], again["debt_id"])
	}
	f.Title = "twice"
	_, err = store.FileDebt(ctx, dw.rt, "stg", dw.crew, "same", f)
	wantIs(t, err, store.ErrConflict, "")
	bad := debtFiling("a local path")
	bad.Texts[0].Body = "see ~/scratch/notes.txt"
	_, err = store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), bad)
	wantIs(t, err, store.ErrInvalid, "names a local path")
	bad = debtFiling("no evidence")
	bad.Evidence = nil
	_, err = store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), bad)
	wantIs(t, err, store.ErrInvalid, "at least one piece of evidence")
	if got := dw.mustFile(t, "stg", "next"); got != "TDS-490" {
		t.Fatalf("after a replay and three refusals the next number is %s, want TDS-490", got)
	}
	if n := debtCount(t, dw.rt, "TDS-%"); n != 2 {
		t.Fatalf("%d debts, want 2", n)
	}
}

// Filings at once, by two threads, on separate connections, never share a
// number and leave none out (§2.4: two sessions in the same second).
func TestDebtConcurrentFilingsNeverCollide(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	pool := dw.db.Pool(t, dw.db.Runtime)
	const n = 12
	ids := make([]string, n)
	errs := make([]error, n)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			w := wr(fmt.Sprintf("thread:filer-%d", i%2))
			out, err := store.FileDebt(ctx, pool, "stg", w, fmt.Sprintf("c-%d", i), debtFiling(fmt.Sprintf("at once %d", i)))
			if errs[i] = err; err == nil {
				ids[i] = out["debt_id"].(string)
			}
		}()
	}
	start.Done()
	wg.Wait()
	for _, err := range errs {
		ok(t, err)
	}
	var want []string
	for i := range n {
		want = append(want, fmt.Sprintf("TDS-%d", 489+i))
	}
	slices.Sort(ids) // three digits each: text order is number order
	if !slices.Equal(ids, want) {
		t.Fatalf("numbers %v, want %v", ids, want)
	}
}

// The floor: no number at or below it is issued; it only rises, by the Owner
// or the runtime; a number above it already there wins (max of the two).
func TestDebtFloorIsRespected(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	_, err := store.RaiseDebtFloor(ctx, dw.rt, dw.crew, dw.key(), 600)
	wantIs(t, err, store.ErrInvalid, "does not raise the debt floor")
	out, err := store.RaiseDebtFloor(ctx, dw.rt, dw.owner, dw.key(), 600)
	ok(t, err)
	if fmt.Sprint(out["from"]) != "488" || fmt.Sprint(out["floor"]) != "600" {
		t.Fatalf("raised %v", out)
	}
	for _, floor := range []int{550, 600} {
		_, err = store.RaiseDebtFloor(ctx, dw.rt, dw.owner, dw.key(), floor)
		wantIs(t, err, store.ErrState, "it only rises")
	}
	if got := dw.mustFile(t, "stg", "above the floor"); got != "TDS-601" {
		t.Fatalf("after the floor rose to 600 the next number is %s, want TDS-601", got)
	}
	// A number above the floor already filed (as an import would) wins.
	ok(t, dw.debtEvent(t, "TDS-900", "debt_filed", "controller", map[string]any{"source": "import", "project": "crew",
		"priority": "P3", "state": "doing", "flow": "manual"}, insertDebtSQL("TDS-900", "doing", "controller")))
	if got := dw.mustFile(t, "stg", "after an import"); got != "TDS-901" {
		t.Fatalf("with TDS-900 there the next number is %s, want TDS-901", got)
	}
	if got := dw.mustFile(t, "dev", "its own prefix"); got != "TDD-601" {
		t.Fatalf("DEV's next number is %s, want TDD-601 (the floor, not STG's numbers)", got)
	}
	// Past the store: a thread does not raise it, nobody lowers it, nobody
	// adds a second row; the runtime raises it.
	floor := func(actor string, to int) error {
		return store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.DebtCounterStream, Type: "debt_floor_raised", Actor: actor,
				Body: map[string]any{"floor": to, "from": 600}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.debt_counter SET floor = $1, version = version + 1`, to)
			return err
		})
	}
	wantErr(t, floor(dw.crew.Actor, 700), "does not write a debt_floor_raised event")
	wantErr(t, floor("owner", 500), "only rises")
	wantErr(t, floor("controller", 600), "only rises")
	ok(t, floor("controller", 700))
	wantErr(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.DebtCounterStream, Type: "debt_floor_raised", Actor: "owner",
			Body: map[string]any{"floor": 800, "from": 700}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.debt_counter (debt_counter_id, floor) VALUES (1, 800)`)
		return err
	}), "the one row 0011 wrote")
}

// debtEvent writes one event on a debt's stream (naming it) and then the
// statements, in one transaction, as the runtime role: a writer past the
// store.
func (dw *debtWorld) debtEvent(t *testing.T, debt, typ, actor string, body map[string]any, stmts ...string) error {
	t.Helper()
	return debtEventOn(t, dw.rt, debt, typ, actor, body, stmts...)
}

func debtEventOn(t *testing.T, conn *pgx.Conn, debt, typ, actor string, body map[string]any, stmts ...string) error {
	t.Helper()
	body["debt_id"] = debt
	var appendErr error
	err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.DebtStream(debt), Type: typ, Actor: actor, Body: body}); err != nil {
			appendErr = err
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if appendErr != nil {
		t.Fatalf("the case's event does not append: %v", appendErr)
	}
	return err
}

// insertDebtSQL is a debt row as a filing writes it, in state.
func insertDebtSQL(id, state, actor string) string {
	return fmt.Sprintf(`INSERT INTO agentrt.debt (debt_id, project, title, priority, state, flow, filed_at, state_since, created_by_actor_id)
	  VALUES ('%s', 'crew', 'past the store', 'P3', '%s', 'manual', now(), now(), '%s')`, id, state, actor)
}

// The database's half of T2: a TD- number only by platform_agentrt_prod,
// whatever the store says; TDS- and TDD- by any runtime role.
func TestDebtTDPrefixOnlyByProdRole(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	prod := dw.prodRuntime(t)
	body := func() map[string]any {
		return map[string]any{"source": "file", "project": "crew", "priority": "P3", "state": "open", "flow": "manual"}
	}
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-1', 'thread', 'crew')`)
	wantErr(t, dw.debtEvent(t, "TD-1000", "debt_filed", dw.crew.Actor, body(), insertDebtSQL("TD-1000", "open", "thread:crew-1")),
		"only PROD's runtime (platform_agentrt_prod) issues TD- numbers, not platform_agentrt_dev")
	wantErr(t, debtEventOn(t, dw.runtimeAs(t, "platform_agentrt_stg"), "TD-1000", "debt_filed", dw.crew.Actor, body(),
		insertDebtSQL("TD-1000", "open", "thread:crew-1")), "not platform_agentrt_stg")
	ok(t, debtEventOn(t, prod, "TD-1000", "debt_filed", dw.crew.Actor, body(), insertDebtSQL("TD-1000", "open", "thread:crew-1")))
	ok(t, dw.debtEvent(t, "TDS-1000", "debt_filed", dw.crew.Actor, body(), insertDebtSQL("TDS-1000", "open", "thread:crew-1")))
	ok(t, dw.debtEvent(t, "TDD-1000", "debt_filed", dw.crew.Actor, body(), insertDebtSQL("TDD-1000", "open", "thread:crew-1")))
}

// A filing with an acceptance spec and parts flows automatically (§6.1);
// with either missing, by hand; a spec outside the closed grammar of §6.4 is
// refused.
func TestDebtFilingFlow(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	spec := []any{map[string]any{"kind": "ci_test", "repo": "bifrost-platform", "path": "api/internal/x_test.go", "name": "TestX"},
		map[string]any{"kind": "promql", "query": "up", "expect": map[string]any{"eq": 1}}}
	for _, c := range []struct {
		repos []string
		spec  []any
		flow  string
	}{{[]string{"bifrost-platform", "example-infra"}, spec, "auto"}, {nil, spec, "manual"}, {[]string{"bifrost-platform"}, nil, "manual"}} {
		f := debtFiling("flow " + c.flow)
		f.Repos, f.Acceptance = c.repos, c.spec
		out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
		ok(t, err)
		if out["flow"] != c.flow {
			t.Errorf("repos %v, spec %v: flow %v, want %s", c.repos, c.spec != nil, out["flow"], c.flow)
		}
	}
	var parts int
	ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_part WHERE debt_id = 'TDS-489'`).Scan(&parts))
	if parts != 2 {
		t.Fatalf("TDS-489 has %d parts, want 2", parts)
	}
	for name, bad := range map[string]any{
		"an unknown kind":     map[string]any{"kind": "shell", "say": "rm -rf"},
		"an unknown key":      map[string]any{"kind": "manual", "say": "look", "expect": "fine", "run": "now"},
		"no comparison":       map[string]any{"kind": "promql", "query": "up", "expect": map[string]any{"near": 1}},
		"a missing test name": map[string]any{"kind": "ci_test", "repo": "bifrost-platform", "path": "x_test.go"},
	} {
		f := debtFiling(name)
		f.Repos, f.Acceptance = []string{"bifrost-platform"}, []any{bad}
		_, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
		if !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}
