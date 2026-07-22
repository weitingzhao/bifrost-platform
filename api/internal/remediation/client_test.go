package remediation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFakeRunnerServer builds an httptest server that mimics the remediation
// runner's HTTP surface closely enough to exercise RunnerClient.
func newFakeRunnerServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"1.2.3","service":"remediation-runner","cursor_api_key":true}`))
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"job-1","status":"running","phase":"starting","scope":"test-scope"}`))
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jobs":[{"id":"job-1","status":"running"}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/run/job-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"job-1","status":"done","phase":"done","summary":"all good"}`))
	})
	mux.HandleFunc("/run/job-missing", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/run/job-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"job-1","status":"cancelled","phase":"cancelled"}`))
	})
	mux.HandleFunc("/run/job-1/respond", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/run/job-1/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"status\",\"text\":\"hi\"}\n\n"))
	})
	mux.HandleFunc("/nightly/run", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"started","script":"nightly.sh"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestRunnerClient(t *testing.T, primaryURL string) *RunnerClient {
	t.Helper()
	t.Setenv("REMEDIATION_RUNNER_URL", primaryURL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", "")
	return NewRunnerClient()
}

func TestRunnerClientHealth(t *testing.T) {
	srv := newFakeRunnerServer(t)
	c := newTestRunnerClient(t, srv.URL)

	out, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if out["status"] != "ok" || out["version"] != "1.2.3" {
		t.Fatalf("Health() = %+v", out)
	}
}

func TestRunnerClientStartGetCancelRespond(t *testing.T) {
	srv := newFakeRunnerServer(t)
	c := newTestRunnerClient(t, srv.URL)
	ctx := context.Background()

	job, err := c.Start(ctx, StartRunnerRequest{Scope: "test-scope"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if job.ID != "job-1" || job.Status != JobRunning {
		t.Fatalf("Start() job = %+v", job)
	}

	got, err := c.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil || got.Status != JobDone {
		t.Fatalf("Get() job = %+v", got)
	}

	missing, err := c.Get(ctx, "job-missing")
	if err != nil {
		t.Fatalf("Get(missing) error = %v", err)
	}
	if missing != nil {
		t.Fatalf("Get(missing) = %+v, want nil", missing)
	}

	cancelled, err := c.Cancel(ctx, "job-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.Status != JobCancelled {
		t.Fatalf("Cancel() job = %+v", cancelled)
	}

	if err := c.Respond(ctx, "job-1", RespondRequest{OptionID: "opt-1"}); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
}

func TestRunnerClientList(t *testing.T) {
	srv := newFakeRunnerServer(t)
	c := newTestRunnerClient(t, srv.URL)

	jobs, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("List() = %+v", jobs)
	}
}

func TestRunnerClientTriggerNightly(t *testing.T) {
	srv := newFakeRunnerServer(t)
	c := newTestRunnerClient(t, srv.URL)

	out, err := c.TriggerNightly(context.Background())
	if err != nil {
		t.Fatalf("TriggerNightly() error = %v", err)
	}
	if out.Status != "started" || out.Script != "nightly.sh" {
		t.Fatalf("TriggerNightly() = %+v", out)
	}
}

func TestRunnerClientStream(t *testing.T) {
	srv := newFakeRunnerServer(t)
	c := newTestRunnerClient(t, srv.URL)

	var lines [][]byte
	err := c.Stream(context.Background(), "job-1", func(payload []byte) error {
		lines = append(lines, append([]byte(nil), payload...))
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("Stream() delivered %d lines, want 1", len(lines))
	}
}

func TestRunnerClientHealthPropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(srv.Close)
	c := newTestRunnerClient(t, srv.URL)

	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("Health() error = nil, want error for HTTP 500")
	}
}

// TestRunnerClientFailoverToStandby verifies that when the primary endpoint
// is unreachable, requests transparently fail over to the standby and the
// client remembers the standby as active for subsequent calls.
func TestRunnerClientFailoverToStandby(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // connections to this address will now be refused

	standby := newFakeRunnerServer(t)

	t.Setenv("REMEDIATION_RUNNER_URL", deadURL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", standby.URL)
	c := NewRunnerClient()

	if got := c.PrimaryURL(); got != deadURL {
		t.Fatalf("PrimaryURL() = %q, want %q", got, deadURL)
	}
	if got := c.ActiveURL(); got != deadURL {
		t.Fatalf("ActiveURL() before failover = %q, want primary %q", got, deadURL)
	}

	out, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v (expected failover to standby to succeed)", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("Health() = %+v", out)
	}
	if got := c.ActiveURL(); got != standby.URL {
		t.Fatalf("ActiveURL() after failover = %q, want standby %q", got, standby.URL)
	}
	// Primary URL is unaffected by failover — nightly drift always targets it.
	if got := c.PrimaryURL(); got != deadURL {
		t.Fatalf("PrimaryURL() after failover = %q, want unchanged %q", got, deadURL)
	}
}

func TestRunnerClientHealthAllFailsBackToPrimaryWhenHealthy(t *testing.T) {
	primary := newFakeRunnerServer(t)
	standby := newFakeRunnerServer(t)

	t.Setenv("REMEDIATION_RUNNER_URL", primary.URL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", standby.URL)
	c := NewRunnerClient()

	healths := c.HealthAll(context.Background())
	if len(healths) != 2 {
		t.Fatalf("HealthAll() len = %d, want 2", len(healths))
	}
	var primaryHealth, standbyHealth *RunnerHealth
	for i := range healths {
		switch healths[i].Role {
		case "primary":
			primaryHealth = &healths[i]
		case "standby":
			standbyHealth = &healths[i]
		}
	}
	if primaryHealth == nil || primaryHealth.Status != "ok" || !primaryHealth.Active {
		t.Fatalf("primary health = %+v, want ok+active", primaryHealth)
	}
	if standbyHealth == nil || standbyHealth.Status != "ok" || standbyHealth.Active {
		t.Fatalf("standby health = %+v, want ok+not-active (primary preferred)", standbyHealth)
	}
}

func TestRunnerClientHealthAllPromotesStandbyWhenPrimaryDown(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	standby := newFakeRunnerServer(t)

	t.Setenv("REMEDIATION_RUNNER_URL", deadURL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", standby.URL)
	c := NewRunnerClient()

	healths := c.HealthAll(context.Background())
	var active *RunnerHealth
	for i := range healths {
		if healths[i].Active {
			active = &healths[i]
		}
	}
	if active == nil || active.URL != standby.URL {
		t.Fatalf("expected standby to be promoted active, got %+v", healths)
	}
	if got := c.ActiveURL(); got != standby.URL {
		t.Fatalf("ActiveURL() = %q, want standby %q", got, standby.URL)
	}
}

func TestTrimRunnerErrorBodyExtractsReferenceError(t *testing.T) {
	html := []byte(`<html><body>ReferenceError: foo is not defined<div>trace</div></body></html>`)
	got := trimRunnerErrorBody(html, 500)
	if got != "ReferenceError: foo is not defined" {
		t.Fatalf("trimRunnerErrorBody() = %q", got)
	}
}

func TestTrimRunnerErrorBodyTruncatesLongPlainText(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'x'
	}
	got := trimRunnerErrorBody(long, 500)
	if len(got) <= 400 {
		t.Fatalf("expected truncated text with ellipsis marker, got len=%d", len(got))
	}
}

func TestTrimRunnerErrorBodyPassesThroughShortPlainText(t *testing.T) {
	got := trimRunnerErrorBody([]byte("  short error  "), 400)
	if got != "short error" {
		t.Fatalf("trimRunnerErrorBody() = %q", got)
	}
}
