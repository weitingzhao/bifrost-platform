package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate/k8sstatetest"
)

// Serial: installs the production statefile backend with client-go's default
// rate limiter; only the HTTP transport is replaced, as in review 731.
func TestThreadInboxK8sBacklogDoesNotBlockAnswer(t *testing.T) {
	defer statefile.Use(nil, "")
	for _, backlog := range []bool{false, true} {
		t.Run(fmt.Sprint(backlog), func(t *testing.T) {
			// Serial: global statefile backend.
			now := time.Now().UTC()
			q := Question{ID: "q_aaaaaaaaaaaaaaaa", Number: 1, Status: StatusOpen,
				Requester: "test", RequesterThread: "local_review", Title: "Proceed?",
				Options: []Option{{Key: "a", Label: "Proceed"}}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
			doc := legacyInboxDocument{document: document{Questions: []Question{q}}, Inbox: map[string]legacyInboxEntry{}}
			if backlog {
				for i := 0; i < 1574; i++ {
					id := fmt.Sprintf("q_%016x", i)
					doc.Inbox["question:"+id+":answered"] = legacyInboxEntry{Record: id, Order: uint64(i + 1), To: "thread:local_review", Body: strings.Repeat("x", 390)}
				}
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			backend, transport := k8sstatetest.New(t, "questions", raw)
			root := t.TempDir()
			statefile.Use(backend, root)
			svc := New(filepath.Join(root, "questions"), nil)
			answer := func() Result {
				return svc.Answer(context.Background(), "console", "owner", "admin", q.ID, []byte(`{"option":"a","channel":"console"}`))
			}
			if backlog {
				// A restarted writer must retry a failed business CAS without
				// exposing the uncommitted answer or losing data.
				transport.FailBusiness = 1
				if res := answer(); res.Code != 500 {
					t.Fatalf("CAS failure control: %+v", res)
				}
				saved, found, err := svc.Lookup(q.ID)
				if err != nil || !found || saved.Status != StatusOpen {
					t.Fatal("failed write changed answer")
				}
				svc = New(svc.store.path, nil)
			}
			before := transport.Requests
			started := time.Now()
			res := answer()
			elapsed := time.Since(started)
			requests := transport.Requests - before
			saved, found, err := svc.Lookup(q.ID)
			t.Logf("bytes=%d elapsed=%s requests=%d result=%d status=%s", len(raw), elapsed, requests, res.Code, saved.Status)
			if res.Code != 200 || err != nil || !found || saved.Status != StatusAnswered {
				t.Fatalf("backlog blocked answer: %+v %v", res, err)
			}
			if requests > 8 || elapsed > 5*time.Second {
				t.Fatal("answer performed backlog-sized work")
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
				if err := svc.store.update(func(doc *document) error { doc.Questions[0].Title = "After archive failure"; return nil }); err != nil {
					t.Fatal(err)
				}
				saved, found, err = New(svc.store.path, nil).Lookup(q.ID)
				if err != nil || !found || saved.Status != StatusAnswered {
					t.Fatal("archive failure changed answer")
				}
			}
			statefile.Use(nil, "")
		})
	}
}
