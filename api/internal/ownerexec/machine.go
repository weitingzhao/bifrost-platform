package ownerexec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var machineName = regexp.MustCompile("^[a-z][a-z0-9-]{0,30}$")

// ServeHost polls outbound only and runs at most one approved card at a time.
// The platform consumes the ticket when it returns a claim. A lost claim
// response is never permission to run a command locally.
func ServeHost(ctx context.Context, base, host, bearer, home string, timeout time.Duration, logf func(string, ...any)) error {
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return errors.New("host execution requires a verified HTTPS platform origin")
	}
	if !machineName.MatchString(host) || !filepath.IsAbs(home) || timeout <= 0 || timeout > 30*time.Minute {
		return errors.New("set a machine ID, absolute account home, and a positive timeout up to 30m")
	}
	if bearer == "" || strings.ContainsAny(bearer, " \t\r\n") {
		return errors.New("machine executor credential is invalid")
	}
	p := NewHTTPPlatform(base)
	p.Client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	runner := &Server{Platform: p, Timeout: timeout, Logf: logf}
	runner.Run = func(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome {
		return runMachineCommand(ctx, command, home, timeout, secrets...)
	}
	return runner.pollMachine(ctx, host, bearer, p.claimHost)
}

type machineClaim func(context.Context, string, string) (Claim, error)

func (s *Server) pollMachine(ctx context.Context, host, bearer string, claim machineClaim) error {
	executor := "owner-exec:" + host
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		got, err := claim(ctx, bearer, executor)
		switch {
		case err != nil:
			var refusal *StatusError
			if errors.As(err, &refusal) && (refusal.Status == 401 || refusal.Status == 403) {
				return fmt.Errorf("platform rejected the machine credential: HTTP %d", refusal.Status)
			}
			s.Logf("host %s: claim unavailable; retrying", host)
		case got.Approval.ID != "":
			s.runMachineClaim(ctx, got, host, bearer)
		}
		timer.Reset(3 * time.Second)
	}
}

func (s *Server) runMachineClaim(ctx context.Context, got Claim, host, bearer string) {
	card := got.Approval
	if got.LeaseID == "" {
		s.Logf("host %s: missing lease; command ignored", host)
		return
	}
	if card.Status != "running" || card.Action != Action || card.Runner != "host" || card.Host() != host || card.Command() == "" {
		notStarted := false
		_ = s.Platform.Result(ctx, bearer, card.ID, Result{
			LeaseID: got.LeaseID, Started: &notStarted, Refusal: "ticket did not name this machine and a command",
		})
		return
	}
	period := time.Duration(got.HeartbeatSeconds) * time.Second
	if period <= 0 {
		period = 20 * time.Second
	}
	s.wg.Add(1)
	s.execute(card.ID, got.LeaseID, card.Command(), bearer, "", period)
}

// ReadMachineToken reads the credential provisioned for this machine’s
// least-privilege executor. It is never placed in command arguments or child env.
func ReadMachineToken(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("machine credential path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		return "", errors.New("machine credential must be a regular file with mode 0600")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("could not load the machine credential")
	}
	return strings.TrimSpace(string(value)), nil
}
