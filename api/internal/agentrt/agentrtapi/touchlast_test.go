package agentrtapi

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// stuckDB is a database whose every transaction waits until its context ends,
// as one held up by a pool or a lock would; it keeps the contexts it saw.
type stuckDB struct {
	store.DB
	seen chan context.Context
}

func (d stuckDB) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	d.seen <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

// Review 527 B3: the last record of a prune call's contact, made once the
// call is answered or cancelled, has a deadline of its own: a database that
// does not answer holds the finished call no longer than touchLast.
func TestTheLastTouchHasADeadline(t *testing.T) {
	defer func(d time.Duration) { touchLast = d }(touchLast)
	touchLast = 50 * time.Millisecond
	db := stuckDB{seen: make(chan context.Context, 4)}
	a := &API{DB: db}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan func(), 1)
	go func() { started <- a.keepTouching(ctx, "h1") }()
	<-db.seen // the first touch, held until the request is cancelled
	cancel()
	finish := <-started
	done := make(chan struct{})
	start := time.Now()
	go func() { finish(); close(done) }()
	last := <-db.seen
	if _, has := last.Deadline(); !has {
		t.Fatal("the last touch has no deadline")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the finished call waited on the database past its deadline")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the finished call waited %v", took)
	}
}
