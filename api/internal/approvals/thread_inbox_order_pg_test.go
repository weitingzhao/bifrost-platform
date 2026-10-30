package approvals

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	runtime "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

var errOrderReceipt = errors.New("conclusion receipt unavailable")

type orderReceiptFailureDB struct{ runtime.DB }
type orderReceiptFailureTx struct{ pgx.Tx }

func (db orderReceiptFailureDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return orderReceiptFailureTx{tx}, nil
}

func (tx orderReceiptFailureTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "INSERT INTO agentrt.idempotency_key") {
		return pgconn.CommandTag{}, errOrderReceipt
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

// Exercise real transactional receipts, separate process-local cursors and
// pools. Fail after message writes but before the receipt, so rollback is real.
func TestThreadInboxLateConclusionOrderPG(t *testing.T) {
	t.Parallel()
	for _, state := range []string{StatusExecuted, StatusFailed} {
		for _, fault := range []string{"before_commit", "lost_reply", "persistent"} {
			for _, replicas := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/%s/replicas=%d", state, fault, replicas), func(t *testing.T) {
					t.Parallel()
					lateConclusionOrderPG(t, state, fault, replicas)
				})
			}
		}
	}
}

func lateConclusionOrderPG(t *testing.T, state, fault string, replicas int) {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := runtime.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	w := runtime.Writer{Actor: "thread:local_approval", Token: "order-test"}
	if _, _, err := runtime.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register")); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	s := lateInboxOrderFixture(t, now, state)
	services := []*Service{s}
	if replicas == 2 {
		services = append(services, New(s.store.path, nil))
	}
	senders := make([]threadinbox.Sender, len(services))
	for i, service := range services {
		conn := db.Pool(t, db.Runtime)
		first := true
		senders[i] = func(ctx context.Context, key, to, body string) error {
			if key == "approval:appr_unknown:unknown" && (first || fault == "persistent") {
				first = false
				if fault == "lost_reply" {
					if err := runtime.PostConclusion(ctx, conn, key, to, body); err != nil {
						return err
					}
					return errOrderReceipt // committed; only the caller lost its reply
				}
				return runtime.PostConclusion(ctx, orderReceiptFailureDB{conn}, key, to, body)
			}
			return runtime.PostConclusion(ctx, conn, key, to, body)
		}
		if err := service.retryThreadInbox(ctx, senders[i]); !errors.Is(err, errOrderReceipt) {
			t.Fatalf("replica %d fault not exercised: %v", i, err)
		}
	}
	mail, err := runtime.Unread(ctx, pool, w, 100)
	wantInitial := 0
	if fault == "lost_reply" {
		wantInitial = 1
	}
	if err != nil || len(mail) != wantInitial {
		t.Fatalf("fault left partial/duplicate messages: mail=%+v err=%v", mail, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(services)*2)
	start := make(chan struct{})
	for i, service := range services {
		wg.Add(1)
		safego.Go("test-conclusion-order-replica", func() {
			defer wg.Done()
			<-start
			for pass := 0; pass < 2; pass++ {
				errs <- service.retryThreadInbox(ctx, senders[i])
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	attempts := 0
	for err := range errs {
		attempts++
		if fault == "persistent" {
			if !errors.Is(err, errOrderReceipt) {
				t.Fatalf("persistent failure hidden: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if attempts != len(services)*2 {
		t.Fatalf("replica stopped early: attempts=%d", attempts)
	}
	mail, err = runtime.Unread(ctx, pool, w, 100)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 3
	if fault == "persistent" {
		wantCount = 1
	}
	if len(mail) != wantCount {
		t.Fatalf("mailbox=%+v wantCount=%d", mail, wantCount)
	}
	var late []string
	healthy := false
	for _, m := range mail {
		if strings.Contains(m.Body, "appr_zhealthy") {
			healthy = true
		}
		if strings.Contains(m.Body, "appr_unknown") {
			for _, status := range []string{StatusUnknown, state} {
				if strings.Contains(m.Body, "Conclusion: "+status) {
					late = append(late, status)
				}
			}
		}
	}
	var want []string
	if fault != "persistent" {
		want = []string{StatusUnknown, state}
	}
	if !healthy || !reflect.DeepEqual(late, want) {
		t.Fatalf("healthy=%t late=%v want=%v", healthy, late, want)
	}
	var receipts int
	if err := db.Connect(t, db.Reader).QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key
	  WHERE route='thread-conclusion' AND expires_at='infinity'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != wantCount {
		t.Fatalf("permanent receipts=%d want=%d", receipts, wantCount)
	}
}
