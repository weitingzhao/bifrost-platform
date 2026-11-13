package ownerexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

const (
	// Retain complete output up to this bound. If exceeded, omit output
	// rather than redact a fragment whose secret marker may have been lost.
	tailKeep = 1 << 20
	tailSend = 2048
)

// Outcome is what one command run produced.
type Outcome struct {
	ExitCode   *int
	Duration   time.Duration
	OutputSHA  string
	OutputTail string
	Err        string
}

// tailWriter hashes everything and retains bounded, complete output.
type tailWriter struct {
	mu       sync.Mutex
	sum      hash.Hash
	tail     []byte
	overflow bool
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sum.Write(p)
	if !w.overflow {
		if len(p) > tailKeep-len(w.tail) {
			w.overflow = true
			w.tail = nil
		} else {
			w.tail = append(w.tail, p...)
		}
	}
	return len(p), nil
}

// RunCommand runs command with bash in a fresh working directory. The child
// gets a short fixed environment: none of the runner's own variables, and
// nothing from the request that started it.
func RunCommand(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome {
	return runCommand(ctx, command, timeout, true, secrets...)
}

// The shell must return complete output: exact request secrets are only known
// to the runner. Neither hop may clip before that last redaction pass.
func runCommand(ctx context.Context, command string, timeout time.Duration, clip bool, secrets ...string) Outcome {
	started := time.Now()
	dir, err := os.MkdirTemp("", "owner-exec-")
	if err != nil {
		return Outcome{Err: "create working directory: " + err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = childEnv(dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &tailWriter{sum: sha256.New()}
	cmd.Stdout, cmd.Stderr = out, out

	runErr := cmd.Run()
	res := Outcome{Duration: time.Since(started)}
	out.mu.Lock()
	res.OutputSHA = hex.EncodeToString(out.sum.Sum(nil))
	tail := string(out.tail)
	if out.overflow {
		tail = "[output omitted: redaction buffer limit exceeded]"
	}
	out.mu.Unlock()
	if clip {
		tail = outputTail(tail, secrets...)
	}
	res.OutputTail = tail

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		zero := 0
		res.ExitCode = &zero
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Err = "timed out after " + timeout.String() + " and was killed"
	case errors.As(runErr, &exitErr) && exitErr.ExitCode() >= 0:
		code := exitErr.ExitCode()
		res.ExitCode = &code
	default:
		res.Err = Scrub(runErr.Error(), secrets...)
	}
	return res
}

// childEnv is the whole environment of a command. The Kubernetes service
// variables let kubectl find the API server with the Pod's own identity.
func childEnv(dir string) []string {
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + dir,
		"TMPDIR=" + dir,
		"LANG=C.UTF-8",
	}
	for _, k := range []string{"KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT", "KUBERNETES_SERVICE_PORT_HTTPS"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Scrub removes each secret from s. platform-api redacts token shapes again
// when it stores the tail; this pass removes the exact values this request
// carried, whatever they look like.
func Scrub(s string, secrets ...string) string {
	for _, sec := range secrets {
		if len(sec) >= 6 {
			s = strings.ReplaceAll(s, sec, "[removed]")
		}
	}
	return s
}

// Exact values first, then generic shapes, then the only output clipping.
func outputTail(s string, secrets ...string) string {
	s = notice.Redact(Scrub(s, secrets...))
	if len(s) > tailSend {
		s = s[len(s)-tailSend:]
	}
	return strings.ToValidUTF8(s, "?")
}
