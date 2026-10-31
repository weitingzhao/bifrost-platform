package questions

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestAssessmentIsOnlyOnHTTPViews(t *testing.T) {
	h, s := newHTTP(t)
	created := mustCreate(t, s, "thread-a", "assessment-view-463", nil)
	id := created["id"].(string)
	before, err := os.ReadFile(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	s.Assessment = func(_ context.Context, got string) map[string]any {
		if got != id {
			t.Fatalf("assessment id: %s", got)
		}
		reads++
		return map[string]any{"events": []any{}}
	}
	for _, path := range []string{"/api/v1/questions/" + id, "/api/v1/questions"} {
		r := call(t, h, http.MethodGet, path, "", "viewer-test-token", "owner-console")
		if r.Code != 200 || !strings.Contains(r.Body.String(), `"assessment":{"events":[]}`) {
			t.Fatalf("view: %d %s", r.Code, r.Body.String())
		}
	}
	after, err := os.ReadFile(s.store.path)
	if err != nil || !bytes.Equal(before, after) || reads != 2 {
		t.Fatalf("view wrote a question or missed its stream: reads=%d err=%v", reads, err)
	}
}
