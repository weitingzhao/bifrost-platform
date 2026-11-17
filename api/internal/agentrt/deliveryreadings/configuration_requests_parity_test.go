package deliveryreadings_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The isolated fixture is not a platform migration and contains no user data.
func TestConfigurationRequestsFrozenQueryPG(t *testing.T) {
	t.Parallel()
	db := agentrttest.New(t)
	pool := db.Pool(t, db.Admin)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE SCHEMA agentrt; CREATE TABLE agentrt.mission(mission_id text,goal text,state text,created_at timestamptz)`)
	ok(t, err)
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	goals := []string{"other", "Delivery configuration: missing", "Delivery configuration: x\nnot-json", "Delivery configuration: x\n{}", "Delivery configuration: x\n{\"boundary_id\":1}", "Delivery configuration: x\n{\"boundary_id\":\"a\"}\nextra", "Delivery configuration: y\n{\"boundary_id\":\"b\",\"note\":\"<>&\"}"}
	for i, g := range goals {
		_, err = pool.Exec(ctx, `INSERT INTO agentrt.mission VALUES($1,$2,'proposed',$3)`, string(rune('a'+i)), g, now.Add(time.Duration(i%2)*time.Second))
		ok(t, err)
	}
	old := func(ctx context.Context, limit int) ([]map[string]any, error) {
		out := []map[string]any{}
		rows, err := pool.Query(ctx, `SELECT mission_id,goal,state,created_at FROM agentrt.mission WHERE starts_with(goal,'Delivery configuration: ') ORDER BY created_at DESC,mission_id DESC LIMIT $1`, limit)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, goal, state string
			var created time.Time
			if err = rows.Scan(&id, &goal, &state, &created); err != nil {
				return out, err
			}
			lines := strings.SplitN(goal, "\n", 3)
			if len(lines) < 2 {
				continue
			}
			var request map[string]string
			if json.Unmarshal([]byte(lines[1]), &request) != nil || request["boundary_id"] == "" {
				continue
			}
			out = append(out, map[string]any{"mission_id": id, "state": state, "created_at": created, "request": request})
		}
		return out, rows.Err()
	}
	for _, limit := range []int{-1, 0, 1, 2, 4, 20} {
		want, we := old(ctx, limit)
		got, ge := deliveryreadings.ConfigurationRequests(ctx, pool, limit)
		if !reflect.DeepEqual(want, got) || errorString(we) != errorString(ge) {
			t.Fatalf("limit %d: %#v %v != %#v %v", limit, want, we, got, ge)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	want, we := old(cancelled, 2)
	got, ge := deliveryreadings.ConfigurationRequests(cancelled, pool, 2)
	if !reflect.DeepEqual(want, got) || errorString(we) != errorString(ge) {
		t.Fatalf("cancel differs: %#v %v / %#v %v", want, we, got, ge)
	}
	// Keep the interface explicitly tied to pgx rows; callers retain transactions.
	var _ interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	} = pool
}
func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func errorString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
