package ownerexec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestReviewF2RedactsBeforeEitherBoundary(t *testing.T) {
	shell := httptest.NewServer(ShellHandler(func(string, ...any) {}))
	defer shell.Close()
	for _, n := range []int{2037, 8181, 100000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			out := RemoteRun(shell.URL)(context.Background(), fmt.Sprintf("printf 'token=CANARYVALUE'; printf '%%%ds' ''", n), time.Minute)
			if strings.Contains(out.OutputTail, "CANARYVALUE") {
				t.Fatal("F2: token value survived clipping before redaction")
			}
		})
	}
	for _, n := range []int{2040, 8180} {
		t.Run(fmt.Sprintf("exact-%d", n), func(t *testing.T) {
			out := RemoteRun(shell.URL)(context.Background(), fmt.Sprintf("printf 'fake-exact-CANARYVALUE'; printf '%%%ds' ''", n), time.Minute, "fake-exact-CANARYVALUE")
			if strings.Contains(out.OutputTail, "VALUE") {
				t.Fatal("F2: exact secret fragment survived shell clipping")
			}
		})
	}
}

func TestReviewF1DisconnectDoesNotPostDefinitiveFailure(t *testing.T) {
	for _, mode := range []string{"disconnect", "bad-json", "http-502", "empty-object"} {
		t.Run(mode, func(t *testing.T) {
			var ran atomic.Int32
			shell := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran.Add(1) // a simulated side effect before losing the reply
				if mode == "http-502" {
					w.WriteHeader(502)
					return
				}
				if mode == "empty-object" {
					_, _ = w.Write([]byte("{}"))
					return
				}
				if mode == "bad-json" {
					_, _ = w.Write([]byte("{"))
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer shell.Close()
			h := newHarness(t, "approved")
			h.srv.Run = RemoteRun(shell.URL)
			h.post(testBearer, h.body(h.code()))
			if ran.Load() != 1 {
				t.Fatalf("ran %d times", ran.Load())
			}
			if h.fake.result != nil {
				t.Fatal("F1: uncertain shell outcome posted as definitive result (nil exit + error becomes failed)")
			}
		})
	}
}

func TestReviewF3BackgroundCannotOutliveCompletion(t *testing.T) {
	if os.Getenv("OWNER_EXEC_REPRO_F3") != "1" {
		t.Skip("known F3: requires per-command containment design; set OWNER_EXEC_REPRO_F3=1 to reproduce")
	}
	marker := filepath.Join(t.TempDir(), "late-write")
	out := RunCommand(context.Background(), fmt.Sprintf("(sleep 0.3; echo escaped > %q) >/dev/null 2>&1 & exit 0", marker), time.Second)
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", out)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("F3: background child wrote after successful command completion")
	}
}

func TestReviewF3DetachedChildRequiresContainment(t *testing.T) {
	if os.Getenv("OWNER_EXEC_REPRO_F3") != "1" {
		t.Skip("known F3: requires per-command containment design; set OWNER_EXEC_REPRO_F3=1 to reproduce")
	}
	marker := filepath.Join(t.TempDir(), "late-write")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := RunCommand(context.Background(), fmt.Sprintf("%q -test.run ^TestReviewDetachedHelper$ -- %q >/dev/null 2>&1 & wait", exe, marker), 2*time.Second)
	if !strings.Contains(out.Err, "timed out") {
		t.Fatalf("unexpected result: %+v", out)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("F3: detached child wrote after timeout killed the original process group")
	}
}

type recoveringPlatform struct {
	*fakePlatform
	attempts int
}

func (p *recoveringPlatform) Heartbeat(context.Context, string, string, string) error {
	p.fakePlatform.note("heartbeat")
	return nil
}

func (p *recoveringPlatform) Result(ctx context.Context, bearer, id string, res Result) error {
	p.attempts++
	if p.attempts <= 5 {
		return &StatusError{Status: 503}
	}
	return p.fakePlatform.Result(ctx, bearer, id, res)
}
func TestReviewF4WaitRequiresResultDeliveryAfterRecovery(t *testing.T) {
	h := newHarness(t, "approved")
	p := &recoveringPlatform{fakePlatform: h.fake}
	h.srv.Platform = p
	h.post(testBearer, h.body(h.code()))
	if p.attempts < 6 || h.fake.result == nil {
		t.Fatalf("F4: Wait returned with result lost after %d attempts", p.attempts)
	}
	if h.commandsRun() != 1 {
		t.Fatal("result retry reran the command")
	}
	if !h.fake.called("heartbeat") {
		t.Fatal("lease renewal stopped while a known result was pending")
	}
}

func TestReviewDetachedHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	if _, err := syscall.Setsid(); err != nil {
		os.Exit(2)
	}
	time.Sleep(3 * time.Second)
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("escaped"), 0600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestReviewF2CompleteOutputAcrossWritesAndLongSecrets(t *testing.T) {
	shell := httptest.NewServer(ShellHandler(func(string, ...any) {}))
	defer shell.Close()
	secret := strings.Repeat("FAKE-ONLY-", 400)
	commands := []struct{ command, secret, fragment string }{
		{"printf 'token=CANA'; sleep 0.05; printf 'RYVALUE'; printf '%2037s' ''", "", "CANARYVALUE"},
		{fmt.Sprintf("printf '%s'; printf '%%2040s' ''", secret), secret, "FAKE-ONLY-"},
		{"printf 'token='; head -c 1100000 /dev/zero | tr '\\0' 'S'", "", "SSSSSS"},
		{"printf 'token='; head -c 9000 /dev/zero | tr '\\0' 'S'", "", "SSSSSS"},
	}
	for i, c := range commands {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			out := RemoteRun(shell.URL)(context.Background(), c.command, time.Minute, c.secret)
			if out.ExitCode == nil || *out.ExitCode != 0 {
				t.Fatalf("run failed: %+v", out)
			}
			if strings.Contains(out.OutputTail, c.fragment) {
				t.Fatal("secret survived complete-output redaction")
			}
			if len(out.OutputTail) > tailSend {
				t.Fatal("tail exceeds card limit")
			}
			if i == 2 && !strings.Contains(out.OutputTail, "output omitted") {
				t.Fatal("overflow must be explicit")
			}
		})
	}
}
