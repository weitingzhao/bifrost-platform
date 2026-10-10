package approvalnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

const secretCommand = "kubectl -n cicd delete configmap bifrost-remediation-runner-stg-dockerfile --token=s3cr3t"

func ownerRun() Item {
	return Item{
		ID: "appr_0123456789abcdef", Number: 57, Action: "owner_run_command", Tier: "D", Env: "host",
		Summary:   "Delete retired ConfigMap bifrost-remediation-runner-stg-dockerfile (TD-290)",
		KeyParams: map[string]string{"command": secretCommand},
		Requester: "claude", Thread: "S0-0 plan", WorkID: "W-37", Runner: "host",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour),
	}
}

func gitopsSync() Item {
	return Item{
		ID: "appr_fedcba9876543210", Number: 58, Action: "gitops_sync_app", Tier: "C", Env: "prod",
		Summary:   "Sync Argo app bifrost-platform-prod to HEAD",
		KeyParams: map[string]string{"name": "bifrost-platform-prod"},
		Requester: "cursor-w49", Runner: "platform",
		CreatedAt: now.Add(-30 * time.Minute), ExpiresAt: now.Add(23*time.Hour + 30*time.Minute),
	}
}

// TestComposeSnapshots is the text the Owner's phone shows, kind by kind
// (TD-286: #n, tier, action, env, summary, params, requester, expiry).
func TestComposeSnapshots(t *testing.T) {
	failed := gitopsSync()
	failed.Error = "argocd: app bifrost-platform-prod not found"
	notRun := gitopsSync()
	notRun.Error = "not executed before the execution deadline; last refusal: release window held by bifrost-research"
	waiting := gitopsSync()
	waiting.CreatedAt, waiting.ExpiresAt = now.Add(-4*time.Hour), now.Add(20*time.Hour)
	expiring := ownerRun()
	expiring.CreatedAt, expiring.ExpiresAt = now.Add(-22*time.Hour), now.Add(2*time.Hour)

	for _, tc := range []struct {
		kind  string
		item  Item
		title string
		body  string
		prio  int
	}{
		{KindCreated, ownerRun(),
			"#57 · D · owner_run_command · host",
			"Delete retired ConfigMap bifrost-remediation-runner-stg-dockerfile (TD-290)\n" +
				"from W-37 · S0-0 plan (claude) · runs on: system · expires in 23h", 5},
		{KindCreated, gitopsSync(),
			"#58 · C · gitops_sync_app · prod",
			"Sync Argo app bifrost-platform-prod to HEAD\n" +
				"name=bifrost-platform-prod\n" +
				"from cursor-w49 · runs on: platform · expires in 23h", 4},
		{KindFailed, failed,
			"#58 failed · gitops_sync_app · prod",
			"Sync Argo app bifrost-platform-prod to HEAD\n" +
				"error: argocd: app bifrost-platform-prod not found\n" +
				"from cursor-w49 · runs on: platform", 4},
		{KindUnknown, ownerRun(),
			"#57 unknown · owner_run_command · host",
			"Delete retired ConfigMap bifrost-remediation-runner-stg-dockerfile (TD-290)\n" +
				"executor lost: lease lapsed with no result\n" +
				"Check before running it again.\n" +
				"from W-37 · S0-0 plan (claude) · runs on: system", 5},
		{KindNotExecuted, notRun,
			"#58 not executed · gitops_sync_app · prod",
			"Sync Argo app bifrost-platform-prod to HEAD\n" +
				"Approved, but not executed before the deadline.\n" +
				"not executed before the execution deadline; last refusal: release window held by bifrost-research\n" +
				"from cursor-w49 · runs on: platform", 4},
		{KindWaiting, waiting,
			"Waiting 4h · #58 · C · gitops_sync_app · prod",
			"Sync Argo app bifrost-platform-prod to HEAD\n" +
				"name=bifrost-platform-prod\n" +
				"from cursor-w49 · runs on: platform · expires in 20h", 4},
		{KindExpiring, expiring,
			"Expires in 2h · #57 · D · owner_run_command · host",
			"Delete retired ConfigMap bifrost-remediation-runner-stg-dockerfile (TD-290)\n" +
				"from W-37 · S0-0 plan (claude) · runs on: system", 5},
	} {
		m := Compose(tc.kind, tc.item, now)
		if m.Title != tc.title || m.Message != tc.body || m.Priority != tc.prio {
			t.Errorf("%s:\n got title %q\nwant title %q\n got body %q\nwant body %q\n prio %d want %d",
				tc.kind, m.Title, tc.title, m.Message, tc.body, m.Priority, tc.prio)
		}
		if m.ClickURL != ConsoleClickPrefix+tc.item.ID {
			t.Errorf("%s: click %q", tc.kind, m.ClickURL)
		}
		if strings.Contains(m.Title+m.Message, "kubectl") || strings.Contains(m.Title+m.Message, "s3cr3t") {
			t.Errorf("%s: the command text reached the push: %+v", tc.kind, m)
		}
	}
}

