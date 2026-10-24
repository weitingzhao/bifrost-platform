package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memoryTransport func(*http.Request) (*http.Response, error)

func (fn memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestLegacyPutSessionAndCSRFRetry(t *testing.T) {
	for _, status := range []int{401, 403} {
		for _, persistent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/persistent=%t", status, persistent), func(t *testing.T) {
				logins, puts := 0, 0
				body := map[string]any{"use_fixedip": true, "fixed_ip": "192.168.10.80", "name": "ubt-k3s-07"}
				c := New(Config{Host: "controller.invalid", Site: "test-site"})
				c.SetHTTPClient(&http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
					w := httptest.NewRecorder()
					if r.URL.Path == "/api/auth/login" {
						logins++
						w.Header().Set("Set-Cookie", fmt.Sprintf("TOKEN=%d; Path=/", logins))
						w.Header().Set("X-CSRF-Token", fmt.Sprintf("csrf-%d", logins))
					} else {
						if r.Method != "PUT" || r.URL.Path != "/proxy/network/api/s/test-site/rest/user/client-1" {
							t.Fatalf("request=%s %s", r.Method, r.URL.Path)
						}
						puts++
						csrf := fmt.Sprintf("csrf-%d", logins)
						if puts == 3 {
							csrf = "rotated"
						}
						if r.Header.Get("Cookie") != fmt.Sprintf("TOKEN=%d", logins) || r.Header.Get("X-CSRF-Token") != csrf {
							t.Fatalf("session headers=%v", r.Header)
						}
						var got map[string]any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Fatal(err)
						}
						if len(got) != 3 || got["fixed_ip"] != body["fixed_ip"] || got["name"] != body["name"] || got["use_fixedip"] != true {
							t.Fatalf("body=%v", got)
						}
						if puts == 1 || persistent {
							w.WriteHeader(status)
						} else {
							w.Header().Set("X-Updated-CSRF-Token", "rotated")
						}
					}
					_, _ = w.WriteString(`{"meta":{"rc":"ok"},"data":[]}`)
					return w.Result(), nil
				})})
				_, err := c.LegacyPut(context.Background(), "rest/user/client-1", body)
				if (err != nil) != persistent || logins != 2 || puts != 2 {
					t.Fatalf("err=%v logins=%d puts=%d", err, logins, puts)
				}
				if !persistent {
					if _, err := c.LegacyPut(context.Background(), "/rest/user/client-1", body); err != nil {
						t.Fatal(err)
					}
					if logins != 2 || puts != 3 {
						t.Fatalf("session not reused: %d %d", logins, puts)
					}
				}
			})
		}
	}
}

func TestFixedIPConfigFromEnv(t *testing.T) {
	t.Setenv("UNIFI_USER", "fake")
	t.Setenv("UNIFI_PASS", "fake")
	t.Setenv("UNIFI_SERVER_SUBNET", "")
	t.Setenv("UNIFI_SERVER_GATEWAY", "")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.ServerSubnet != "192.168.10.0/24" || cfg.ServerGateway != "192.168.10.1" {
		t.Fatalf("defaults not loaded: %v", err)
	}
	t.Setenv("UNIFI_SERVER_SUBNET", "10.2.0.0/24")
	t.Setenv("UNIFI_SERVER_GATEWAY", "10.2.0.254")
	cfg, err = ConfigFromEnv()
	if err != nil || cfg.ServerSubnet != "10.2.0.0/24" || cfg.ServerGateway != "10.2.0.254" {
		t.Fatalf("overrides not loaded: %v", err)
	}
}

// Assert the request boundary as well as uncertainty: login, encoding and an
// already-cancelled context cannot have sent a PUT and keep their old errors.
func TestLegacyPutUncertaintyBoundary(t *testing.T) {
	for _, mode := range []string{"login", "encode", "cancelled", "transport", "timeout", "5xx", "body", "4xx"} {
		t.Run(mode, func(t *testing.T) {
			puts := 0
			c := New(Config{Host: "controller.invalid", Site: "default"})
			c.SetHTTPClient(&http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				if r.URL.Path == "/api/auth/login" {
					if mode == "login" {
						w.WriteHeader(500)
					} else {
						w.Header().Set("Set-Cookie", "TOKEN=fake")
					}
					_, _ = w.WriteString(`{}`)
					return w.Result(), nil
				}
				puts++
				switch mode {
				case "transport":
					return nil, errors.New("lost response")
				case "timeout":
					return nil, context.DeadlineExceeded
				case "5xx":
					w.WriteHeader(503)
					_, _ = w.WriteString("previous HTTP 401")
				case "4xx":
					w.WriteHeader(400)
				case "body":
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: failingPutBody{}}, nil
				default:
					t.Fatalf("unexpected PUT in %s", mode)
				}
				return w.Result(), nil
			})})
			ctx := context.Background()
			var body any = map[string]any{"name": "host"}
			if mode == "encode" {
				body = make(chan int)
			}
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := c.LegacyPut(ctx, "/rest/user/id", body)
			uncertain := mode == "transport" || mode == "timeout" || mode == "5xx" || mode == "body"
			wantPuts := 0
			if uncertain || mode == "4xx" {
				wantPuts = 1
			}
			if err == nil || errors.Is(err, ErrWriteUncertain) != uncertain || puts != wantPuts {
				t.Fatalf("err=%v puts=%d wantUncertain=%v wantPuts=%d", err, puts, uncertain, wantPuts)
			}
			if mode == "5xx" && !strings.Contains(err.Error(), "HTTP 503") {
				t.Fatalf("lost detail: %v", err)
			}
		})
	}
}

type failingPutBody struct{}

func (failingPutBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingPutBody) Close() error             { return nil }
