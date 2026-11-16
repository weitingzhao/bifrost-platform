// Package netobservations owns transport-only read observations, not identity or policy.
package netobservations

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func DrainHTTP(client *http.Client, req *http.Request) (int, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func DialTCP(ctx context.Context, addr string, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func ClassifyHTTP(code int) (string, string) {
	switch {
	case code == 200:
		return "ok", fmt.Sprintf("HTTP %d", code)
	case code == 503:
		return "degraded", fmt.Sprintf("HTTP %d (service starting or degraded)", code)
	case code >= 400:
		return "fail", fmt.Sprintf("HTTP %d", code)
	default:
		return "unknown", fmt.Sprintf("HTTP %d", code)
	}
}
