package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// gateStub answers like GET /api/v1/bus/events: nothing for the first
// answers, then the event, recording what it was asked.
func gateStub(t *testing.T, emptyFirst int, event string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RawQuery+" auth="+r.Header.Get("Authorization"))
		n := len(asked)
		mu.Unlock()
		events := []json.RawMessage{}
		if event != "" && n > emptyFirst {
			events = append(events, json.RawMessage(event))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": events, "next": "77.0"})
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// 2026-11-16: bus step 1 (RULES.md §8 item 3): wait reads on with the
// cursor the gate gave until an event comes, prints it and exits 0.
func TestWaitPrintsTheEvent(t *testing.T) {
	srv, asked := gateStub(t, 1, `{"id":"ev-5","type":"git.main.advanced"}`)
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"wait", "git.main.advanced", "--match", "bifrost-platform@", "--timeout", "60"},
		env(map[string]string{"PLATFORM_API_URL": srv.URL, "PLATFORM_TOKEN": "tok"}), &out, &errb, srv.Client())
	if code != 0 || !strings.Contains(out.String(), `"ev-5"`) {
		t.Fatalf("exit %d, out %q, err %q", code, out.String(), errb.String())
	}
	if len(*asked) != 2 || strings.Contains((*asked)[0], "after=") || !strings.Contains((*asked)[1], "after=77.0") ||
		!strings.Contains((*asked)[0], "match=bifrost-platform%40") || !strings.Contains((*asked)[0], "auth=Bearer tok") {
		t.Fatalf("asked %v", *asked)
	}
}

// 2026-11-16: bus step 1: no event within the timeout is exit 2, with the
// cursor to read on from; no PLATFORM_API_URL is exit 1.
func TestWaitTimesOut(t *testing.T) {
	srv, _ := gateStub(t, 0, "")
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"wait", "git.main.advanced", "--timeout", "0"},
		env(map[string]string{"PLATFORM_API_URL": srv.URL}), &out, &errb, srv.Client())
	if code != 2 || !strings.Contains(errb.String(), "--after 77.0") {
		t.Fatalf("exit %d, err %q", code, errb.String())
	}
	if code := run(context.Background(), []string{"wait", "git.main.advanced"}, env(nil), &out, &errb, srv.Client()); code != 1 {
		t.Fatalf("without PLATFORM_API_URL: exit %d", code)
	}
}
