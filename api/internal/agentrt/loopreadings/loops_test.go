package loopreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"testing"
	"time"
)

type row struct {
	values []any
	err    error
}

func (r row) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, v := range r.values {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type query struct {
	pgx.Tx
	r    row
	args []any
}

func (q *query) QueryRow(_ context.Context, _ string, args ...any) pgx.Row { q.args = args; return q.r }
func TestLatestDistinguishesMissingRecoveryAndFailure(t *testing.T) {
	at := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	failure := errors.New("db failed")
	for _, tc := range []struct {
		kind         string
		err          error
		found, alert bool
	}{{"controller_loop_failing", nil, true, true}, {"controller_loop_recovered", nil, true, false},
		// The rows before migration 0029 (2026-11-17) are still read as before.
		{"policy_alert", nil, true, true}, {"note", nil, true, false}, {"", pgx.ErrNoRows, false, false}, {"", failure, false, false}} {
		q := &query{r: row{values: []any{tc.kind, at, "artifact"}, err: tc.err}}
		got, err := Latest(context.Background(), q, "controller", "digest", "loop passes again")
		if got.Found != tc.found || got.Alert != tc.alert || tc.err == failure && !errors.Is(err, failure) || tc.err != failure && err != nil || !reflect.DeepEqual(q.args, []any{"controller", "digest", "loop passes again"}) {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
func TestFailingStableOrderAndErrors(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	q := &query{r: row{values: []any{[]byte("error body")}}}
	latest := func(_ context.Context, _ pgx.Tx, name string) (Record, error) {
		return Record{Alert: name != "healthy", At: at, Artifact: name}, nil
	}
	got, err := Failing(ctx, q, []string{"second", "healthy", "first"}, latest)
	if err != nil || len(got) != 2 || got[0]["loop"] != "second" || got[1]["loop"] != "first" || got[0]["since"] != "2026-11-16T07:00:00Z" || got[0]["error"] != "error body" {
		t.Fatal(got, err)
	}
	empty, err := Failing(ctx, q, nil, latest)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	failure := errors.New("read failed")
	q.r.err = failure
	if got, err := Failing(ctx, q, []string{"one"}, latest); got != nil || !errors.Is(err, failure) {
		t.Fatal(got, err)
	}
	if got, err := Failing(ctx, q, []string{"one"}, func(context.Context, pgx.Tx, string) (Record, error) { return Record{}, failure }); got != nil || !errors.Is(err, failure) {
		t.Fatal(got, err)
	}
}
