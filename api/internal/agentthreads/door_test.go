package agentthreads

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

const mark = "Ω"

func doorAt() (time.Time, time.Time) {
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	return at, time.Date(2026, 10, 11, 4, 15, 0, 0, time.UTC)
}

func baseThread() Thread {
	at, _ := doorAt()
	return Thread{
		Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
		Title: "quota", Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120,
		At: at, TurnStartedAt: at,
	}
}

func setThread(t Thread, field, value string) Thread {
	switch field {
	case "title":
		t.Title = value
	case "vendor":
		t.Vendor = value
	case "host":
		t.Host = value
	case "work":
		t.Work = value
	case "tool":
		t.Tool = value
	case "event":
		t.Event = Event(value)
		t.Tool = ""
		t.ToolTimeoutS = 0
	case "thread":
		t.Title = ""
		t.Thread = value
	default:
		panic("field " + field)
	}
	return t
}

func place(sample, pos string) string {
	switch pos {
	case "first":
		return sample + mark
	case "last":
		return mark + sample
	default:
		return mark + sample + mark
	}
}

func lookMessage(title, body, kind, id string) approvalnotify.Message {
	return approvalnotify.Message{
		Title: title, Message: body,
		ClickURL: "http://ops.bifrost.lan/#needs-you",
		Fallback: "Look · thread", Kind: kind, ID: id,
	}
}

func TestLookFallbackIsClean(t *testing.T) {
	if lookMessage("", "", "", "").Fallback != "Look · thread" || notice.HasHidden("Look · thread") || notice.HasHidden(notice.WithheldBody) {
		t.Fatal("fallback")
	}
}

func TestPinLookTextIsPostedUnchanged(t *testing.T) {
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := time.Date(2026, 10, 11, 4, 15, 0, 0, time.UTC)
	title, body := DefaultConfig().Message(Thread{
		Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
		Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120,
		At: at, TurnStartedAt: at,
	}, now)
	wantTitle := "Look · claude 7e939cd5 stopped 11m ago"
	wantBody := "Nothing is working on it. Open the thread.\n" +
		"claude on mbp · W-65 · silent 11m · last event before_tool Bash (timeout 120s) at 04:04 UTC · in turn 11m. Nothing was restarted."
	if title != wantTitle || body != wantBody {
		t.Fatalf("compose changed\n%q\n%q", title, body)
	}
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	if err := approvalnotify.Notify(context.Background(), lookMessage(title, body, "thread", "claude/7e939cd5abcd")); err != nil {
		t.Fatal(err)
	}
	var posted struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
		Priority int    `json:"priority"`
	}
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != wantTitle || posted.Message != wantBody || posted.ClickURL != "http://ops.bifrost.lan/#needs-you" || posted.Priority != 4 {
		t.Fatalf("%+v", posted)
	}
}

func TestCleanCorpusIsPostedUnchanged(t *testing.T) {
	n := notice.CleanCorpusN()
	if testing.Short() {
		n = 24
	}
	samples := notice.CleanSamples(n)
	if len(samples) != n {
		t.Fatalf("corpus %d", len(samples))
	}
	_, now := doorAt()
	cfg := DefaultConfig()
	for _, sample := range samples {
		th := baseThread()
		th.Title = sample
		th.Vendor = sample
		th.Host = sample
		th.Work = sample
		th.Tool = sample
		th.Event = Event(sample)
		th.Thread = sample
		title, body := cfg.Message(th, now)
		msg := lookMessage(title, body, "thread", "k")
		got := notice.Posted(approvalnotify.Message(msg))
		if got.Title != title || got.Message != body {
			t.Fatalf("thread changed\n%q", got.Title)
		}
		h := Host{Host: sample, At: th.At}
		mid := []Thread{{Title: sample, Vendor: sample, Thread: sample, Host: sample}}
		ht, hb := cfg.HostMessage(h, mid, now)
		got = notice.Posted(lookMessage(ht, hb, "host", sample))
		if got.Title != ht || got.Message != hb {
			t.Fatalf("host changed\n%q", got.Title)
		}
	}
}

func TestHiddenFieldsAreWithheldAtTheRelay(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	_, now := doorAt()
	cfg := DefaultConfig()
	positions := []string{"first", "middle", "last"}
	threadFields := []string{"title", "vendor", "host", "work", "tool", "event", "thread"}
	var cases int
	check := func(title, body, kind, id, sample string, hidden bool) {
		t.Helper()
		cases++
		if err := approvalnotify.Notify(context.Background(), lookMessage(title, body, kind, id)); err != nil {
			t.Fatal(err)
		}
		var posted struct {
			Title    string `json:"title"`
			Message  string `json:"message"`
			ClickURL string `json:"click_url"`
			Priority int    `json:"priority"`
		}
		if err := json.Unmarshal(raw, &posted); err != nil {
			t.Fatal(err)
		}
		if !hidden {
			t.Fatalf("expected a hidden compose")
		}
		if posted.Title != "Look · thread" || posted.Message != notice.WithheldBody || posted.ClickURL != "http://ops.bifrost.lan/#needs-you" || posted.Priority != 4 {
			t.Fatalf("posted %+v", posted)
		}
		if strings.Contains(posted.Title+posted.Message, mark) || strings.Contains(string(raw), sample) || !utf8.ValidString(string(raw)) {
			t.Fatalf("leaked %q", raw)
		}
	}
	for _, field := range threadFields {
		shownT, shownB := cfg.Message(setThread(baseThread(), field, mark), now)
		if !strings.Contains(shownT+shownB, mark) {
			t.Fatalf("thread %s not in the push\n%q\n%q", field, shownT, shownB)
		}
		for _, sample := range notice.HiddenSamples() {
			for _, pos := range positions {
				th := setThread(baseThread(), field, place(sample.Text, pos))
				title, body := cfg.Message(th, now)
				if !notice.HasHidden(title) && !notice.HasHidden(body) {
					t.Fatalf("thread %s %s %s dropped the character\n%q", field, sample.Name, pos, title)
				}
				check(title, body, "thread", "claude/t", sample.Text, true)
			}
		}
	}
	for _, field := range []string{"host", "title"} {
		for _, sample := range notice.HiddenSamples() {
			for _, pos := range positions {
				value := place(sample.Text, pos)
				h := Host{Host: "mbp", At: baseThread().At}
				mid := []Thread{{Title: "quota", Vendor: "claude", Thread: "abcd"}}
				if field == "host" {
					h.Host = value
				} else {
					mid[0].Title = value
				}
				title, body := cfg.HostMessage(h, mid, now)
				if !notice.HasHidden(title) && !notice.HasHidden(body) {
					t.Fatalf("host %s %s %s dropped\n%q\n%q", field, sample.Name, pos, title, body)
				}
				id := "mbp"
				if field == "host" {
					id = value
				}
				check(title, body, "host", id, sample.Text, true)
			}
		}
	}
	if cases < 500 {
		t.Fatalf("cases %d", cases)
	}
}
