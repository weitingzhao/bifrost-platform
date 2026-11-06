package approvals

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func grantRequestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	return New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
}

func grantRequestParams() map[string]any {
	task := "t_00000000000000000000000001"
	return map[string]any{"env": "prod", "repo": "bifrost-platform", "task": task, "op": "land:" + task + ":1",
		"R": strings.Repeat("a", 40), "material_digest": "sha256:" + strings.Repeat("b", 64), "stg_release": false}
}

// R5:5.7 goal 3: only the landing grant actions may be requested in process.
func TestGrantRequestRefusesOtherActions(t *testing.T) {
	svc := grantRequestService(t)
	checked := false
	svc.SetCreateCheck(func(context.Context, string, map[string]any) error {
		checked = true
		return nil
	})
	for _, action := range []string{"apply_manifest", "owner_run_command", "", "approve_core_landing_x"} {
		_, err := svc.Request(context.Background(), "controller", action, "why", "", map[string]any{})
		var re *RequestError
		if !errors.As(err, &re) || re.Status != 403 {
			t.Fatalf("%q: %v", action, err)
		}
	}
	if checked {
		t.Fatal("a refused action reached the create check")
	}
	if list, _ := svc.Observations(); len(list) != 0 {
		t.Fatalf("stored %d", len(list))
	}
}

// The create check runs as for POST /approvals: its refusal stores nothing;
// accepted, the request is pending, by the requester given.
func TestGrantRequestRunsCreateCheck(t *testing.T) {
	svc := grantRequestService(t)
	var seen []string
	refuse := true
	svc.SetCreateCheck(func(_ context.Context, action string, params map[string]any) error {
		seen = append(seen, action+" "+params["op"].(string))
		if refuse {
			return errors.New("material_mismatch")
		}
		return nil
	})
	_, err := svc.Request(context.Background(), "controller", "approve_guardrail_landing", "waits on a grant", "reject", grantRequestParams())
	var re *RequestError
	if !errors.As(err, &re) || re.Status != 422 || !strings.Contains(err.Error(), "material_mismatch") {
		t.Fatalf("refused: %v", err)
	}
	if list, _ := svc.Observations(); len(list) != 0 {
		t.Fatalf("a refused request was stored: %d", len(list))
	}
	refuse = false
	for _, action := range []string{"approve_guardrail_landing", "approve_core_landing"} {
		p := grantRequestParams()
		if action == "approve_core_landing" {
			p["op"] = "land:t_00000000000000000000000001:2"
		}
		a, err := svc.Request(context.Background(), "controller", action, "waits on a grant", "reject", p)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if a.Action != action || a.Status != StatusPending || a.Requester != "controller" || a.Tier != "D" || a.Number < 1 {
			t.Fatalf("%s: %+v", action, a)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("create check ran %d times, want 3: %v", len(seen), seen)
	}
}

// A pending landing request whose operation went on without it is withdrawn
// off the Owner's list (expired, with the reason); a decided one, or one of
// another action, is not.
func TestGrantRequestWithdraw(t *testing.T) {
	svc := grantRequestService(t)
	a, err := svc.Request(context.Background(), "controller", "approve_core_landing", "why", "", grantRequestParams())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Withdraw(a.ID, ""); err == nil {
		t.Fatal("withdrawn without a reason")
	}
	if err := svc.Withdraw(a.ID, "landed on the rule"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := svc.Lookup(a.ID)
	if got.Status != StatusExpired || !strings.Contains(got.Error, "landed on the rule") || len(svc.Pending()) != 0 {
		t.Fatalf("%+v", got)
	}
	if err := svc.Withdraw(a.ID, "again"); err != nil {
		t.Fatal("expired twice", err)
	}
	b, err := svc.Request(context.Background(), "controller", "approve_guardrail_landing", "why", "", grantRequestParams())
	if err != nil {
		t.Fatal(err)
	}
	if d := svc.reject(b.ID, "no"); d.Status != 200 {
		t.Fatal(d)
	}
	var re *RequestError
	if err := svc.Withdraw(b.ID, "landed"); !errors.As(err, &re) || re.Status != 409 {
		t.Fatal("a rejected request withdrawn", err)
	}
	if err := svc.Withdraw("nope", "landed"); !errors.As(err, &re) || re.Status != 404 {
		t.Fatal(err)
	}
}
