package ownerexec

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// The runner and the commands it runs must not share a container.
//
// A command runs as the same user as whatever started it, so in one container
// it could read the seed file and the runner's memory, which holds the admin
// bearer of the request being served. PROD therefore runs two containers of
// this image in one Pod:
//
//   - the runner (default mode): holds the seed, checks the code, talks to
//     platform-api. No cluster identity.
//   - the shell (`owner-exec shell`): runs commands with the Pod's cluster
//     identity. No seed, and it never sees a bearer or a code.
//
// The runner hands the shell a command over the Pod's loopback. Only the two
// containers share that network; a command posting to the shell gains nothing
// it does not already have, and the runner still asks every caller for an
// admin bearer and a code.
const ShellRunPath = "/run"

const shellUnanswered = "the shell container did not answer; whether the command ran is unknown"
const shellUnreadable = "the shell container's answer could not be read; whether the command ran is unknown"

type shellRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type shellResponse struct {
	ExitCode   *int   `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	OutputSHA  string `json:"output_sha256"`
	OutputTail string `json:"output_tail"`
	Err        string `json:"error"`
}

// ShellHandler serves the shell container: it runs what the runner sends.
func ShellHandler(logf func(format string, args ...any)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+ShellRunPath, func(w http.ResponseWriter, r *http.Request) {
		var req shellRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.Command) == "" || req.TimeoutSeconds <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "command and timeout_seconds are required"})
			return
		}
		// Not the request's context: a runner that goes away must not leave a
		// half-run command; it runs to its own timeout and the card goes unknown.
		out := runCommand(context.Background(), req.Command, time.Duration(req.TimeoutSeconds)*time.Second, false)
		logf("shell: ran a command in %s", out.Duration.Round(time.Millisecond))
		writeJSON(w, http.StatusOK, shellResponse{
			ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(),
			OutputSHA: out.OutputSHA, OutputTail: out.OutputTail, Err: out.Err,
		})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	return mux
}

// RemoteRun is a Server.Run that sends the command to the shell container.
// The secrets are scrubbed from what comes back here, in the runner: they are
// never sent to the shell.
func RemoteRun(base string) func(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome {
	base = strings.TrimRight(base, "/")
	return func(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome {
		started := time.Now()
		raw, _ := json.Marshal(shellRequest{Command: command, TimeoutSeconds: int(timeout / time.Second)})
		client := &http.Client{Timeout: timeout + 30*time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ShellRunPath, bytes.NewReader(raw))
		if err != nil {
			return Outcome{Err: "shell request: " + err.Error(), Duration: time.Since(started)}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return Outcome{Err: shellUnanswered, Duration: time.Since(started)}
		}
		defer func() { _ = resp.Body.Close() }()
		var out shellResponse
		if resp.StatusCode != http.StatusOK {
			return Outcome{Err: shellUnreadable, Duration: time.Since(started)}
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil || (out.ExitCode == nil && out.Err == "") {
			return Outcome{Err: shellUnreadable, Duration: time.Since(started)}
		}
		return Outcome{
			ExitCode: out.ExitCode, Duration: time.Duration(out.DurationMS) * time.Millisecond,
			OutputSHA: out.OutputSHA, OutputTail: outputTail(out.OutputTail, secrets...), Err: Scrub(out.Err, secrets...),
		}
	}
}
