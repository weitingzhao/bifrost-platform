package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

// Before TD-333: both script-output cases were red, configuration was green.
func TestFirewallRefusalInterpretation(t *testing.T) {
	t.Setenv("UNIFI_USER", "")
	t.Setenv("UNIFI_PASS", "")
	_, configErr := unifi.ConfigFromEnv()
	for _, tc := range []struct {
		name      string
		err       error
		transient bool
	}{
		{"required", errors.New("apply script: exit status 1: required"), false},
		{"UNIFI_USER", errors.New("apply script: exit status 1: UNIFI_USER"), false},
		{"unconfigured", configErr, true},
		{"missing-script", fmt.Errorf("%w: script missing", network.ErrApplyNotStarted), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := network.NewHandler(nil, network.WithApplyFirewall(func(context.Context, bool) (map[string]any, error) {
				return nil, tc.err
			}))
			rec := httptest.NewRecorder()
			h.HandleFirewallApply(rec, httptest.NewRequest(http.MethodPost, "/", nil))
			_, err := interpretAction(rec.Code, rec.Body.Bytes())
			want := http.StatusBadGateway
			if tc.transient {
				want = http.StatusServiceUnavailable
			}
			if rec.Code != want || err == nil || actions.IsTransient(err) != tc.transient {
				t.Fatalf("status=%d err=%v transient=%v", rec.Code, err, actions.IsTransient(err))
			}
		})
	}
}
