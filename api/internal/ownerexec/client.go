package ownerexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Card is the part of an approval record the runner reads.
type Card struct {
	ID         string         `json:"id"`
	Number     int            `json:"number"`
	Action     string         `json:"action"`
	Status     string         `json:"status"`
	Runner     string         `json:"runner"`
	Params     map[string]any `json:"params"`
	ParamsHash string         `json:"params_hash"`
}

// Command is the card's exact command text.
func (c Card) Command() string {
	s, _ := c.Params["command"].(string)
	return s
}

// Claim is a successful POST /approvals/claim.
type Claim struct {
	Approval         Card   `json:"approval"`
	LeaseID          string `json:"lease_id"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
}

// Result is POST /approvals/{id}/result.
type Result struct {
	LeaseID    string `json:"lease_id"`
	Started    *bool  `json:"started,omitempty"`
	Refusal    string `json:"refusal,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	// OutputSHA256 is SHA-256 of the final redacted OutputTail, not raw command output.
	OutputSHA256 string `json:"output_sha256,omitempty"`
	OutputTail   string `json:"output_tail,omitempty"`
	Error        string `json:"error,omitempty"`
}

// StatusError is a non-2xx answer from platform-api, passed on to the page.
type StatusError struct {
	Status int
	Body   map[string]any
}

func (e *StatusError) Error() string {
	if msg, _ := e.Body["error"].(string); msg != "" {
		return fmt.Sprintf("platform-api %d: %s", e.Status, msg)
	}
	return fmt.Sprintf("platform-api %d", e.Status)
}

// Platform is the runner's view of platform-api. Every call carries the
// bearer of the request being served; the runner has no token of its own.
type Platform interface {
	IsAdmin(ctx context.Context, bearer string) (bool, error)
	Card(ctx context.Context, bearer, id string) (Card, error)
	Approve(ctx context.Context, bearer, id string, body json.RawMessage) error
	Claim(ctx context.Context, bearer, id, executor string) (Claim, error)
	Heartbeat(ctx context.Context, bearer, id, lease string) error
	Result(ctx context.Context, bearer, id string, res Result) error
}

// HTTPPlatform talks to platform-api over HTTP.
type HTTPPlatform struct {
	Base   string
	Client *http.Client
}

func NewHTTPPlatform(base string) *HTTPPlatform {
	return &HTTPPlatform{Base: strings.TrimRight(base, "/"), Client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *HTTPPlatform) do(ctx context.Context, bearer, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		// url.Error carries the URL only; the bearer is a header and is not in it.
		return fmt.Errorf("platform-api unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		se := &StatusError{Status: resp.StatusCode, Body: map[string]any{}}
		_ = json.Unmarshal(raw, &se.Body)
		return se
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (p *HTTPPlatform) IsAdmin(ctx context.Context, bearer string) (bool, error) {
	var caps struct {
		Authenticated bool `json:"authenticated"`
		CanAdmin      bool `json:"can_admin"`
	}
	if err := p.do(ctx, bearer, http.MethodGet, "/api/v1/auth/capabilities", nil, &caps); err != nil {
		return false, err
	}
	return caps.Authenticated && caps.CanAdmin, nil
}

func (p *HTTPPlatform) Card(ctx context.Context, bearer, id string) (Card, error) {
	var raw map[string]json.RawMessage
	if err := p.do(ctx, bearer, http.MethodGet, "/api/v1/approvals/"+url.PathEscape(id), nil, &raw); err != nil {
		return Card{}, err
	}
	// The record is either the body itself or under "approval".
	var c Card
	if inner, ok := raw["approval"]; ok {
		return c, json.Unmarshal(inner, &c)
	}
	whole, _ := json.Marshal(raw)
	return c, json.Unmarshal(whole, &c)
}

func (p *HTTPPlatform) Approve(ctx context.Context, bearer, id string, body json.RawMessage) error {
	return p.do(ctx, bearer, http.MethodPost, "/api/v1/approvals/"+url.PathEscape(id)+"/approve", body, nil)
}

func (p *HTTPPlatform) Claim(ctx context.Context, bearer, id, executor string) (Claim, error) {
	var c Claim
	in := map[string]any{"executor_id": executor, "id": id, "runners": []string{"owner"}, "actions": []string{Action}}
	err := p.do(ctx, bearer, http.MethodPost, "/api/v1/approvals/claim", in, &c)
	return c, err
}

func (p *HTTPPlatform) Heartbeat(ctx context.Context, bearer, id, lease string) error {
	return p.do(ctx, bearer, http.MethodPost, "/api/v1/approvals/"+url.PathEscape(id)+"/heartbeat", map[string]string{"lease_id": lease}, nil)
}

func (p *HTTPPlatform) Result(ctx context.Context, bearer, id string, res Result) error {
	return p.do(ctx, bearer, http.MethodPost, "/api/v1/approvals/"+url.PathEscape(id)+"/result", res, nil)
}
