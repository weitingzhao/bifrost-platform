package notice

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendIsTheLastDoor(t *testing.T) {
	var posted struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &posted); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(`{"deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	title := "x\tbearer abcdefgh"
	body := strings.Repeat("x", 80) + " AKIA" + strings.Repeat("A", 8) + "\u200b" + strings.Repeat("A", 8)
	if _, err := Send(t.Context(), Message{Title: title, Message: body}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(posted.Title, "abcdefgh") || strings.Contains(posted.Message, "AKIA"+strings.Repeat("A", 16)) {
		t.Fatalf("title %q\nbody %q", posted.Title, posted.Message)
	}
	if posted.Title != PushText(title, false) || posted.Message != PushText(body, true) {
		t.Fatalf("title %q\nbody %q", posted.Title, posted.Message)
	}
	if PushText(posted.Title, false) != posted.Title || PushText(posted.Message, true) != posted.Message {
		t.Fatalf("title %q\nbody %q", posted.Title, posted.Message)
	}
}
