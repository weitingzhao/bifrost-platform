package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

type conclusionBeginSpy struct{ calls int }

var errConclusionBeginReached = errors.New("begin reached without a database")

func (s *conclusionBeginSpy) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	s.calls++
	return nil, errConclusionBeginReached
}

// Review 737 R-01: a canonical address must survive the sender's second parse.
func TestConclusionValidLongThreadReachesSender(t *testing.T) {
	t.Parallel()
	for _, n := range []int{36, 121, 122, 128, 129} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			id := "local_" + strings.Repeat("a", n-len("local_"))
			actor, err := store.ThreadActor(id)
			if n > 128 {
				if err == nil {
					t.Fatal("invalid session accepted")
				}
				actor = "thread:" + id
			} else if err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{id, actor, id + " (W-31)", actor + " (W-31)"} {
				to := threadinbox.Recipient(label)
				if n <= 128 && to != actor || n > 128 && to != "" {
					t.Errorf("source selected %q for %q", to, label)
				}
				spy := &conclusionBeginSpy{}
				err = store.PostConclusion(context.Background(), spy, "question:q_boundary:answered", to, "answer")
				if n <= 128 && (spy.calls != 1 || !errors.Is(err, errConclusionBeginReached)) {
					t.Errorf("valid thread silently skipped: session=%d calls=%d err=%v", n, spy.calls, err)
				}
				if n > 128 && (spy.calls != 0 || err != nil) {
					t.Errorf("invalid thread reached database: calls=%d err=%v", spy.calls, err)
				}
			}
		})
	}
}

func TestConclusionLongRegisteredThreadPG(t *testing.T) {
	t.Parallel()
	for _, n := range []int{121, 122, 128} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			db, conn, _ := conclusionDB(t)
			id := "local_" + strings.Repeat("a", n-len("local_"))
			actor, err := store.ThreadActor(id)
			ok(t, err)
			w := store.Writer{Actor: actor, Token: "long-session"}
			_, _, err = store.UploadArtifact(ctx, conn, w, "message", "text/plain", []byte("register"))
			ok(t, err)
			for _, key := range []string{"approval:appr_long:executed", "question:q_long:answered"} {
				for _, label := range []string{id, actor, id + " (W-31)", actor + " (W-31)"} {
					ok(t, store.PostConclusion(ctx, conn, key, threadinbox.Recipient(label), key))
				}
			}
			mail, err := store.Unread(ctx, conn, w, 100)
			ok(t, err)
			if len(mail) != 2 || mail[0].Body != "approval:appr_long:executed" || mail[1].Body != "question:q_long:answered" {
				t.Fatalf("long actor mailbox=%+v", mail)
			}
			var receipts int
			ok(t, db.Connect(t, db.Reader).QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key
			  WHERE route='thread-conclusion' AND expires_at='infinity'`).Scan(&receipts))
			if receipts != 2 {
				t.Fatalf("permanent receipts=%d", receipts)
			}
		})
	}
}
