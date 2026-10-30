package approvals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sourceCapacityFixture(t *testing.T, businessOnly bool, state string) (*Service, []byte, int) {
	t.Helper()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	exit := 0
	if state == StatusFailed {
		exit = 1
	}
	a := Approval{ID: "appr_capacity", Number: 1, Action: "review_fixture", Status: StatusUnknown,
		RequesterThread: "local_review", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Execution: &Execution{LeaseID: "fixture"}}
	finished := a
	finished.Status = state
	if state == StatusFailed {
		finished.Error = "exit 1"
	}
	finished.Execution = &Execution{LeaseID: "fixture", FinishedAt: now, ExitCode: &exit, LateResult: true}
	expected := file{Approvals: []Approval{finished}, extra: map[string]json.RawMessage{"padding": json.RawMessage(`""`)}}
	if businessOnly {
		expected.extra = nil
		for i := 0; i < 400; i++ {
			expected.Approvals = append(expected.Approvals, Approval{ID: fmt.Sprintf("appr_open_%03d", i), Number: i + 2,
				Action: "review_fixture", Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
		}
	}
	raw, err := expected.marshalOnce()
	if err != nil {
		t.Fatal(err)
	}
	fill := maxDocBytes - len(raw) - 60
	padding, err := json.Marshal(strings.Repeat("x", fill))
	if err != nil {
		t.Fatal(err)
	}
	doc := file{Approvals: []Approval{a}, extra: map[string]json.RawMessage{"padding": padding}}
	if businessOnly {
		doc.extra = nil
		for i := 0; i < 400; i++ {
			other := expected.Approvals[i+1]
			n := fill / 400
			if i < fill%400 {
				n++
			}
			other.Reason = strings.Repeat("x", n)
			doc.Approvals = append(doc.Approvals, other)
		}
	}

	raw, err = doc.marshalOnce()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxDocBytes {
		t.Fatalf("business input exceeds budget: %d", len(raw))
	}
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	s.SetClock(func() time.Time { return now })
	return s, raw, exit
}

// The 734 A-03 fixture retains its production byte boundary and success
// control. Expected output hashes come from the same mutation on f0aa82bb.
func TestThreadInboxSourceHeadCannotPruneLateResult(t *testing.T) {
	t.Parallel()
	golden, err := os.ReadFile("testdata/thread_inbox_capacity_f0aa82bb.json")
	if err != nil {
		t.Fatal(err)
	}
	var hashes map[string]string
	if err := json.Unmarshal(golden, &hashes); err != nil {
		t.Fatal(err)
	}
	for _, businessOnly := range []bool{false, true} {
		for _, state := range []string{StatusExecuted, StatusFailed} {
			for _, pending := range []bool{false, true} {
				t.Run(fmt.Sprintf("business=%t/%s/pending=%t", businessOnly, state, pending), func(t *testing.T) {
					t.Parallel()
					s, raw, exit := sourceCapacityFixture(t, businessOnly, state)
					var err error
					if !pending {
						var doc map[string]json.RawMessage
						if err := json.Unmarshal(raw, &doc); err != nil {
							t.Fatal(err)
						}
						doc["thread_inbox"] = json.RawMessage(`{"approval:appr_capacity:unknown":{"record":"appr_capacity","done":true}}`)
						raw, err = writeRawObject(doc)
						if err != nil {
							t.Fatal(err)
						}
					}
					if len(raw) > maxDocBytes {
						t.Fatalf("fixture input exceeds budget: %d", len(raw))
					}
					if err := os.WriteFile(s.store.path, raw, 0600); err != nil {
						t.Fatal(err)
					}
					res := s.result("appr_capacity", resultInput{LeaseID: "fixture", ExitCode: &exit})
					saved, found, err := s.Lookup("appr_capacity")
					if res.Status != 200 || err != nil || !found || saved.Status != state {
						t.Fatalf("late result lost: response=%+v found=%t status=%s err=%v", res, found, saved.Status, err)
					}
					output, err := os.ReadFile(s.store.path)
					if err != nil {
						t.Fatal(err)
					}
					sum := sha256.Sum256(output)
					if hex.EncodeToString(sum[:]) != hashes[fmt.Sprintf("%t/%s", businessOnly, state)] {
						t.Fatal("business result differs byte-for-byte from f0aa82bb")
					}
					if strings.Contains(string(output), "thread_inbox") {
						t.Fatal("delivery field added")
					}
					restarted := New(s.store.path, nil)
					mail := map[string]string{}
					send := dedupeInboxSender(func(_ context.Context, key, _, body string) error { mail[key] = body; return nil })
					for i := 0; i < 2; i++ {
						if err := restarted.retryThreadInbox(context.Background(), send); err != nil {
							t.Fatal(err)
						}
					}
					if len(mail) != 2 || !strings.Contains(mail["approval:appr_capacity:"+state], "Conclusion: "+state) || !strings.Contains(mail["approval:appr_capacity:unknown"], "Conclusion: unknown") {
						t.Fatalf("late conclusions missing: %v", mail)
					}
					if strings.Contains(mail["approval:appr_capacity:unknown"], "Exit code:") {
						t.Fatal("earlier unknown copied the later result")
					}
					t.Logf("input=%d output=%d response=%d retained=%s", len(raw), len(output), res.Status, saved.Status)
				})
			}
		}
	}
}
