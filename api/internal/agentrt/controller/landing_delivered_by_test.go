package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type reworkDBProbe struct{ attempts int }

func (d *reworkDBProbe) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.attempts++
	return nil, errors.New("rework database probe")
}

func TestReworkSeenSkipsInvalidOldAOnce(t *testing.T) {
	for _, oldA := range []string{"", "abcdef0", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("A", 40), strings.Repeat("g", 40)} {
		t.Run(fmt.Sprintf("old_a=%q", oldA), func(t *testing.T) {
			db := &reworkDBProbe{}
			var logs []string
			c := Controller{DB: db, Logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }}
			pairs := []store.DeliveredByPair{{Root: "root", Op: "op", OldA: oldA, By: "sibling", ByOp: "by-op", NewA: strings.Repeat("b", 40)}}
			keys := []string{"repo/root/by-op/" + oldA + "/" + pairs[0].NewA}
			c.deliveredBy.settle(keys[0], "not_comparable")
			for pass := 0; pass < 2; pass++ {
				if err := c.reworkSeenPass(context.Background(), "repo", pairs, keys, time.Now().Add(time.Duration(pass)*24*time.Hour)); err != nil {
					t.Fatal(err)
				}
				if db.attempts != 0 || len(logs) != 1 {
					t.Fatalf("pass %d: database attempts=%d, logs=%v", pass+1, db.attempts, logs)
				}
				if !strings.Contains(logs[0], "old_a") || !strings.Contains(logs[0], "not a full commit") {
					t.Fatalf("missing skip explanation: %v", logs)
				}
			}
		})
	}
}

func TestReworkSeenFullOldAStillAttemptsStore(t *testing.T) {
	db := &reworkDBProbe{}
	c := Controller{DB: db, Logf: func(string, ...any) { t.Error("valid old_a was skipped") }}
	pairs := []store.DeliveredByPair{{Root: "root", Op: "op", OldA: strings.Repeat("a", 40), By: "sibling", ByOp: "by-op", NewA: strings.Repeat("b", 40)}}
	keys := []string{"repo/root/by-op"}
	c.deliveredBy.settle(keys[0], "different")
	if err := c.reworkSeenPass(context.Background(), "repo", pairs, keys, time.Now()); err == nil || db.attempts != 1 {
		t.Fatalf("valid old_a did not reach store: attempts=%d err=%v", db.attempts, err)
	}
}
