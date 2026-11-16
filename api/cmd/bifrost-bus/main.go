// Command bifrost-bus is the bus's entry point for scripts and pipelines
// (api/internal/bus/RULES.md §8 item 3): it long-polls the platform's gate,
// GET /api/v1/bus/events, and holds no NATS connection and no consumer.
//
//	bifrost-bus wait <type> [--match S] [--after CURSOR] [--timeout SECONDS] [--client LABEL]
//
// prints the first event of <type> whose subject holds S, after CURSOR
// (from now when left out), as its CloudEvents JSON on one line, and exits
// 0; exit 2 when none comes within the timeout (default 600 s), 1 on an
// error. PLATFORM_API_URL is the platform (required); PLATFORM_TOKEN, when
// set, is sent as the bearer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const usage = "usage: bifrost-bus wait <type> [--match S] [--after CURSOR] [--timeout SECONDS] [--client LABEL]"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr, http.DefaultClient))
}

// page is one answer of the gate.
type page struct {
	Events []json.RawMessage `json:"events"`
	Next   string            `json:"next"`
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, client *http.Client) int {
	if len(args) < 2 || args[0] != "wait" {
		_, _ = fmt.Fprintln(stderr, usage)
		return 1
	}
	typ := args[1]
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	match := fs.String("match", "", "a substring of the event's subject")
	after := fs.String("after", "", "the cursor to read after (default: from now)")
	timeout := fs.Int("timeout", 600, "seconds to wait in all")
	label := fs.String("client", "bifrost-bus", "who is waiting, for the receipts (at most 40 characters)")
	if err := fs.Parse(args[2:]); err != nil {
		_, _ = fmt.Fprintln(stderr, usage)
		return 1
	}
	base := strings.TrimRight(strings.TrimSpace(getenv("PLATFORM_API_URL")), "/")
	if base == "" {
		_, _ = fmt.Fprintln(stderr, "bifrost-bus: PLATFORM_API_URL is not set")
		return 1
	}
	if *timeout < 0 {
		_, _ = fmt.Fprintln(stderr, "bifrost-bus: --timeout is seconds, 0 or more")
		return 1
	}
	token := strings.TrimSpace(getenv("PLATFORM_TOKEN"))
	deadline := time.Now().Add(time.Duration(*timeout) * time.Second)
	cursor := *after
	for {
		wait := min(25, int(time.Until(deadline).Seconds()))
		if wait < 0 {
			wait = 0
		}
		q := url.Values{"types": {typ}, "wait": {strconv.Itoa(wait)}, "client": {*label}}
		if *match != "" {
			q.Set("match", *match)
		}
		if cursor != "" {
			q.Set("after", cursor)
		}
		p, err := get(ctx, client, base+"/api/v1/bus/events?"+q.Encode(), token)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "bifrost-bus:", err)
			return 1
		}
		if len(p.Events) > 0 {
			_, _ = fmt.Fprintln(stdout, string(p.Events[0]))
			return 0
		}
		cursor = p.Next
		if !time.Now().Before(deadline) {
			_, _ = fmt.Fprintf(stderr, "bifrost-bus: no %s within %d s; read on with --after %s\n", typ, *timeout, cursor)
			return 2
		}
	}
}

func get(ctx context.Context, client *http.Client, u, token string) (page, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return page{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return page{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return page{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("GET /api/v1/bus/events: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var p page
	if err := json.Unmarshal(body, &p); err != nil {
		return page{}, err
	}
	if p.Next == "" && len(p.Events) == 0 {
		return page{}, errors.New("the gate answered no cursor")
	}
	return p, nil
}
