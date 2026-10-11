package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func TestHiddenFieldsThroughTheRealApprovalChain(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----X-----END\nPRIVATE KEY-----QQQQQQQQ"
	hidden := []struct {
		name, raw string
		forbid    []string
	}{
		{"zwsp", "safe token=s3cr3\u200btvalue", []string{"s3cr3", "tvalue", "\u200b"}},
		{"rtl", "public\nhidden\u202e", []string{"public\nhidden", "\u202e"}},
		{"past clip", strings.Repeat("a", 160) + "\u202e", []string{strings.Repeat("a", 40), "\u202e"}},
		{"soft hyphen", "to\u00adken=QQQQQQQQ", []string{"QQQQQQQQ", "\u00ad"}},
	}
	for _, h := range hidden {
		t.Run(h.name, func(t *testing.T) {
			for _, kind := range []string{"created", "failed", "unknown", "not_executed", "reminder_waiting", "reminder_expiring"} {
				t.Run(kind, func(t *testing.T) {
					f := relay(t)
					svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
					requester := h.raw
					if strings.ContainsAny(requester, "\r\n") {
						// A request header cannot carry a raw newline. The
						// same payload still arrives as the reason, the name,
						// the thread, the work id and, on a result, the error.
						requester = "cursor\u202e"
					}
					code, id := createHiddenApproval(t, svc, h.raw, h.raw, requester, h.raw, h.raw)
					if code != http.StatusCreated {
						t.Fatalf("create = %d", code)
					}
					driveApproval(t, svc, id, kind, h.raw)
					svc.notices.Wait()
					assertPushes(t, f.got(), h.forbid, true)
					assertKind(t, f.got(), kind)
					if _, ok := svc.find(id); !ok {
						t.Fatal("record missing after the push")
					}
				})
			}
		})
	}

	t.Run("pem redacted", func(t *testing.T) {
		for _, kind := range []string{"created", "failed", "unknown", "not_executed"} {
			t.Run(kind, func(t *testing.T) {
				f := relay(t)
				svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
				code, id := createHiddenApproval(t, svc, pem, pem, "cursor", "slice", "W-65")
				if code != http.StatusCreated {
					t.Fatalf("create = %d", code)
				}
				driveApproval(t, svc, id, kind, pem)
				svc.notices.Wait()
				assertPushes(t, f.got(), []string{"QQQQQQQQ", "BEGIN PRIVATE"}, false)
			})
		}
	})

	t.Run("pem thread and work are refused and not pushed", func(t *testing.T) {
		f := relay(t)
		svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
		if code, _ := createHiddenApproval(t, svc, "because", "node-a", "cursor", pem, ""); code != http.StatusBadRequest {
			t.Fatalf("thread = %d", code)
		}
		if code, _ := createHiddenApproval(t, svc, "because", "node-a", "cursor", "", pem); code != http.StatusBadRequest {
			t.Fatalf("work = %d", code)
		}
		if len(f.got()) != 0 {
			t.Fatalf("refused create pushed %+v", f.got())
		}
	})

	t.Run("refused push leaves the approval", func(t *testing.T) {
		f := relay(t)
		svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
		code, id := createHiddenApproval(t, svc, "patch the node", "node-a", "cursor", "slice", "W-65")
		if code != http.StatusCreated {
			t.Fatalf("create = %d", code)
		}
		ds, err := notice.Send(context.Background(), notice.Message{Title: "Decide \u202e", Message: "body"})
		if err == nil || err.Error() != "unclean text" || ds[0].Result != notice.ResultFailed {
			t.Fatalf("door err %v deliveries %+v", err, ds)
		}
		if _, ok := svc.find(id); !ok {
			t.Fatal("refused push removed the approval")
		}
		if len(f.got()) != 1 || strings.Contains(f.got()[0].Title+f.got()[0].Message, "\u202e") {
			t.Fatalf("create push %+v", f.got())
		}
	})
}

func createHiddenApproval(t *testing.T, svc *Service, reason, name, requester, thread, work string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"action":           "cordon_node",
		"reason":           reason,
		"params":           map[string]any{"name": name},
		"requester_thread": thread,
		"work_id":          work,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/approvals", bytes.NewReader(body))
	req.Header.Set(SessionHeader, requester)
	rec := httptest.NewRecorder()
	svc.HandleCreate(rec, req)
	if rec.Code != http.StatusCreated {
		return rec.Code, ""
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return rec.Code, out.ID
}

func driveApproval(t *testing.T, svc *Service, id, kind, errText string) {
	t.Helper()
	if kind == "created" || kind == "reminder_waiting" || kind == "reminder_expiring" {
		if strings.HasPrefix(kind, "reminder") {
			base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
			at := base
			svc.SetClock(func() time.Time { return at })
			// The record was created on the previous clock. Reminders measure
			// from CreatedAt, so move that clock's view of now forward.
			rec, ok := svc.find(id)
			if !ok {
				t.Fatal("missing")
			}
			at = rec.CreatedAt.Add(4 * time.Hour)
			if kind == "reminder_expiring" {
				at = rec.ExpiresAt.Add(-2 * time.Hour)
			}
			svc.remindDue(context.Background())
		}
		return
	}
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) {
		switch kind {
		case "unknown":
			return nil, actions.Uncertain(errText)
		case "not_executed":
			return nil, actions.Transient(errText)
		default:
			return nil, errors.New(errText)
		}
	})
	out := svc.approve(context.Background(), id, "chat")
	if kind == "failed" && out.Body["status"] != StatusFailed {
		t.Fatalf("approve = %v", out.Body)
	}
	if kind == "unknown" && out.Body["status"] != StatusUnknown {
		t.Fatalf("approve = %v", out.Body)
	}
	if kind == "not_executed" {
		got, ok := svc.get(id)
		if !ok {
			t.Fatal("missing after requeue")
		}
		after := got.Execution.Deadline.Add(time.Second)
		svc.SetClock(func() time.Time { return after })
		if got, _ = svc.get(id); got.Status != StatusExpired {
			t.Fatalf("status = %s", got.Status)
		}
	}
}

func assertKind(t *testing.T, pushes []push, kind string) {
	t.Helper()
	var titles string
	for _, p := range pushes {
		titles += p.Title + "\n"
	}
	want := map[string]string{
		"created":           "Decide ·",
		"failed":            "failed",
		"unknown":           "result unknown",
		"not_executed":      "approved but never ran",
		"reminder_waiting":  "waiting",
		"reminder_expiring": "expires in",
	}[kind]
	if !strings.Contains(titles, want) {
		t.Fatalf("kind %s missing from %q", kind, titles)
	}
}

func assertPushes(t *testing.T, pushes []push, forbid []string, wantMarker bool) {
	t.Helper()
	if len(pushes) == 0 {
		t.Fatal("no push")
	}
	for _, p := range pushes {
		text := p.Title + "\n" + p.Message
		if !utf8.ValidString(text) || notice.HasHidden(text) || strings.ContainsRune(text, '\t') {
			t.Fatalf("unclean push title %q body %q", p.Title, p.Message)
		}
		for _, s := range forbid {
			if strings.Contains(text, s) {
				t.Fatalf("push contains %q\ntitle %q\nbody %q", s, p.Title, p.Message)
			}
		}
		if wantMarker && !strings.Contains(text, notice.WithheldMarker) {
			t.Fatalf("withheld field was not the marker\ntitle %q\nbody %q", p.Title, p.Message)
		}
		if strings.ContainsRune(p.Title, '\n') {
			t.Fatalf("newline in title %q", p.Title)
		}
	}
}
