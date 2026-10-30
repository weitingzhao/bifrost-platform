package approvals

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Review 737 R-02: use the real unknown -> late-result business path and
// retain the same scanner across retries, including its scheduling cursor.
func TestThreadInboxLateConclusionOrderAfterFailure(t *testing.T) {
	t.Parallel()
	for _, state := range []string{StatusExecuted, StatusFailed} {
		for _, fault := range []string{"none", "before_commit", "deadline", "lost_reply", "between_states", "persistent"} {
			t.Run(fmt.Sprintf("%s/%s", state, fault), func(t *testing.T) {
				t.Parallel()
				s := lateInboxOrderFixture(t, time.Now().UTC(), state)
				ctx, cancel := context.WithCancel(context.Background())
				if fault == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				injected := false
				receipts := map[string]bool{}
				var committed, late []string
				finalAttempts := 0
				send := func(ctx context.Context, key, to, body string) error {
					unknown := key == "approval:appr_unknown:unknown"
					if key == "approval:appr_unknown:"+state {
						finalAttempts++
					}
					if unknown && (!injected || fault == "persistent") {
						injected = true
						switch fault {
						case "before_commit", "persistent":
							return errors.New("temporary failure before commit")
						case "deadline":
							<-ctx.Done()
							return ctx.Err()
						}
					}
					if !receipts[key] {
						receipts[key] = true
						committed = append(committed, key)
						if strings.HasPrefix(key, "approval:appr_unknown:") {
							late = append(late, key)
						}
						if unknown {
							switch fault {
							case "lost_reply":
								return errors.New("reply lost after commit")
							case "between_states":
								cancel()
							}
						}
					}
					return nil
				}
				for i := 0; i < 3; i++ {
					passCtx := context.Background()
					if i == 0 {
						passCtx = ctx
					}
					err := s.retryThreadInbox(passCtx, send)
					wantErr := fault == "persistent" || i == 0 && fault != "none"
					if (err != nil) != wantErr {
						t.Fatalf("pass %d: err=%v wantErr=%t", i, err, wantErr)
					}
					if i == 0 && fault == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("deadline not exercised: %v", err)
					}
				}
				if !receipts["approval:appr_zhealthy:rejected"] {
					t.Fatalf("failed record starved a later record: %v", committed)
				}
				if fault == "persistent" {
					if len(late) != 0 || finalAttempts != 0 {
						t.Fatalf("final attempted without successful predecessor: late=%v attempts=%d", late, finalAttempts)
					}
					return
				}
				want := []string{"approval:appr_unknown:unknown", "approval:appr_unknown:" + state}
				if !reflect.DeepEqual(late, want) || len(committed) != 3 {
					t.Fatalf("committed=%v late=%v want=%v", committed, late, want)
				}
			})
		}
	}
}

func lateInboxOrderFixture(t *testing.T, now time.Time, state string) *Service {
	t.Helper()
	s := inboxFixture(t, now, StatusUnknown, "local_approval")
	transitionInboxFixture(t, s, StatusUnknown)
	exit := 0
	if state == StatusFailed {
		exit = 1
	}
	if res := s.result("appr_unknown", resultInput{LeaseID: "private-lease", ExitCode: &exit}); res.Status != 200 {
		t.Fatalf("late result=%+v", res)
	}
	if err := s.store.update(func(doc *file) error {
		doc.put(Approval{ID: "appr_zhealthy", Number: 8, Status: StatusRejected, RequesterThread: "local_approval"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s
}
