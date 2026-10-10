package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func TestSecretsAreRedactedBeforeTheyAreStored(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.json")
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(auditPath))
	h := chi.NewRouter()
	h.Post("/approvals/{id}/result", svc.HandleResult)
	h.Post("/approvals/{id}/reject", svc.HandleReject)

	const (
		tokenValue = "abc123tokenvalue"
		password   = "SUPERSECRETVALUE"
		keyBody    = "MIISECRETKEY"
	)

	rec, lease := claimed(t, svc)
	head := strings.Repeat("p", 400) + "note\n"
	tailBody := password + "\n" + "token=" + tokenValue + "\n"
	tailBody += strings.Repeat("q", maxTail-len(tailBody))
	output := head + "password=" + tailBody
	naive := tail(output, maxTail)
	if strings.Contains(naive, "password=") || !strings.Contains(naive, password) || !strings.Contains(naive, tokenValue) {
		t.Fatalf("fixture tail does not cut password= off the secret: %q", naive[:80])
	}
	body, _ := json.Marshal(resultInput{
		LeaseID:      lease,
		ExitCode:     intPtr(1),
		OutputSHA256: strings.Repeat("ab", 32),
		OutputTail:   output,
		Error:        "failed token=" + tokenValue,
	})
	res := postApproval(h, "/approvals/"+rec.ID+"/result", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("result = %d %s", res.Code, res.Body.String())
	}
	got, _ := svc.find(rec.ID)
	assertNoSecret(t, "output tail", got.Execution.OutputTail, password, tokenValue)
	assertNoSecret(t, "execution error", got.Execution.Error, tokenValue)
	assertNoSecret(t, "approval error", got.Error, tokenValue)
	if !strings.Contains(got.Execution.OutputTail, "[redacted]") || !strings.Contains(got.Error, "[redacted]") {
		t.Fatalf("stored text lost the redaction mark: tail %q error %q", got.Execution.OutputTail, got.Error)
	}

	rec2, lease2 := claimed(t, svc)
	header := "-----BEGIN OPENSSH PRIVATE KEY-----\n"
	keyTail := strings.Repeat(keyBody, 30)
	if len(keyTail) > maxTail {
		keyTail = keyTail[:maxTail]
	}
	keyTail += strings.Repeat("w", maxTail-len(keyTail))
	keyOutput := strings.Repeat("z", 80) + header + keyTail
	naiveKey := tail(keyOutput, maxTail)
	if strings.Contains(naiveKey, "BEGIN") || !strings.Contains(naiveKey, keyBody) {
		t.Fatal("fixture tail still contains the private-key header or lost the body")
	}
	body, _ = json.Marshal(resultInput{
		LeaseID:      lease2,
		ExitCode:     intPtr(1),
		OutputSHA256: strings.Repeat("cd", 32),
		OutputTail:   keyOutput,
		Error:        "key token=" + tokenValue,
	})
	res = postApproval(h, "/approvals/"+rec2.ID+"/result", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("key result = %d %s", res.Code, res.Body.String())
	}
	got, _ = svc.find(rec2.ID)
	assertNoSecret(t, "key tail", got.Execution.OutputTail, keyBody)
	if !strings.Contains(got.Execution.OutputTail, "[redacted private key]") {
		t.Fatalf("private key header was not applied to the whole output: %q", got.Execution.OutputTail)
	}

	rec3, lease3 := claimed(t, svc)
	notStarted := false
	body, _ = json.Marshal(resultInput{LeaseID: lease3, Started: &notStarted, Refusal: "kube token=" + tokenValue})
	res = postApproval(h, "/approvals/"+rec3.ID+"/result", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("refusal = %d %s", res.Code, res.Body.String())
	}
	got, _ = svc.find(rec3.ID)
	assertNoSecret(t, "last refusal", got.Execution.LastRefusal, tokenValue)
	if !strings.Contains(got.Execution.LastRefusal, "[redacted]") {
		t.Fatalf("refusal = %q", got.Execution.LastRefusal)
	}

	pending := svc.create(context.Background(), "s", "cordon_node", "c", "", map[string]any{"name": "n"})
	if pending.Status != http.StatusCreated {
		t.Fatalf("create = %d", pending.Status)
	}
	body, _ = json.Marshal(map[string]string{"reason": "no token=" + tokenValue})
	res = postApproval(h, "/approvals/"+pending.Approval.ID+"/reject", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("reject = %d %s", res.Code, res.Body.String())
	}
	got, _ = svc.find(pending.Approval.ID)
	assertNoSecret(t, "reject reason", got.RejectReason, tokenValue)

	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "audit", string(raw), password, tokenValue, keyBody)
	if !strings.Contains(string(raw), "[redacted]") {
		t.Fatal("audit rows dropped the redacted error and refusal")
	}
}

func intPtr(n int) *int { return &n }

func assertNoSecret(t *testing.T, where, text string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			t.Fatalf("%s still contains %q", where, secret)
		}
	}
}
