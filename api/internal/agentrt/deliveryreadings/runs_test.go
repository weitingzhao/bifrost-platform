package deliveryreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"testing"
)

type fakeRows struct {
	pgx.Rows
	data            [][]byte
	index           int
	closed          bool
	scanErr, endErr error
}

func (r *fakeRows) Next() bool { return r.index < len(r.data) }
func (r *fakeRows) Scan(dst ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dst[0].(*[]byte) = r.data[r.index]
	r.index++
	return nil
}
func (r *fakeRows) Close()     { r.closed = true }
func (r *fakeRows) Err() error { return r.endErr }

type fakeTx struct {
	pgx.Tx
	rows *fakeRows
	err  error
	args []any
}

func (t *fakeTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	t.args = args
	return t.rows, t.err
}
func TestRunsPreserveOrderTimesAndEmpty(t *testing.T) {
	rows := &fakeRows{data: [][]byte{[]byte(`{"run":"second","repo":"demo","result":"Failed","created_at":"2026-11-16T01:00:00-06:00"}`), []byte(`{"run":"first","pipeline":"ci","kind":"ci","revision":"abc","started_at":"2026-11-16T07:01:00Z","completed_at":"2026-11-16T07:02:00Z"}`)}}
	tx := &fakeTx{rows: rows}
	got, err := Runs(context.Background(), tx, "abc")
	if err != nil || len(got) != 2 || got[0].Run != "second" || got[0].Repo != "demo" || got[0].CreatedAt.UTC().Hour() != 7 || got[1].Revision != "abc" || got[1].CompletedAt.Minute() != 2 || !rows.closed || !reflect.DeepEqual(tx.args, []any{"abc"}) {
		t.Fatalf("%+v %v", got, err)
	}
	tx.rows = &fakeRows{}
	got, err = Runs(context.Background(), tx, "abc")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
func TestRunReadErrorsAndPartialIteration(t *testing.T) {
	failure := errors.New("read failed")
	for _, stage := range []string{"query", "scan", "decode", "iteration"} {
		t.Run(stage, func(t *testing.T) {
			rows := &fakeRows{data: [][]byte{[]byte(`{"run":"one"}`)}}
			tx := &fakeTx{rows: rows}
			switch stage {
			case "query":
				tx.err = failure
			case "scan":
				rows.scanErr = failure
			case "decode":
				rows.data[0] = []byte(`{`)
				rows.endErr = failure
			case "iteration":
				rows.endErr = failure
			}
			got, err := Runs(context.Background(), tx, "abc")
			if err == nil || stage != "query" && !rows.closed {
				t.Fatal(got, err)
			}
			if stage == "decode" {
				if errors.Is(err, failure) || got != nil {
					t.Fatal("decode precedence")
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if stage == "iteration" && len(got) != 1 {
				t.Fatal("partial iteration result changed")
			}
		})
	}
}
