package approvalnotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// TestPinRedact records Redact as it behaves today. The expected strings are
// the contract for the later move into notice.
func TestPinRedact(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"c0", "\u0007", "\u0007"},
		{"u202e", "\u202e", "\u202e"},
		{"u200b", "\u200b", "\u200b"},
		{"newline", "\n", "\n"},
		{"secret", "token=s3cr3tvalue", "token=[redacted]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// pinNow and pinItem are the one approval whose Decide and Check texts are pinned.
var pinNow = time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)

func pinItem() Item {
	return Item{
		ID: "appr_pin0000000001", Number: 7, Action: "gitops_sync_app", Tier: "C", Env: "prod",
		Summary:   "Sync \u202e app token=s3cr3tvalue",
		KeyParams: map[string]string{"name": "app-prod", "command": "rm -rf secret"},
		Requester: "cursor", Thread: "slice zero", WorkID: "W-65", Runner: "platform",
		CreatedAt: pinNow.Add(-time.Hour), ExpiresAt: pinNow.Add(23 * time.Hour),
		Error: "relay token=s3cr3tvalue",
	}
}

// TestPinApprovalNoticeText records one Decide push and one Check push.
func TestPinApprovalNoticeText(t *testing.T) {
	it := pinItem()
	click := ConsoleClickPrefix + url.QueryEscape(it.ID)
	for _, tc := range []struct {
		kind     string
		title    string
		body     string
		priority int
	}{
		{KindCreated,
			"Decide · #7 · Sync \u202e app token=[redacted]",
			"Runs as soon as you approve.\n" +
				"Sync \u202e app token=[redacted]\n" +
				"name=app-prod\n" +
				"from W-65 · slice zero (cursor) · runs on: platform · expires in 23h",
			4},
		{KindFailed,
			"Check · #7 · failed · Sync \u202e app token=[redacted]",
			"error: relay token=[redacted]\n" +
				"Nothing else was run.\n" +
				"Sync \u202e app token=[redacted]\n" +
				"from W-65 · slice zero (cursor) · runs on: platform",
			4},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			m := Compose(tc.kind, it, pinNow)
			if m.Title != tc.title || m.Message != tc.body || m.Priority != tc.priority || m.ClickURL != click {
				t.Fatalf("title %q\nbody %q\npriority %d click %q", m.Title, m.Message, m.Priority, m.ClickURL)
			}
		})
	}
}

// TestPinRelayWithholdsTheHiddenRows is the same Decide and Check inputs as
// TestPinApprovalNoticeText. Compose still returns those bytes. The relay
// receives the server-written fallback, because the summary contains U+202E.
func TestPinRelayWithholdsTheHiddenRows(t *testing.T) {
	it := pinItem()
	click := ConsoleClickPrefix + url.QueryEscape(it.ID)
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	for _, tc := range []struct {
		kind, title string
	}{
		{KindCreated, "Decide · #7"},
		{KindFailed, "Check · #7"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ds := Send(context.Background(), tc.kind, it, pinNow)
			var posted struct {
				Title    string `json:"title"`
				Message  string `json:"message"`
				ClickURL string `json:"click_url"`
				Priority int    `json:"priority"`
			}
			if err := json.Unmarshal(raw, &posted); err != nil {
				t.Fatal(err)
			}
			if posted.Title != tc.title || posted.Message != notice.WithheldBody || posted.ClickURL != click || posted.Priority != 4 {
				t.Fatalf("%+v", posted)
			}
			if strings.Contains(string(raw), "Sync") || strings.Contains(string(raw), "\u202e") || strings.Contains(string(raw), "s3cr3t") || strings.Contains(string(raw), "token=") {
				t.Fatalf("composed text was posted: %s", raw)
			}
			if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Kind != tc.kind || !ds[0].At.Equal(pinNow) || ds[0].Error != "" {
				t.Fatalf("delivery %+v", ds)
			}
		})
	}
}
