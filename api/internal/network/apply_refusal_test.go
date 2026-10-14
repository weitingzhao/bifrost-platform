package network

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

// TD-333: all apply cases were red with this synthetic script. The read-only
// configuration and ordinary upstream cases below were green.
func TestFirewallApplyRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, output          string
		missing, unconfigured bool
		want                  int
	}{
		{name: "required", output: "required", want: 502},
		{name: "UNIFI_USER", output: "UNIFI_USER", want: 502},
		{name: "missing-script", missing: true, want: 503},
		{name: "unconfigured", unconfigured: true, want: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("PLATFORM_PROJECT_ROOT", root)
			t.Setenv("UNIFI_USER", "test-user")
			t.Setenv("UNIFI_PASS", "test-password")
			if tc.unconfigured {
				t.Setenv("UNIFI_USER", "")
			}
			if !tc.missing {
				if err := os.Mkdir(filepath.Join(root, "scripts"), 0700); err != nil {
					t.Fatal(err)
				}
				// This synthetic executor never contacts a controller.
				body := "import sys\nopen('started', 'w').close()\nprint('" + tc.output + "')\nsys.exit(1)\n"
				if err := os.WriteFile(filepath.Join(root, "scripts", "unifi_firewall_setup.py"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := NewHandler(nil)
			rec := httptest.NewRecorder()
			h.HandleFirewallApply(rec, httptest.NewRequest(http.MethodPost, "/", nil))
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.output != "" && !strings.Contains(rec.Body.String(), tc.output) {
				t.Fatalf("script output lost: %s", rec.Body.String())
			}
			_, err := os.Stat(filepath.Join(root, "started"))
			if wantStarted := !tc.missing && !tc.unconfigured; (err == nil) != wantStarted {
				t.Fatalf("script started=%v want=%v", err == nil, wantStarted)
			}
		})
	}
}

func TestReadOnlyNetworkErrors(t *testing.T) {
	t.Setenv("UNIFI_USER", "")
	t.Setenv("UNIFI_PASS", "")
	_, configErr := unifi.ConfigFromEnv()
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"unconfigured", configErr, 503},
		{"wrapped-unconfigured", fmt.Errorf("dial: %w", configErr), 503},
		{"upstream", errors.New("controller unavailable"), 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, WithDial(func(context.Context) (*unifi.Client, error) {
				return nil, tc.err
			}))
			rec := httptest.NewRecorder()
			h.HandleStatus(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
