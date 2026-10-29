package approvals

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// Review 694 R-02: at most one open request per subject is the store's own
// rule, checked in the update that inserts. Two requests whose pre-create
// checks both ran before either was stored (the barrier) still leave one
// open request; once it is closed, another may be made. A drill holds no
// subject. (On dff96c3c the check ran outside the store: both were 201.)
func TestC5R02OneOpenRequestPerSubject(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "prod")
	dir := t.TempDir()
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	svc.SetOpenKey(func(action string, p map[string]any) string {
		if action != "approve_core_landing" && action != "approve_guardrail_landing" {
			return ""
		}
		op, _ := p["op"].(string)
		return "landing-grant:" + op
	})
	var both sync.WaitGroup
	both.Add(2)
	barrier := true
	svc.SetCreateCheck(func(context.Context, string, map[string]any) error {
		if barrier {
			both.Done()
			both.Wait()
		}
		return nil
	})
	task := "t_00000000000000000000000001"
	params := func(op string) map[string]any {
		return map[string]any{"env": "prod", "repo": "bifrost-platform", "task": task, "op": op,
			"R": strings.Repeat("a", 40), "material_digest": "sha256:" + strings.Repeat("b", 64), "stg_release": true}
	}
	result := make(chan createResult, 2)
	for _, action := range []string{"approve_core_landing", "approve_guardrail_landing"} {
		go func() {
			result <- svc.create(context.Background(), "coordinator", action, "review 694", "revert", params("land:"+task+":1"))
		}()
	}
	first, second := <-result, <-result
	if (first.Status == 201) == (second.Status == 201) || first.Status+second.Status != 201+409 {
		t.Fatalf("statuses %d, %d", first.Status, second.Status)
	}
	barrier = false
	open := first
	if open.Status != 201 {
		open = second
	}
	// Another operation is another subject.
	if r := svc.create(context.Background(), "coordinator", "approve_core_landing", "review 694", "revert", params("land:"+task+":2")); r.Status != 201 {
		t.Fatalf("another op: %d %v", r.Status, r.Body)
	}
	// Closed, the subject is free again.
	if d := svc.reject(open.Approval.ID, "not now"); d.Status != 200 {
		t.Fatalf("reject: %d %v", d.Status, d.Body)
	}
	if r := svc.create(context.Background(), "coordinator", "approve_core_landing", "review 694", "revert", params("land:"+task+":1")); r.Status != 201 {
		t.Fatalf("after the close: %d %v", r.Status, r.Body)
	}
}
