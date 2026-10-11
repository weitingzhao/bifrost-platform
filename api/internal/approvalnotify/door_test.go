package approvalnotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

const mark = "Ω"

func plainItem() Item {
	it := gitopsSync()
	it.Number = 7
	it.ID = "appr_pin0000000001"
	it.Summary = "Sync the app"
	it.Reason = ""
	it.Error = "relay down"
	it.Requester = "cursor"
	it.Thread = "slice zero"
	it.WorkID = "W-65"
	it.Env = "prod"
	it.KeyParams = map[string]string{"name": "app-prod"}
	return it
}

func setField(it Item, field, value string) Item {
	switch field {
	case "summary":
		it.Summary = value
		it.Reason = ""
	case "reason":
		it.Reason = value
	case "error":
		it.Error = value
	case "requester":
		it.Requester = value
	case "thread":
		it.Thread = value
	case "work":
		it.WorkID = value
	case "env":
		it.Env = value
		it.Summary = ""
		it.Reason = ""
	case "param":
		it.KeyParams = map[string]string{"name": value}
	default:
		panic("field " + field)
	}
	return it
}

func fieldsFor(kind string) []string {
	all := []string{"summary", "reason", "requester", "thread", "work", "env"}
	switch kind {
	case KindFailed, KindUnknown, KindNotExecuted:
		return append(all, "error")
	default:
		return append(all, "param")
	}
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

func TestFallbackIsTheVerbAndTheNumber(t *testing.T) {
	it := plainItem()
	if notice.HasHidden(notice.WithheldBody) || notice.HasHidden(notice.WithheldTitle) {
		t.Fatal("stock fallback")
	}
	for kind, verb := range kindVerb {
		fb := fallbackTitle(kind, it)
		want := verb + " · #7"
		if fb != want || notice.HasHidden(fb) {
			t.Fatalf("%s fallback %q", kind, fb)
		}
	}
	it.Number = 0
	if got := fallbackTitle(KindCreated, it); got != "Decide · "+it.ID || notice.HasHidden(got) {
		t.Fatalf("id fallback %q", got)
	}
}

func TestCleanCorpusIsPostedUnchanged(t *testing.T) {
	n := notice.CleanCorpusN()
	if testing.Short() {
		n = 24
	}
	samples := notice.CleanSamples(n)
	if len(samples) != n {
		t.Fatalf("corpus %d want %d", len(samples), n)
	}
	kinds := []string{KindCreated, KindFailed, KindUnknown, KindNotExecuted, KindWaiting, KindExpiring}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	errCh := make(chan string, workers)
	chunk := (n + workers - 1) / workers
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if lo >= n {
			break
		}
		if hi > n {
			hi = n
		}
		wg.Add(1)
		go func(part []string) {
			defer wg.Done()
			for _, sample := range part {
				it := plainItem()
				it.Summary = sample
				it.Reason = sample
				it.Error = sample
				it.Requester = sample
				it.Thread = sample
				it.WorkID = sample
				it.Env = sample
				it.KeyParams = map[string]string{"name": sample}
				for _, kind := range kinds {
					composed := Compose(kind, it, now)
					msg := composed
					msg.Fallback = fallbackTitle(kind, it)
					msg.Kind = kind
					msg.ID = it.ID
					got := notice.Posted(msg)
					if got.Title != composed.Title || got.Message != composed.Message || got.ClickURL != composed.ClickURL || got.Priority != composed.Priority {
						errCh <- kind + " changed a clean push"
						return
					}
				}
			}
		}(samples[lo:hi])
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != "" {
		t.Fatal(err)
	}
}

func TestHiddenFieldsAreWithheldAtTheRelay(t *testing.T) {
	var mu sync.Mutex
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = body
		mu.Unlock()
		_, _ = w.Write([]byte(`{"deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	kinds := []string{KindCreated, KindFailed, KindUnknown, KindNotExecuted, KindWaiting, KindExpiring}
	positions := []string{"first", "middle", "last"}
	var cases int
	for _, kind := range kinds {
		for _, field := range fieldsFor(kind) {
			shown := Compose(kind, setField(plainItem(), field, mark), now)
			if !strings.Contains(shown.Title+shown.Message, mark) {
				t.Fatalf("%s %s did not reach the push\n%q\n%q", kind, field, shown.Title, shown.Message)
			}
			for _, sample := range notice.HiddenSamples() {
				for _, pos := range positions {
					cases++
					it := setField(plainItem(), field, place(sample.Text, pos))
					composed := Compose(kind, it, now)
					hidden := notice.HasHidden(composed.Title) || notice.HasHidden(composed.Message)
					// U+2028 and U+2029 are Unicode spaces. oneLine and
					// joinNonEmpty already trim them off either end, which
					// is the behaviour this round leaves alone.
					edgeSpace := pos != "middle" && (sample.Name == "ls" || sample.Name == "ps")
					if edgeSpace {
						if hidden {
							t.Fatalf("%s %s %s was not trimmed", kind, field, sample.Name)
						}
					} else if !hidden {
						t.Fatalf("%s %s %s %s did not survive composition\n%q\n%q", kind, field, sample.Name, pos, composed.Title, composed.Message)
					}
					ds := Send(context.Background(), kind, it, now)
					mu.Lock()
					body := append([]byte(nil), raw...)
					mu.Unlock()
					if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Kind != kind || !ds[0].At.Equal(now) {
						t.Fatalf("delivery %+v", ds)
					}
					var posted struct {
						Title    string `json:"title"`
						Message  string `json:"message"`
						ClickURL string `json:"click_url"`
						Priority int    `json:"priority"`
					}
					if err := json.Unmarshal(body, &posted); err != nil {
						t.Fatal(err)
					}
					if hidden {
						if posted.Title != fallbackTitle(kind, it) || posted.Message != notice.WithheldBody {
							t.Fatalf("%s %s %s %s posted %q / %q", kind, field, sample.Name, pos, posted.Title, posted.Message)
						}
						if strings.Contains(posted.Title+posted.Message, mark) || bytesContain(body, sample.Text) {
							t.Fatalf("%s %s leaked %q", kind, field, body)
						}
					} else if posted.Title != composed.Title || posted.Message != composed.Message || bytesContain(body, sample.Text) {
						t.Fatalf("trimmed edge changed or kept the character\n got %q\nwant %q", posted.Title, composed.Title)
					}
					if posted.ClickURL != composed.ClickURL || posted.Priority != composed.Priority || !utf8.ValidString(string(body)) {
						t.Fatalf("click or priority %q", body)
					}
				}
			}
		}
	}
	if cases < 1000 {
		t.Fatalf("cases %d", cases)
	}
}

func TestFormFeedInTheMiddleIsWithheld(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	mid := setField(plainItem(), "summary", mark+"\f"+mark)
	Send(context.Background(), KindCreated, mid, now)
	var posted struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != "Decide · #7" || posted.Message != notice.WithheldBody || strings.Contains(string(raw), mark) {
		t.Fatalf("middle %q", raw)
	}
	edge := setField(plainItem(), "summary", "\f"+mark)
	composed := Compose(KindCreated, edge, now)
	if notice.HasHidden(composed.Title) || notice.HasHidden(composed.Message) || !strings.Contains(composed.Title, mark) {
		t.Fatalf("edge compose %q %q", composed.Title, composed.Message)
	}
	Send(context.Background(), KindCreated, edge, now)
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Title != composed.Title || posted.Message != composed.Message || strings.Contains(string(raw), "\f") {
		t.Fatalf("edge posted %q", raw)
	}
}

func TestCleanKindIsPostedThroughTheRelay(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	for _, kind := range []string{KindCreated, KindFailed, KindUnknown, KindNotExecuted, KindWaiting, KindExpiring} {
		it := plainItem()
		composed := Compose(kind, it, now)
		ds := Send(context.Background(), kind, it, now)
		var posted struct {
			Title    string `json:"title"`
			Message  string `json:"message"`
			ClickURL string `json:"click_url"`
			Priority int    `json:"priority"`
		}
		if err := json.Unmarshal(raw, &posted); err != nil {
			t.Fatal(err)
		}
		if posted.Title != composed.Title || posted.Message != composed.Message || posted.ClickURL != composed.ClickURL || posted.Priority != composed.Priority {
			t.Fatalf("%s\n got %q\nwant %q", kind, posted.Title, composed.Title)
		}
		if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Kind != kind {
			t.Fatalf("%s delivery %+v", kind, ds)
		}
	}
}

func bytesContain(hay []byte, needle string) bool {
	return strings.Contains(string(hay), needle)
}
