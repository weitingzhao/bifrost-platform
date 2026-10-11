package questions

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

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

const mark = "Ω"

func doorNow() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC) }

func baseQuestion() Question {
	return Question{
		ID: "q_pin000000000001", Number: 12, Requester: "cursor",
		Title: "Where is the quota?", Urgency: "today",
		ExpiresAt: doorNow().Add(23 * time.Hour),
	}
}

func setQuestion(q Question, field, value string) Question {
	switch field {
	case "title":
		q.Title = value
	case "urgency":
		q.Urgency = value
	case "requester":
		q.Requester = value
		q.RequesterThread = ""
	case "thread":
		q.RequesterThread = value
	default:
		panic("field " + field)
	}
	return q
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

func TestAnswerFallbackIsTheNumber(t *testing.T) {
	q := baseQuestion()
	if answerFallback(q.Number) != "Answer · Q12" || notice.HasHidden(answerFallback(q.Number)) || notice.HasHidden(notice.WithheldBody) {
		t.Fatal(answerFallback(q.Number))
	}
	msg := createdNotice(q, doorNow())
	if msg.Fallback != "Answer · Q12" || msg.Kind != "question" || msg.ID != q.ID || msg.Priority != 4 {
		t.Fatalf("%+v", msg)
	}
}

func TestWorkIDIsNotInTheQuestionNotice(t *testing.T) {
	q := baseQuestion()
	q.WorkID = "W\u202e65"
	q.Body = "body\u200b"
	msg := createdNotice(q, doorNow())
	if notice.HasHidden(msg.Title) || notice.HasHidden(msg.Message) || strings.Contains(msg.Title+msg.Message, "\u202e") || strings.Contains(msg.Title+msg.Message, "body") {
		t.Fatalf("work id or body reached the notice\n%q\n%q", msg.Title, msg.Message)
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
	now := doorNow()
	for _, sample := range samples {
		q := baseQuestion()
		q.Title = sample
		q.Urgency = sample
		q.Requester = sample
		q.RequesterThread = sample
		msg := createdNotice(q, now)
		got := notice.Posted(msg)
		if got.Title != msg.Title || got.Message != msg.Message || got.ClickURL != msg.ClickURL || got.Priority != msg.Priority {
			t.Fatalf("changed %q", got.Title)
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
	now := doorNow()
	fields := []string{"title", "urgency", "requester", "thread"}
	positions := []string{"first", "middle", "last"}
	var cases int
	for _, field := range fields {
		shown := createdNotice(setQuestion(baseQuestion(), field, mark), now)
		if !strings.Contains(shown.Title+shown.Message, mark) {
			t.Fatalf("%s not in the notice\n%q\n%q", field, shown.Title, shown.Message)
		}
		for _, sample := range notice.HiddenSamples() {
			for _, pos := range positions {
				cases++
				msg := createdNotice(setQuestion(baseQuestion(), field, place(sample.Text, pos)), now)
				if !notice.HasHidden(msg.Title) && !notice.HasHidden(msg.Message) {
					t.Fatalf("%s %s %s dropped\n%q\n%q", field, sample.Name, pos, msg.Title, msg.Message)
				}
				ds, err := notice.Send(context.Background(), msg)
				if err != nil {
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
				if posted.Title != "Answer · Q12" || posted.Message != notice.WithheldBody || posted.ClickURL != msg.ClickURL || posted.Priority != 4 {
					t.Fatalf("%s posted %+v", field, posted)
				}
				if strings.Contains(posted.Title+posted.Message, mark) || strings.Contains(string(raw), sample.Text) || !utf8.ValidString(string(raw)) {
					t.Fatalf("leaked %q", raw)
				}
				if len(ds) != 1 || ds[0].Result != notice.ResultAccepted || ds[0].Kind != "" {
					t.Fatalf("delivery %+v", ds)
				}
			}
		}
	}
	if cases != len(fields)*len(notice.HiddenSamples())*len(positions) {
		t.Fatalf("cases %d", cases)
	}
}
