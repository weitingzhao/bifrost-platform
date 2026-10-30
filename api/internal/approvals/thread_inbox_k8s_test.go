package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate/k8sstatetest"
)

// Serial: installs the process-wide backend. This is review 731's real
// client-go/default limiter + production statefile deadline counterexample.
func TestThreadInboxK8sBacklogDoesNotBlockDecision(t *testing.T) {
	defer statefile.Use(nil, "")
	for _, state := range []string{StatusRejected, StatusExecuted, StatusFailed, StatusApproved} {
		for _, backlog := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/backlog=%t", state, backlog), func(t *testing.T) {
				// Serial: process-wide backend.
				now := time.Now().UTC()
				a := Approval{ID: "appr_review", Number: 1, Action: "cordon_node", Env: "stg", Status: StatusPending,
					RequesterThread: "local_review", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
				doc := legacyInboxDocument{file: file{Approvals: []Approval{a}}, Inbox: map[string]legacyInboxEntry{}}
				if backlog {
					for i := 0; i < 3100; i++ {
						old := a
						old.ID = fmt.Sprintf("appr_%016x", i)
						old.Number = i + 2
						old.Status = StatusFailed
						doc.Inbox["approval:"+old.ID+":failed"] = legacyInboxEntry{Record: old.ID, Order: uint64(i + 1), To: "thread:local_review", Body: approvalConclusion(old)}
					}
				}
				if state == StatusExecuted || state == StatusFailed {
					doc.Approvals[0].Status = StatusRunning
					doc.Approvals[0].Execution = &Execution{LeaseID: "fixture", LeaseExpiresAt: now.Add(time.Hour)}
				}
				if state == StatusApproved {
					doc.Approvals[0].Runner = actions.RunnerOwner
					doc.Approvals[0].ParamsHash, _ = actions.ParamsHash(doc.Approvals[0].Params)
				}
				raw, err := doc.marshalOnce()
				if err != nil {
					t.Fatal(err)
				}
				backend, transport := k8sstatetest.New(t, "approvals", raw)
				root := t.TempDir()
				statefile.Use(backend, root)
				svc := New(filepath.Join(root, "approvals"), nil)
				decide := func() decided {
					switch state {
					case StatusApproved:
						return svc.approve(context.Background(), a.ID, "console")
					case StatusRejected:
						return svc.reject(a.ID, "not now")
					default:
						exit := 0
						if state == StatusFailed {
							exit = 1
						}
						return svc.result(a.ID, resultInput{LeaseID: "fixture", ExitCode: &exit})
					}
				}
				started := time.Now()
				res := decide()
				elapsed := time.Since(started)
				requests := transport.Requests
				saved, found, err := svc.Lookup(a.ID)
				t.Logf("bytes=%d elapsed=%s requests=%d status=%d record=%s", len(raw), elapsed, requests, res.Status, saved.Status)
				if res.Status >= 300 || err != nil || !found || saved.Status != state {
					t.Fatalf("backlog blocked business: %+v %v", res, err)
				}
				if requests > 9 || elapsed > 5*time.Second {
					t.Fatal("business write performed backlog-sized work")
				}
				if backlog {
					if len(transport.Objects) != 1 {
						t.Fatal("business write created auxiliary delivery state")
					}
					transport.FailArchive = true
					readStart := transport.Requests
					if err := svc.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error { return nil }); err != nil {
						t.Fatal(err)
					}
					if transport.Requests-readStart != 1 {
						t.Fatal("scan did more than read the business document")
					}
					// Force a primary business write failure, then retry with all auxiliary I/O unavailable.
					transport.FailBusiness = 1
					update := func(doc *file) error { doc.Approvals[0].Reason = "after archive failure"; return nil }
					if err := svc.store.update(update); err == nil {
						t.Fatal("business CAS fault not exercised")
					}
					transport.FailArchive = true
					if err := svc.store.update(update); err != nil {
						t.Fatal(err)
					}
					// Overwrite a terminal source while archive I/O is still unavailable.
					if err := svc.store.update(func(current *file) error {
						current.Approvals[0] = doc.Approvals[0]
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if res := decide(); res.Status >= 300 {
						t.Fatalf("archive failure blocked decision: %+v", res)
					}
					restarted := New(svc.store.path, nil)
					saved, found, err = restarted.Lookup(a.ID)
					if err != nil || !found || saved.Status != state {
						t.Fatal("retry lost the decision")
					}
					raw, err := statefile.ReadFile(svc.store.path)
					if err != nil || strings.Contains(string(raw), "appr_0000000000000000") {
						t.Fatal("legacy backlog still occupies business document")
					}
					var current map[string]json.RawMessage
					if json.Unmarshal(raw, &current) != nil || len(current["thread_inbox_source"]) != 0 || len(current["thread_inbox"]) != 0 {
						t.Fatal("delivery metadata entered the business document")
					}
				}
				statefile.Use(nil, "")
			})
		}
	}
}
