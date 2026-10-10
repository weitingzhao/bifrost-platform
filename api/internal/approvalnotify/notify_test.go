package approvalnotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyCreatedSkipsWhenUnset(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	t.Cleanup(srv.Close)
	if err := NotifyCreated(context.Background(), Created{ID: "ap-1"}); err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("skipped notify still dialed a server")
	}
}

func TestNotifyCreatedPostsClickAndBearer(t *testing.T) {
	const token = "relay-token-value"
	var gotAuth, gotClick, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		var body struct {
			Title    string `json:"title"`
			Message  string `json:"message"`
			ClickURL string `json:"click_url"`
			Priority int    `json:"priority"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		gotClick = body.ClickURL
		if body.Title != "Approval needed" || body.Priority != 4 || !strings.Contains(body.Message, "sess-1") {
			t.Errorf("body: %+v", body)
		}
		if strings.Contains(body.Message, token) || strings.Contains(gotBody, token) {
			t.Error("token leaked into the ntfy body")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", token)

	if err := NotifyCreated(context.Background(), Created{
		ID: "ap-1", Action: "gitops_sync_app", Tier: "C", Requester: "sess-1",
	}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+token {
		t.Fatalf("auth %q", gotAuth)
	}
	if gotClick != ConsoleClickPrefix+"ap-1" {
		t.Fatalf("click %q", gotClick)
	}
}

func TestNotifyCreatedRelayFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	err := NotifyCreated(context.Background(), Created{ID: "ap-2", Action: "drain_node"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err %v", err)
	}
}

func TestNotifySendsMessageThroughTheSameRelay(t *testing.T) {
	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
		Priority int    `json:"priority"`
	}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "relay-token-value")
	if err := Notify(context.Background(), Message{Title: "Release policy expires in 23h", Message: "sign: release.sh policy sign"}); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer relay-token-value" || body.Title != "Release policy expires in 23h" ||
		body.ClickURL != ConsoleApprovals || body.Priority != 4 || !strings.Contains(body.Message, "policy sign") {
		t.Fatalf("auth=%q body=%+v", auth, body)
	}
}