func TestComposeWithoutNumberUsesTheID(t *testing.T) {
	it := gitopsSync()
	it.Number = 0
	if m := Compose(KindCreated, it, now); !strings.HasPrefix(m.Title, "appr_fedcba9876543210 · C") {
		t.Fatalf("title %q", m.Title)
	}
}

func TestSendSkipsWhenUnset(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	ds := Send(context.Background(), KindCreated, gitopsSync(), now)
	if Configured() || len(ds) != 1 || ds[0].Result != ResultSkipped || ds[0].Kind != KindCreated || !ds[0].At.Equal(now) {
		t.Fatalf("deliveries %+v", ds)
	}
}

func TestSendPostsAndKeepsTheRelayTargets(t *testing.T) {
	const token = "relay-token-value"
	var gotAuth, gotBody string
	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
		Priority int    `json:"priority"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(`{"status":"sent","deliveries":[{"channel":"ntfy","target":"ntfy:0a1b2c3d4e5f","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", token)

	ds := Send(context.Background(), KindCreated, ownerRun(), now)
	if gotAuth != "Bearer "+token {
		t.Fatalf("auth %q", gotAuth)
	}
	if body.Title != "#57 · D · owner_run_command · host" || body.Priority != 5 || body.ClickURL != ConsoleClickPrefix+"appr_0123456789abcdef" {
		t.Fatalf("body %+v", body)
	}
	if strings.Contains(gotBody, token) || strings.Contains(gotBody, "kubectl") {
		t.Fatalf("token or command in the push: %s", gotBody)
	}
	want := Delivery{Kind: KindCreated, Channel: "ntfy", Target: "ntfy:0a1b2c3d4e5f", At: now, Result: ResultAccepted}
	if len(ds) != 1 || ds[0] != want {
		t.Fatalf("deliveries %+v", ds)
	}
}

func TestSendRecordsEveryFailure(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	relayFailed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"ntfy: status 500","deliveries":[{"channel":"ntfy","target":"ntfy:0a1b2c3d4e5f","result":"failed","error":"ntfy: status 500"}]}`))
	}))
	t.Cleanup(relayFailed.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", relayFailed.URL)
	ds := Send(context.Background(), KindFailed, gitopsSync(), now)
	if len(ds) != 1 || ds[0].Result != ResultFailed || ds[0].Target != "ntfy:0a1b2c3d4e5f" || ds[0].Error != "ntfy: status 500" {
		t.Fatalf("relay reported failure: %+v", ds)
	}

	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(bare.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", bare.URL)
	ds = Send(context.Background(), KindCreated, gitopsSync(), now)
	if len(ds) != 1 || ds[0].Result != ResultFailed || ds[0].Target != "relay" || !strings.Contains(ds[0].Error, "401") {
		t.Fatalf("relay refused: %+v", ds)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()
	t.Setenv("APPROVAL_NOTIFY_URL", dead.URL)
	ds = Send(context.Background(), KindCreated, gitopsSync(), now)
	if len(ds) != 1 || ds[0].Result != ResultFailed || ds[0].Error == "" || strings.Contains(ds[0].Error, "tok") {
		t.Fatalf("relay down: %+v", ds)
	}
}

func TestSendAcceptsAnOlderRelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	ds := Send(context.Background(), KindCreated, gitopsSync(), now)
	if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Target != "relay" {
		t.Fatalf("deliveries %+v", ds)
	}
}

func TestNotifySendsMessageThroughTheSameRelay(t *testing.T) {
	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
		Priority int    `json:"priority"`
	}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "relay-token-value")
	if err := Notify(context.Background(), Message{Title: "Release policy expires in 23h", Message: "sign: release.sh policy sign"}); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer relay-token-value" || body.Title != "Release policy expires in 23h" ||
		body.ClickURL != ConsoleApprovals || body.Priority != 4 || !strings.Contains(body.Message, "policy sign") {
		t.Fatalf("auth=%q body=%+v", auth, body)
	}
}

func TestNotifyReturnsTheRelayFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	if err := Notify(context.Background(), Message{Title: "x", Message: "y"}); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err %v", err)
	}
}

func TestDeliveryErrorIsRedactedBeforeClipping(t *testing.T) {
	const secret = "SUPERSECRETVALUE"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"token=` + secret + `","deliveries":[{"channel":"ntfy","target":"ntfy:abc","result":"failed","error":"token=` + secret + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	ds := Send(context.Background(), KindFailed, gitopsSync(), now)
	blob, _ := json.Marshal(ds)
	if strings.Contains(string(blob), secret) || !strings.Contains(string(blob), "[redacted]") {
		t.Fatalf("deliveries = %s", blob)
	}
}

func TestProbeArgsStayOutOfThePush(t *testing.T) {
	it := Item{
		ID: "appr_probe", Number: 9, Action: "run_probe_pod", Tier: "B", Env: "data",
		Summary: "probe",
		KeyParams: map[string]string{
			"namespace": "data",
			"args":      "--password,SYNTHETIC_SECRET",
			"api_key":   "sk-LEAKEDAPIKEY999",
		},
	}
	m := Compose(KindCreated, it, now)
	text := m.Title + "\n" + m.Message
	if strings.Contains(text, "SYNTHETIC_SECRET") || strings.Contains(text, "sk-LEAKEDAPIKEY999") || strings.Contains(text, "args=") {
		t.Fatalf("push showed an unsafe key: %q", text)
	}
	if !strings.Contains(text, "namespace=data") {
		t.Fatalf("safe key missing: %q", text)
	}
}

func TestUnknownPushShowsTheReason(t *testing.T) {
	for _, reason := range []string{
		"create outcome unknown; outcome needs checking",
		"object cicd/run already exists; it must be checked by hand",
		"executor lost: lease lapsed with no result",
	} {
		it := gitopsSync()
		it.Error = reason
		m := Compose(KindUnknown, it, now)
		if !strings.Contains(m.Message, reason) {
			t.Fatalf("message %q does not contain %q", m.Message, reason)
		}
		if strings.Contains(m.Message, "Executor lost: no result after its lease lapsed") {
			t.Fatalf("fixed lease sentence replaced the reason: %q", m.Message)
		}
	}
	it := gitopsSync()
	it.Error = "create outcome unknown token=SYNTHETIC_SECRET"
	m := Compose(KindUnknown, it, now)
	if strings.Contains(m.Title+m.Message, "SYNTHETIC_SECRET") || !strings.Contains(m.Message, "token=[redacted]") {
		t.Fatalf("push = %q", m.Message)
	}
}

func TestNotifySlogRedactsTheTransportError(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	t.Setenv("APPROVAL_NOTIFY_URL", "http://user:token=SYNTHETIC_SECRET@127.0.0.1:1/notify")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	ds := Send(context.Background(), KindUnknown, gitopsSync(), now)
	blob := logs.String() + ds[0].Error
	if strings.Contains(blob, "SYNTHETIC_SECRET") || !strings.Contains(logs.String(), "[redacted]") {
		t.Fatalf("log=%s delivery=%+v", logs.String(), ds)
	}
}
