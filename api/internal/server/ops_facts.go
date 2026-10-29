package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/opsfacts"
)

// opsFactReader is a fixed allowlist of existing read handlers. No caller URL,
// credentials, write handlers, or auth-bypassing recursive router calls enter it.
// Keep this adapter in server: opsfacts imports only the standard library.
type opsFactReader struct {
	handlers map[opsfacts.Source]http.HandlerFunc
}

func (r opsFactReader) Read(ctx context.Context, source opsfacts.Source) opsfacts.ReadResult {
	handler, ok := r.handlers[source]
	if !ok {
		return opsfacts.ReadResult{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.Path, nil)
	if err != nil {
		return opsfacts.ReadResult{Err: err}
	}
	response := &factResponse{header: make(http.Header)}
	handler(response, request)
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return opsfacts.ReadResult{Status: response.status, Body: response.body.Bytes(), Err: response.err}
}

// A bounded response writer reuses the existing source contracts without an
// HTTP round trip. Oversized/invalid documents become errors, never empty facts.
type factResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
	err    error
}

func (w *factResponse) Header() http.Header { return w.header }
func (w *factResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *factResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len()+len(p) > 4<<20 {
		w.err = errors.New("fact source response exceeds limit")
		return 0, w.err
	}
	return w.body.Write(p)
}
func (s *Server) opsFactSources() opsFactReader {
	return opsFactReader{handlers: map[opsfacts.Source]http.HandlerFunc{
		opsfacts.HealthSource:  s.handleHealth,
		opsfacts.AlertsSource:  s.telemetry.HandleAlerts,
		opsfacts.TargetsSource: s.telemetry.HandleTargets,
		opsfacts.WindowSource:  s.delivery.HandleGetReleaseWindow,
		opsfacts.PolicySource:  s.handleReleasePolicy,
		opsfacts.AverageSource: s.telemetry.HandlePromQL,
		opsfacts.CountSource:   s.telemetry.HandlePromQL,
	}}
}
func (s *Server) handleOpsSystemFacts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, opsfacts.ProjectSystem(ctx, s.opsFactSources(), time.Now))
}
