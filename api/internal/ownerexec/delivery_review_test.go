package ownerexec

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Source: codex/reports/review-f2-fix.md B2 and "原提交尚缺哪些测试".
func TestReviewB2OnlyAcceptedConflictAcknowledgesDelivery(t *testing.T) {
	for _, tc := range []struct {
		status  int
		message string
		want    bool
	}{
		{409, "result already accepted", true},
		{409, "lease mismatch", false},
		{409, "lease lapsed; a refusal cannot requeue an unknown run", false},
		{503, "result already accepted", false},
		{409, "", false},
	} {
		err := &StatusError{Status: tc.status, Body: map[string]any{"error": tc.message}}
		if got := resultDelivered(fmt.Errorf("wrapped: %w", err)); got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
	if !resultDelivered(nil) || resultDelivered(errors.New("result already accepted")) {
		t.Fatal("wrong non-HTTP acknowledgement")
	}
}

type lostReplyPlatform struct {
	*fakePlatform
	attempts int
}

func (p *lostReplyPlatform) Result(ctx context.Context, bearer, id string, res Result) error {
	p.attempts++
	if p.attempts == 1 {
		_ = p.fakePlatform.Result(ctx, bearer, id, res)
		return errors.New("reply lost after storage")
	}
	return &StatusError{Status: 409, Body: map[string]any{"error": "result already accepted"}}
}

// Source: codex/reports/review-f2-fix.md B2: stored result, lost reply, retry, Wait.
func TestReviewB2LostReplyWaitEndsWithoutRerun(t *testing.T) {
	h := newHarness(t, "approved")
	p := &lostReplyPlatform{fakePlatform: h.fake}
	h.srv.Platform = p
	done := make(chan struct{})
	go func() { h.post(testBearer, h.body(h.code())); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait retained an already accepted result")
	}
	if p.attempts != 2 || h.commandsRun() != 1 || h.fake.result == nil {
		t.Fatalf("attempts=%d runs=%d", p.attempts, h.commandsRun())
	}
}

// Source: codex/reports/review-f2-fix.md C1: the digest must describe displayed output.
func TestReviewC1HashRedactedTail(t *testing.T) {
	shell := httptest.NewServer(ShellHandler(func(string, ...any) {}))
	defer shell.Close()
	for name, run := range map[string]func(context.Context, string, time.Duration, ...string) Outcome{"local": RunCommand, "remote": RemoteRun(shell.URL)} {
		for _, tc := range []struct{ name, command, secret, want string }{
			{"exact", "printf 123456", "123456", "[removed]"},
			{"generic", "printf token=CANARYVALUE", "", "token=[redacted]"},
			{"clipped", "printf '%3000s' ''", "", strings.Repeat(" ", tailSend)},
			{"overflow", "head -c 1048577 /dev/zero | tr '\\0' S", "", "[output omitted: redaction buffer limit exceeded]"},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				out := run(context.Background(), tc.command, time.Minute, tc.secret)
				if out.ExitCode == nil || *out.ExitCode != 0 || out.OutputTail != tc.want || out.OutputSHA != shaOf(tc.want) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
			})
		}
	}
}

// Source: codex/reports/review-f2-fix.md C1: the shell must not emit a raw digest.
func TestReviewC1ShellDoesNotHashRawOutput(t *testing.T) {
	out := runCommand(context.Background(), "printf 123456", time.Second, false)
	if out.OutputTail != "123456" || out.OutputSHA != "" {
		t.Fatalf("unexpected intermediate: %+v", out)
	}
}
