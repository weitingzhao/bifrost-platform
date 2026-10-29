package delivery

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

type STGHealth struct {
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	HTTPStatus int    `json:"http_status"`
	// Evidence is populated only after service/status validation. B5-a's
	// health field requires status=ok; a failed/unknown result supplies none.
	Evidence map[string]any `json:"evidence,omitempty"`
}

// ReadHealth uses only STG platform health; Trade smoke is not an input.
// Timeout/transport failure is unknown, HTTP and payload failures are definite.
func (a *STGReleaseAdapter) ReadHealth(ctx context.Context) STGHealth {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	client := a.HTTP
	if client == nil {
		return STGHealth{Outcome: "unknown", Reason: "transport_unavailable"}
	}
	// Preserve the injected transport but never follow redirects to another
	// service. A 200 at the redirected destination is not STG health.
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stgHealthURL, nil)
	if err != nil {
		return STGHealth{Outcome: "unknown", Reason: "request"}
	}
	resp, err := copy.Do(req)
	if err != nil {
		var timeout net.Error
		why := "transport"
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			why = "timeout"
		}
		return STGHealth{Outcome: "unknown", Reason: why}
	}
	defer resp.Body.Close()
	out := STGHealth{Outcome: "failed", HTTPStatus: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		out.Reason = "http"
		return out
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		out.Outcome, out.Reason = "unknown", "body_read"
		return out
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "application/json" || len(body) > 1<<20 {
		out.Reason = "json"
		return out
	}
	payload, err := canon.Parse(body)
	m, ok := payload.(map[string]any)
	if err != nil || !ok {
		out.Reason = "json"
		return out
	}
	if m["service"] != "bifrost-platform-api" {
		out.Reason = "service"
		return out
	}
	if m["status"] != "ok" {
		out.Reason = "status"
		return out
	}
	out.Outcome, out.Reason = "passed", ""
	out.Evidence = m
	return out
}
