package approvalrules

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// requestRunsDirectly are the actions that a request for them runs when the
// signed release policy covers it: the action and the route that runs it.
// Each is a route whose every parameter is in the body.
//
// Owner 2026-11-15. POST /api/v1/approvals answered 400 "call directly" for a
// request the policy covers, and the caller was to call the action's own
// route. A caller can only do that with a tool for the route; a session's
// tool list is fixed when it starts, so every session older than the action
// could ask and be told to go elsewhere (apply_manifest from 07:39 that day,
// run_agentrt_migration from 11:46: four pieces of work waited on a new
// session). Asking is the one door every caller has. Now the answer to a
// covered request is the action's own answer.
//
// start_pipeline_run is not here: its route carries the pipeline in the path
// and the window holder in the body, and its callers have had the tool since
// before the policy existed.
var requestRunsDirectly = map[string]string{
	"apply_manifest":        "/api/v1/actuation/manifests/apply",
	"run_agentrt_migration": "/api/v1/agentrt/migrations/run",
}

// RanDirectlyHeader names the policy that covered a request which was run
// instead of stored.
const RanDirectlyHeader = "X-Bifrost-Ran-Directly"

type bufferedResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(code int)        { b.code = code }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }

// CoveredRequestRuns sits in front of the router. A POST /api/v1/approvals
// goes to the approval service as before; only when that answers 400 "call
// directly" with the policy that covers it, for one of requestRunsDirectly,
// the same caller's request is sent to the action's route (root), which
// authenticates it, judges it on the policy again and audits it exactly as a
// direct call. Nothing is stored either way. Every other answer, a drill
// included, is passed on untouched. readBody reads the request's body and
// leaves it readable for the handler after it.
func CoveredRequestRuns(root http.Handler, readBody func(*http.Request) ([]byte, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || strings.TrimRight(r.URL.Path, "/") != "/api/v1/approvals" {
				next.ServeHTTP(w, r)
				return
			}
			raw, err := readBody(r)
			var ask struct {
				Action   string         `json:"action"`
				Params   map[string]any `json:"params"`
				Reason   string         `json:"reason"`
				Rollback string         `json:"rollback"`
			}
			if err != nil || json.Unmarshal(raw, &ask) != nil {
				next.ServeHTTP(w, r)
				return
			}
			route, ok := requestRunsDirectly[strings.TrimSpace(ask.Action)]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			buf := &bufferedResponse{header: http.Header{}, code: http.StatusOK}
			next.ServeHTTP(buf, r)
			var answer struct {
				Error string `json:"error"`
				By    string `json:"auto_approved_by"`
			}
			if buf.code != http.StatusBadRequest || json.Unmarshal(buf.body.Bytes(), &answer) != nil ||
				answer.Error != "call directly" || strings.TrimSpace(answer.By) == "" {
				for k, v := range buf.header {
					w.Header()[k] = v
				}
				w.WriteHeader(buf.code)
				_, _ = w.Write(buf.body.Bytes())
				return
			}
			body := map[string]any{}
			for k, v := range ask.Params {
				body[k] = v
			}
			// The trace fields of a direct call; the caller's own words.
			body["reason"], body["rollback"] = ask.Reason, ask.Rollback
			if _, has := body["unchecked"]; !has {
				body["unchecked"] = "asked through request_action; the caller's reason says what was not checked"
			}
			payload, err := json.Marshal(body)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			// A fresh routing context: chi would otherwise go on from where
			// the approvals route matched.
			again := r.Clone(context.WithValue(r.Context(), chi.RouteCtxKey, (*chi.Context)(nil)))
			again.URL.Path, again.URL.RawPath, again.RequestURI = route, "", route
			again.Body = io.NopCloser(bytes.NewReader(payload))
			again.ContentLength = int64(len(payload))
			again.Header.Set("Content-Type", "application/json")
			w.Header().Set(RanDirectlyHeader, answer.By)
			root.ServeHTTP(w, again)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
