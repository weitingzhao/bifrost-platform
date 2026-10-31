package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

type unresponsiveAssessmentDB struct{ calls atomic.Int32 }

func (db *unresponsiveAssessmentDB) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	db.calls.Add(1)
	<-ctx.Done() // A stalled database or an exhausted connection pool.
	return nil, ctx.Err()
}

// Exercise the real handlers, assessmentReader and ReadAssessment without PG.
// Optional advice must not consume the ordinary Console read's 15s deadline.
func TestAssessmentListsReturnWithinBudgetWhenDatabaseDoesNotRespond(t *testing.T) {
	for _, kind := range []string{store.AssessApproval, store.AssessCard} {
		for _, count := range []int{8, 32} {
			t.Run(fmt.Sprintf("%s/%d", kind, count), func(t *testing.T) {
				db := &unresponsiveAssessmentDB{}
				s := &Server{agentRuntime: &agentRuntime{api: &agentrtapi.API{DB: db}}}
				key := "approvals"
				if kind == store.AssessCard {
					key = "questions"
				}
				path := filepath.Join(t.TempDir(), key)
				records := make([]any, count)
				for i := range records {
					if kind == store.AssessApproval {
						records[i] = approvals.Approval{ID: fmt.Sprintf("a_%d", i+1), Number: i + 1,
							Status: approvals.StatusPending, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
							Params: map[string]any{"material_digest": "unchanged"}, ParamsHash: "unchanged"}
					} else {
						// Use the persisted fixture shape; only questions_wire.go names
						// the question package, preserving the server dependency boundary.
						records[i] = map[string]any{"id": fmt.Sprintf("q_%d", i+1), "number": i + 1,
							"status": "open", "title": "Choose a direction", "created_at": time.Now(), "expires_at": time.Now().Add(time.Hour)}
					}
				}
				raw, err := json.Marshal(map[string]any{key: records})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				var handler http.HandlerFunc
				if kind == store.AssessApproval {
					s.approvals = approvals.New(path, nil)
					s.approvals.Assessment = s.assessmentReader(kind)
					handler = s.approvals.HandleList
				} else {
					s.questionRuntime = newQuestionRuntime(path, nil)
					s.wireQuestionAssessments()
					handler = s.questionRuntime.svc.HandleList
				}
				// A one-second outer deadline also bounds a regression's test runtime.
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				req := httptest.NewRequest(http.MethodGet, "/api/v1/"+key, nil).WithContext(ctx)
				req.Header.Set("X-Bifrost-Session", "owner-console")
				response := httptest.NewRecorder()
				start := time.Now()
				handler(response, req)
				elapsed := time.Since(start)
				if elapsed >= 750*time.Millisecond {
					t.Errorf("optional assessment exhausted the list budget: %s", elapsed)
				}
				if calls := db.calls.Load(); calls < 1 || calls > 4 {
					t.Errorf("unbounded or missing assessment reads: %d", calls)
				}
				var body map[string][]struct {
					ID         string            `json:"id"`
					Assessment map[string]string `json:"assessment"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK || len(body[key]) != count {
					t.Fatalf("base items unavailable: %d %s (%v)", response.Code, response.Body.String(), err)
				}
				seen := make(map[string]bool)
				for _, item := range body[key] {
					if item.ID == "" || seen[item.ID] || item.Assessment["error"] == "" {
						t.Fatalf("missing item or failure reason: %+v", item)
					}
					seen[item.ID] = true
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, after) {
					t.Fatalf("optional read changed base records/material: %v", err)
				}
				t.Logf("%d base items returned in %s with %d bounded reads", count, elapsed, db.calls.Load())
			})
		}
	}
}
