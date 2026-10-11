package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func TestLookAndPolicyFallbacks(t *testing.T) {
	if LookFallback != "Look · thread" || notice.HasHidden(LookFallback) {
		t.Fatal(LookFallback)
	}
	msg := agentThreadNotice("Look · "+mark+"\u202e", "body "+mark, "thread", "claude/t")
	if msg.Fallback != LookFallback || msg.ClickURL != ConsoleNeedsYou || msg.Kind != "thread" || msg.ID != "claude/t" {
		t.Fatalf("%+v", msg)
	}
	policy := releasePolicyNotice("Release policy expires in 23h", "Sign a new one")
	if policy.Fallback != "" || policy.Kind != "release_policy" || notice.HasHidden(policy.Title) {
		t.Fatalf("%+v", policy)
	}

	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	if err := approvalnotify.Notify(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	var posted struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
	}
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != "Look · thread" || posted.Message != notice.WithheldBody || posted.ClickURL != ConsoleNeedsYou || strings.Contains(string(raw), mark) {
		t.Fatalf("look %q", raw)
	}
	hidden := releasePolicyNotice("Release \u202e policy", "Sign")
	if err := approvalnotify.Notify(context.Background(), hidden); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != notice.WithheldTitle || posted.Message != notice.WithheldBody || strings.Contains(string(raw), "Release") || strings.Contains(string(raw), "\u202e") {
		t.Fatalf("policy %q", raw)
	}
	clean := releasePolicyNotice("Release policy expires in 23h", "Sign a new one")
	if err := approvalnotify.Notify(context.Background(), clean); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != clean.Title || posted.Message != clean.Message {
		t.Fatalf("clean policy %q", raw)
	}
}

const mark = "Ω"
