package approvalnotify

import (
	"net/url"
	"testing"
	"time"
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
		// The summary contains U+202E, so the whole summary is the withheld
		// marker. The error has no hidden character and is redacted as before.
		{KindCreated,
			"Decide · #7 · [withheld: hidden characters]",
			"Runs as soon as you approve.\n" +
				"[withheld: hidden characters]\n" +
				"name=app-prod\n" +
				"from W-65 · slice zero (cursor) · runs on: platform · expires in 23h",
			4},
		{KindFailed,
			"Check · #7 · failed · [withheld: hidden characters]",
			"error: relay token=[redacted]\n" +
				"Nothing else was run.\n" +
				"[withheld: hidden characters]\n" +
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
