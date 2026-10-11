package agentthreads

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func TestHiddenBeatThroughTheHandlerAndWatcher(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----X-----END\nPRIVATE KEY-----QQQQQQQQ"
	cases := []struct {
		name, title, tool, work, vendor, host, thread string
		forbid                                        []string
		marker                                        bool
	}{
		{"zwsp", "safe token=s3cr3\u200btvalue", "safe token=s3cr3\u200btvalue", "W-54", "claude", "mbp", "thread-a", []string{"s3cr3", "tvalue", "\u200b"}, true},
		{"rtl", "public\nhidden\u202e", "Bash", "W-54", "claude", "mbp", "thread-b", []string{"public\nhidden", "\u202e"}, true},
		{"past clip", strings.Repeat("a", 160) + "\u202e", "Bash", "W-54", "claude", "mbp", "thread-c", []string{strings.Repeat("a", 40), "\u202e"}, true},
		{"soft hyphen", "to\u00adken=QQQQQQQQ", "Bash", "W-54", "claude", "mbp", "thread-d", []string{"QQQQQQQQ", "\u00ad"}, true},
		{"pem title", pem, "Bash", "W-54", "claude", "mbp", "thread-e", []string{"QQQQQQQQ", "BEGIN PRIVATE"}, false},
		{"pem tool", "reading", pem, "W-54", "claude", "mbp", "thread-f", []string{"QQQQQQQQ", "BEGIN PRIVATE"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "")
			h := NewHandler(r.rec, DefaultConfig(), nil, nil)
			body := `{"thread":"` + tc.thread + `","vendor":"` + tc.vendor + `","host":"` + tc.host + `","work":"` + tc.work + `","title":` + jsonString(tc.title) + `,"tool":` + jsonString(tc.tool) + `,"event":"turn_start","turn_id":"turn-1","seq":1}`
			rec := postBeat(h, body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("beat %d %s", rec.Code, rec.Body.String())
			}
			var got string
			r.w.notify = func(ctx context.Context, title, body string) error {
				got = title + "\n" + body
				return approvalnotify.Notify(ctx, approvalnotify.Message{Title: title, Message: body})
			}
			r.at(11 * time.Minute)
			r.tick(t)
			if got == "" {
				t.Fatal("no look notice")
			}
			assertNotice(t, got, tc.forbid, tc.marker)
			if _, ok := mustState(t, r).Threads[Key(tc.vendor, tc.thread)]; !ok {
				t.Fatal("thread missing after the push")
			}
		})
	}

	t.Run("hidden vendor host and thread", func(t *testing.T) {
		r := newRig(t, "")
		h := NewHandler(r.rec, DefaultConfig(), nil, nil)
		raw := "safe token=s3cr3\u200btvalue"
		body := `{"thread":` + jsonString(raw) + `,"vendor":` + jsonString(raw) + `,"host":"mbp","event":"turn_start","turn_id":"turn-1","seq":1}`
		rec := postBeat(h, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("beat %d %s", rec.Code, rec.Body.String())
		}
		var got string
		r.w.notify = func(_ context.Context, title, body string) error {
			got = title + "\n" + body
			return nil
		}
		r.at(11 * time.Minute)
		r.tick(t)
		assertNotice(t, got, []string{"s3cr3", "tvalue", "\u200b"}, true)
	})

	t.Run("hidden host notice", func(t *testing.T) {
		r := newRig(t, "")
		h := NewHandler(r.rec, DefaultConfig(), nil, nil)
		raw := "box\u202e"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/hosts/heartbeat", bytes.NewReader([]byte(`{"host":`+jsonString(raw)+`,"vendors":{"claude":{"wired":true,"token":true}}}`)))
		h.HandleHost(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("host %d %s", rec.Code, rec.Body.String())
		}
		if err := r.rec.Flush(); err != nil {
			t.Fatal(err)
		}
		var got string
		r.w.notify = func(_ context.Context, title, body string) error {
			got = title + "\n" + body
			return nil
		}
		r.at(3*time.Minute + time.Second)
		r.tick(t)
		assertNotice(t, got, []string{"box", "\u202e"}, true)
		if len(mustState(t, r).Hosts) != 1 {
			t.Fatal("host missing after the push")
		}
	})

	t.Run("lineage title past the clip", func(t *testing.T) {
		r := newRig(t, "")
		r.beat(t, TurnStart, "", 0)
		lineage := strings.Repeat("a", 160) + "\u202e"
		r.w.titles = func(context.Context) (map[string]string, error) {
			return map[string]string{"7e939cd5-b3b1-4705-96e4-0a3b9e61648d": lineage}, nil
		}
		var got string
		r.w.notify = func(_ context.Context, title, body string) error {
			got = title + "\n" + body
			return nil
		}
		r.at(11 * time.Minute)
		r.tick(t)
		assertNotice(t, got, []string{strings.Repeat("a", 40), "\u202e"}, true)
	})

	t.Run("refused push leaves the thread", func(t *testing.T) {
		r := newRig(t, "")
		r.beat(t, TurnStart, "", 0)
		r.w.notify = func(ctx context.Context, _, _ string) error {
			_, err := notice.Send(ctx, notice.Message{Title: "Look \u0007", Message: "ok"})
			return err
		}
		r.at(11 * time.Minute)
		if r.w.tick(context.Background()) {
			t.Fatal("refused push was treated as sent")
		}
		k := Key("claude", "7e939cd5-b3b1-4705-96e4-0a3b9e61648d")
		if _, ok := mustState(t, r).Threads[k]; !ok {
			t.Fatal("refused push removed the thread")
		}
	})
}

func TestLookAndHostNoticesPassTheDoor(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := at.Add(11 * time.Minute)
	title, body := DefaultConfig().Message(Thread{
		Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
		Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120,
		At: at, TurnStartedAt: at,
	}, now)
	ds, err := notice.Send(context.Background(), notice.Message{Title: title, Message: body})
	if err != nil || ds[0].Result != notice.ResultSkipped {
		t.Fatalf("look err %v %+v\n%q\n%q", err, ds, title, body)
	}
	ht, hb := DefaultConfig().HostMessage(Host{Host: "mbp", At: at, Vendors: map[string]VendorReport{"claude": {Wired: true, Token: true}}}, nil, now)
	ds, err = notice.Send(context.Background(), notice.Message{Title: ht, Message: hb})
	if err != nil || ds[0].Result != notice.ResultSkipped {
		t.Fatalf("host err %v %+v\n%q\n%q", err, ds, ht, hb)
	}
}

func assertNotice(t *testing.T, text string, forbid []string, marker bool) {
	t.Helper()
	if !utf8.ValidString(text) || notice.HasHidden(text) || strings.ContainsRune(text, '\t') {
		t.Fatalf("unclean notice %q", text)
	}
	for _, s := range forbid {
		if strings.Contains(text, s) {
			t.Fatalf("notice contains %q\n%s", s, text)
		}
	}
	if marker && !strings.Contains(text, notice.WithheldMarker) {
		t.Fatalf("marker missing\n%s", text)
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
