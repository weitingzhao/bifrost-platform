package server

import (
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// TD-267: the release-window refusals start-pipeline answers with (HTTP 502,
// message) are transient; a refusal the stored params can never pass is not.
func TestReleaseWindowRefusalsAreTransient(t *testing.T) {
	for _, msg := range []string{
		"REFUSED: release window held by someone else (who=a what=bifrost-trade-core); bifrost-deliver-platform-prod needs one of bifrost-platform,bifrost-ui",
		"REFUSED: release window held by a (what=x); y is not part of that release",
		"REFUSED: no release window for bifrost-deliver-prod. Open one first: release.sh hold --what x",
		"REFUSED: cannot read the release window (timeout)",
	} {
		_, err := interpretAction(http.StatusBadGateway, []byte(`{"ok":false,"message":"`+msg+`"}`))
		if !actions.IsTransient(err) {
			t.Fatalf("%q is not transient", msg)
		}
	}
	for _, body := range []string{
		`{"ok":false,"message":"REFUSED: missing who; pass who matching the release window holder (who=a what=b)"}`,
		`{"error":"plan is not a successful policy pass"}`,
	} {
		_, err := interpretAction(http.StatusBadGateway, []byte(body))
		if err == nil || actions.IsTransient(err) {
			t.Fatalf("%s: err=%v, want a permanent error", body, err)
		}
	}
	if _, err := interpretAction(http.StatusServiceUnavailable, []byte(`{"error":"actuation policy is not loaded"}`)); !actions.IsTransient(err) {
		t.Fatal("503 is not transient")
	}
}
