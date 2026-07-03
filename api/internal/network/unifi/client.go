package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 45 * time.Second

// Client is the shared UniFi REST client for platform-api (Session v2 + legacy v1 reads).
type Client struct {
	cfg        Config
	http       *http.Client
	baseURL    string
	cookie     string
	csrf       string
	loggedIn   bool
}

// New builds a client. Call Login before LegacyGet/V2Get/V2Write.
func New(cfg Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // UCG uses self-signed cert
	return &Client{
		cfg:     cfg,
		baseURL: "https://" + strings.TrimRight(cfg.Host, "/"),
		http: &http.Client{
			Timeout:   defaultTimeout,
			Transport: transport,
		},
	}
}

// Config returns the client configuration (read-only).
func (c *Client) Config() Config {
	return c.cfg
}

// SetHTTPClient overrides the HTTP client (tests only).
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.http = httpClient
}

// Login authenticates via POST /api/auth/login and stores session cookie + CSRF token.
func (c *Client) Login(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/api/auth/login", map[string]string{
		"username": c.cfg.User,
		"password": c.cfg.Pass,
	}, requestOpts{login: true})
	if err != nil {
		return fmt.Errorf("unifi login: %w", err)
	}
	c.loggedIn = true
	return nil
}

// LegacyGet calls GET /proxy/network/api/s/{site}{path} (v1 API).
func (c *Client) LegacyGet(ctx context.Context, path string) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.do(ctx, http.MethodGet, fmt.Sprintf("/proxy/network/api/s/%s%s", c.cfg.Site, path), nil, requestOpts{})
}

// V2Get calls GET /proxy/network/v2/api/site/{site}{path}.
func (c *Client) V2Get(ctx context.Context, path string) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.do(ctx, http.MethodGet, fmt.Sprintf("/proxy/network/v2/api/site/%s%s", c.cfg.Site, path), nil, requestOpts{})
}

// V2Post calls POST on the v2 site API.
func (c *Client) V2Post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/proxy/network/v2/api/site/%s%s", c.cfg.Site, path), body, requestOpts{})
}

// V2Delete calls DELETE on the v2 site API.
func (c *Client) V2Delete(ctx context.Context, path string) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/proxy/network/v2/api/site/%s%s", c.cfg.Site, path), nil, requestOpts{})
	return err
}

// IntegrationGet calls GET with X-API-KEY (audit-only when site UUID unavailable on UCG 10.4.57).
func (c *Client) IntegrationGet(ctx context.Context, path string) (json.RawMessage, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("UNIFI_API_KEY not set")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.do(ctx, http.MethodGet, path, nil, requestOpts{integration: true})
}

// ListDevices wraps stat/device.
func (c *Client) ListDevices(ctx context.Context) (json.RawMessage, error) {
	return c.LegacyGet(ctx, "/stat/device")
}

// ListClients wraps stat/sta.
func (c *Client) ListClients(ctx context.Context) (json.RawMessage, error) {
	return c.LegacyGet(ctx, "/stat/sta")
}

// Health wraps stat/health for the default site.
func (c *Client) Health(ctx context.Context) (json.RawMessage, error) {
	return c.LegacyGet(ctx, "/stat/health")
}

// ListZones wraps v2 firewall zones.
func (c *Client) ListZones(ctx context.Context) (json.RawMessage, error) {
	return c.V2Get(ctx, "/firewall/zone")
}

// ListPolicies wraps v2 firewall policies.
func (c *Client) ListPolicies(ctx context.Context) (json.RawMessage, error) {
	return c.V2Get(ctx, "/firewall-policies")
}

// IntegrationSitesHaveID returns true when integration /sites returns a non-empty site id.
func (c *Client) IntegrationSitesHaveID(ctx context.Context) (bool, error) {
	raw, err := c.IntegrationGet(ctx, "/proxy/network/integration/v1/sites")
	if err != nil {
		return false, err
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, err
	}
	for _, s := range payload.Data {
		if strings.TrimSpace(s.ID) != "" {
			return true, nil
		}
	}
	return false, nil
}

type requestOpts struct {
	login       bool
	integration bool
}

func (c *Client) do(ctx context.Context, method, path string, body any, opts requestOpts) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.integration {
		req.Header.Set("X-API-KEY", c.cfg.APIKey)
	} else {
		if c.cookie != "" {
			req.Header.Set("Cookie", c.cookie)
		}
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if opts.login {
		if sc := resp.Header.Get("Set-Cookie"); sc != "" {
			c.cookie = strings.Split(sc, ";")[0]
		}
		if token := resp.Header.Get("X-Csrf-Token"); token != "" {
			c.csrf = token
		} else if token := resp.Header.Get("x-csrf-token"); token != "" {
			c.csrf = token
		}
	}
	if updated := resp.Header.Get("x-updated-csrf-token"); updated != "" {
		c.csrf = updated
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s %s -> HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 900))
	}
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(raw) {
		return json.RawMessage(fmt.Sprintf(`{"raw":%q}`, string(raw))), nil
	}
	return json.RawMessage(raw), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
