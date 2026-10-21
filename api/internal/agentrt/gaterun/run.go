package gaterun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Run runs one gate command: `go test -json -count=1 -run '^(A|B)$' pkg` in
// dir (the module's root, api/), with AGENTRT_TEST_REQUIRED=1 (whatever the
// environment says), checks that every named test ran and passed, and
// writes the summary to out. It returns the exit code: 0 only when the gate
// passed.
func Run(ctx context.Context, dir string, c Command, out io.Writer) int {
	if err := c.check(); err != nil {
		fmt.Fprintln(out, "gate:", err)
		return 2
	}
	quoted := make([]string, len(c.Tests))
	for i, t := range c.Tests {
		quoted[i] = regexp.QuoteMeta(t)
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-json", "-count=1", "-run", "^("+strings.Join(quoted, "|")+")$", c.Pkg)
	cmd.Dir, cmd.Env = dir, requiredEnv(os.Environ())
	var stream, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stream, &stderr
	runErr := cmd.Run()
	return finish(out, c, &stream, stderr.String(), runErr)
}

// finish reads the stream of a run that ended (runErr its end) and writes
// what it found, the verdict last whatever went wrong reading it (review
// td397-4 B5). It returns the exit code.
func finish(out io.Writer, c Command, stream io.Reader, stderr string, runErr error) int {
	rep, err := Check(stream, c.Tests)
	if err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("reading go test's output: %w", err))
	}
	if s := strings.TrimSpace(stderr); s != "" {
		lines := strings.Split(s, "\n")
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		fmt.Fprintln(out, "--- go test's stderr ---")
		fmt.Fprintln(out, strings.Join(lines, "\n"))
	}
	for _, l := range rep.Summary(30) {
		fmt.Fprintln(out, l)
	}
	// The last line decides, whatever came before it.
	fmt.Fprintln(out, rep.Verdict(c.Pkg, runErr))
	if runErr != nil || !rep.Pass() {
		return 1
	}
	return 0
}

// requiredEnv is env with AGENTRT_TEST_REQUIRED=1 and no other value of it:
// a test that needs the test database fails without one.
func requiredEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "AGENTRT_TEST_REQUIRED=") {
			out = append(out, kv)
		}
	}
	return append(out, "AGENTRT_TEST_REQUIRED=1")
}
