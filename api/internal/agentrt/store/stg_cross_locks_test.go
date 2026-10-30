package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This is a SQL boundary contract, not a claim to simulate PostgreSQL locks.
// The PG matrix separately proves contention using pg_blocking_pids.
type b5hLockTx struct {
	pgx.Tx
	trace       []string
	failMission bool
}

func (x *b5hLockTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	key := "capability"
	switch {
	case strings.Contains(query, "hashtext"):
		key = "rule/" + fmt.Sprint(args[1])
	case strings.Contains(query, "agentrt.plan_node"):
		key = "step/" + fmt.Sprint(args[0])
	case strings.Contains(query, "agentrt.task"):
		key = "task/" + fmt.Sprint(args[0])
	case strings.Contains(query, "agentrt.mission"):
		key = "mission/" + fmt.Sprint(args[0])
	}
	x.trace = append(x.trace, key)
	if strings.HasPrefix(key, "mission/") && x.failMission {
		return pgconn.CommandTag{}, errors.New("mission lock unavailable")
	}
	return pgconn.CommandTag{}, nil
}
func TestB5HBindingLocksIncludeEveryMission(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			t.Parallel()
			tx := &b5hLockTx{failMission: fail}
			bindings := map[string]stgBinding{
				"t-b": {Root: "t-b", Tenant: "b", Step: "s-b", Mission: "m-b"},
				"t-a": {Root: "t-a", Tenant: "a", Step: "s-a", Mission: "m-a"},
				"t-c": {Root: "t-c", Tenant: "a", Step: "s-a", Mission: "m-a"},
			}
			err := stgLockBindings(context.Background(), tx, bindings)
			want := []string{"capability", "rule/landing:a", "rule/landing:b", "step/s-a", "step/s-b", "task/t-a", "task/t-b", "task/t-c", "mission/m-a"}
			if !fail {
				want = append(want, "mission/m-b")
			}
			if (err != nil) != fail || !reflect.DeepEqual(tx.trace, want) {
				t.Fatalf("binding lock identities/order or failure propagation: got=%v want=%v err=%v", tx.trace, want, err)
			}
		})
	}
}
